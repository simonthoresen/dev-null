package server

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/ssh"

	"dev-null/internal/console"
	"dev-null/internal/domain"
	"dev-null/internal/render"
	"dev-null/internal/engine"
	"dev-null/internal/network"
	"dev-null/internal/state"
)

// msgSender is anything that can receive a tea.Msg (tea.Program, display.Backend, etc.).
type msgSender interface {
	Send(msg tea.Msg)
}

type Server struct {
	state    *state.CentralState
	registry *commandRegistry
	dataDir  string // root of games/, logs/
	port     string // SSH listen port, e.g. "23234"
	clock    domain.Clock // central server clock (mockable in tests)

	maxPlayers int // max concurrent SSH sessions (0 = unlimited)
	programs   map[string]msgSender // key = playerID
	programsMu sync.Mutex

	sessions   map[string]ssh.Session // SSH sessions; nil in local mode
	sessionsMu sync.RWMutex

	// channels for communicating events to the console
	logCh   chan string         // server log lines (legacy, used by serverLog)
	chatCh  chan domain.Message // new chat messages
	slogCh  chan console.SlogLine // slog records routed to console

	chatLogFile *os.File // persistent chat log (timestamp-chat.log)

	shutdownFn func()
	sshServer  *ssh.Server

	consoleProgramMu sync.Mutex
	consoleProgram   *tea.Program  // TUI mode: the tea.Program for the console
	consoleSender    msgSender     // GUI or TUI: anything that can receive tea.Msg
	consoleWidth     int

	upnpMapping *network.UPnPMapping

	tickInterval  time.Duration   // how often the server ticks (default 100ms)
	lastUpdateMu  sync.Mutex     // protects lastUpdate
	lastUpdate    time.Time      // last time Update() was called on the active game
	startingDone  chan struct{}   // closed to end starting phase early
	gameOverTimer chan struct{}   // closed to end game-over phase early

	localPlayerName string // non-empty in --local mode; that player gets auto-admin
}

func New(address, password, dataDir string, tickInterval time.Duration) (*Server, error) {
	if tickInterval <= 0 {
		tickInterval = 100 * time.Millisecond
	}
	app := &Server{
		state:        state.New(password),
		registry:     newCommandRegistry(),
		dataDir:      dataDir,
		clock:        domain.RealClock{},
		tickInterval: tickInterval,
		maxPlayers:   domain.MaxConnections,
		programs:     make(map[string]msgSender),
		sessions:     make(map[string]ssh.Session),
		logCh:        make(chan string, 256),
		chatCh:       make(chan domain.Message, 256),
		slogCh:       make(chan console.SlogLine, 256),
	}

	app.registerBuiltins()
	engine.LoadFigletFonts(dataDir)

	server, err := wish.NewServer(
		ssh.EmulatePty(),
		wish.WithAddress(address),
		wish.WithHostKeyPath(filepath.Join(dataDir, "dev-null_ed25519")),
		wish.WithMiddleware(
			wishbubbletea.MiddlewareWithProgramHandler(app.programHandler),
			app.sessionMiddleware(),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		return nil, err
	}
	app.sshServer = server
	slog.Info("server created", "address", address)
	return app, nil
}

func (a *Server) SetShutdownFunc(fn func()) {
	a.shutdownFn = fn
}

func (a *Server) LogCh() <-chan string {
	return a.logCh
}

func (a *Server) SlogCh() <-chan console.SlogLine { return a.slogCh }
func (a *Server) ChatCh() <-chan domain.Message {
	return a.chatCh
}

// --- Interface methods for chrome.ServerAPI and console.ServerAPI ---

func (a *Server) State() *state.CentralState { return a.state }
func (a *Server) Clock() domain.Clock         { return a.clock }
func (a *Server) DataDir() string             { return a.dataDir }
func (a *Server) Uptime() string              { return a.uptime() }

func (a *Server) BroadcastChat(msg domain.Message) { a.broadcastChat(msg) }
func (a *Server) BroadcastMsg(msg tea.Msg)          { a.broadcastMsg(msg) }
func (a *Server) SendToPlayer(playerID string, msg tea.Msg) { a.sendToPlayer(playerID, msg) }
func (a *Server) ServerLog(text string)              { a.serverLog(text) }
func (a *Server) KickPlayer(playerID string) error   { return a.kickPlayer(playerID) }

func (a *Server) TabCandidates(input string, playerNames []string) (string, []string) {
	return a.registry.TabCandidates(input, playerNames)
}
func (a *Server) DispatchCommand(input string, ctx domain.CommandContext) {
	a.registry.Dispatch(input, ctx)
}
func (a *Server) SetConsoleWidth(w int) { a.consoleWidth = w }

func (a *Server) Start(ctx context.Context) error {
	return a.StartWithReady(ctx, nil)
}

// StartWithReady starts the SSH server. If ready is non-nil, it is closed once
// the listener is accepting connections. This allows callers to wait for the
// server to be ready before connecting.
func (a *Server) StartWithReady(ctx context.Context, ready chan<- struct{}) error {
	return a.serveWith(ctx, ready, nil)
}

// ServeListener starts the SSH server using an already-bound net.Listener.
// The listener's address is used as-is (no TCP_NODELAY wrapper is added on top).
// If ready is non-nil it is closed once the server begins accepting connections.
// This is primarily useful in tests, where the caller pre-binds to ":0" to
// obtain an OS-assigned port before handing the listener to the server.
func (a *Server) ServeListener(ctx context.Context, ln net.Listener, ready chan<- struct{}) error {
	return a.serveWith(ctx, ready, ln)
}

func (a *Server) serveWith(ctx context.Context, ready chan<- struct{}, ln net.Listener) error {
	go a.runTicker(ctx)

	errCh := make(chan error, 1)
	go func() {
		var err error
		if ln == nil {
			ln, err = newNoDelayListener(a.sshServer.Addr)
			if err != nil {
				errCh <- err
				return
			}
		}
		slog.Info("TCP_NODELAY listener ready", "address", ln.Addr())
		if ready != nil {
			close(ready)
		}
		err = a.sshServer.Serve(ln)
		if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		slog.Info("server context cancelled, shutting down")
		return a.shutdown()
	case err := <-errCh:
		return err
	}
}

func (a *Server) shutdown() error {
	slog.Info("server shutdown requested")
	a.upnpMapping.RemoveMapping()
	if err := a.sshServer.Close(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		slog.Error("server shutdown failed", "error", err)
		return err
	}
	slog.Info("server shutdown completed")
	return nil
}

// SetupUPnP attempts UPnP port mapping and returns true if successful.
// Should be called after New() and before Start().
func (a *Server) SetupUPnP(port string) bool {
	mapping, mapped := network.TryUPnP(port)
	a.upnpMapping = mapping
	a.state.Lock()
	a.state.Net.UPnPMapped = mapped
	a.state.Unlock()
	return mapped
}

// SetupPublicIP detects the public IP and stores it in state.
// Returns the detected IP, or empty string if detection failed.
func (a *Server) SetupPublicIP() string {
	publicIP := network.DetectPublicIP()
	if publicIP != "" {
		a.state.Lock()
		a.state.Net.PublicIP = publicIP
		a.state.Unlock()
	}
	return publicIP
}

// SetPort stores the SSH listen port so invite scripts can reference it.
func (a *Server) SetPort(port string) { a.port = port }


// InviteToken returns a base64url-encoded binary token containing the server's
// connection endpoints. The token is variable-length — trailing absent fields
// are omitted to keep it short.
//
// Format:
//
//	Bytes 0–1:  SSH port (uint16 big-endian)
//	Bytes 2–5:  LAN IP (4 bytes; 0.0.0.0 = absent) — reserved, currently always zero
//	Bytes 6–9:  Public/UPnP IP (4 bytes; 0.0.0.0 = absent)
//	Bytes 10–11: Pinggy port (uint16 big-endian; 0 = no Pinggy)
//	Bytes 12+:  Pinggy hostname (UTF-8, remaining bytes)
//
// join.ps1 always tries localhost first (not encoded). Field presence is
// determined by token length: ≥6 → LAN, ≥10 → public IP, ≥12 → Pinggy.
func (a *Server) inviteToken() string {
	a.state.RLock()
	n := a.state.Net
	a.state.RUnlock()

	// Parse SSH port.
	var sshPort uint16
	if p, err := net.LookupPort("tcp", a.port); err == nil {
		sshPort = uint16(p)
	}

	// Parse public IP if UPnP mapped.
	var publicIP net.IP
	if n.PublicIP != "" && n.UPnPMapped {
		publicIP = net.ParseIP(n.PublicIP).To4()
	}

	// Parse Pinggy host:port.
	var pinggyHost string
	var pinggyPort uint16
	if n.PinggyURL != "" {
		pinggyHost = n.PinggyURL
		// Strip protocol prefix (e.g. "tcp://").
		if idx := strings.Index(pinggyHost, "://"); idx >= 0 {
			pinggyHost = pinggyHost[idx+3:]
		}
		pp := "22"
		if idx := strings.LastIndex(pinggyHost, ":"); idx >= 0 {
			pp = pinggyHost[idx+1:]
			pinggyHost = pinggyHost[:idx]
		}
		if p, err := net.LookupPort("tcp", pp); err == nil {
			pinggyPort = uint16(p)
		}
	}

	// Build variable-length token, trimming trailing absent fields.
	buf := make([]byte, 2, 32)
	binary.BigEndian.PutUint16(buf[0:2], sshPort)

	// Determine how far we need to encode.
	hasPinggy := pinggyPort != 0 && pinggyHost != ""
	hasPublic := publicIP != nil
	needLAN := hasPublic || hasPinggy // must include LAN placeholder if later fields exist

	if needLAN || n.LANIP != "" {
		lanIP := net.ParseIP(n.LANIP).To4()
		if lanIP == nil {
			lanIP = net.IPv4zero.To4()
		}
		buf = append(buf, lanIP...)
	}

	if hasPublic || hasPinggy {
		if publicIP == nil {
			publicIP = net.IPv4zero.To4()
		}
		buf = append(buf, publicIP...)
	}

	if hasPinggy {
		pp := make([]byte, 2)
		binary.BigEndian.PutUint16(pp, pinggyPort)
		buf = append(buf, pp...)
		buf = append(buf, []byte(pinggyHost)...)
	}

	return base64.RawURLEncoding.EncodeToString(buf)
}

const joinScriptURL = "https://raw.githubusercontent.com/simonthoresen/dev-null/main/join.ps1"

// inviteCommand returns the invite command formatted for terminal display.
// The command is raw PowerShell — paste directly into a PowerShell window.
// Line continuations (backtick + newline) are inserted to keep within console
// width for easy copying.
func (a *Server) inviteCommand() string {
	token := a.inviteToken()
	width := a.consoleWidth
	if width < 40 {
		width = 120 // default before first resize
	}

	seg1 := fmt.Sprintf("$env:NS='%s';", token)
	seg2 := fmt.Sprintf("irm %s|iex", joinScriptURL)

	// Try single line first.
	oneLine := seg1 + seg2
	if len(oneLine) <= width {
		return oneLine
	}

	// Two lines: break between token assignment and irm.
	return seg1 + " `\n" + seg2
}

// LogInviteCommand writes the current invite command to the server log,
// preceded by an ASCII QR code of the same string rendered with quadrant chars.
func (a *Server) LogInviteCommand() {
	cmd := a.inviteCommand()
	qr, err := renderQR(cmd)
	if err == nil {
		a.serverLog("Invite:\n" + qr + cmd)
	} else {
		a.serverLog("Invite:\n" + cmd)
	}
}

func (a *Server) runTicker(ctx context.Context) {
	ticker := time.NewTicker(a.tickInterval)
	defer ticker.Stop()
	repaintTicker := time.NewTicker(30 * time.Second)
	defer repaintTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-repaintTicker.C:
			a.broadcastRepaint()
		case <-ticker.C:
			a.state.Lock()
			a.state.TickN++
			a.state.ElapsedSec = float64(a.state.TickN) * a.tickInterval.Seconds()
			n := a.state.TickN
			game := a.state.ActiveGame
			phase := a.state.GamePhase
			a.state.Unlock()

			// Call Update(dt) once per tick — before broadcast so game
			// state is fresh when players render.
			if game != nil && phase == domain.PhasePlaying {
				now := a.clock.Now()
				a.lastUpdateMu.Lock()
				dt := now.Sub(a.lastUpdate).Seconds()
				a.lastUpdate = now
				a.lastUpdateMu.Unlock()
				game.Update(dt)
			}

			a.broadcastMsg(domain.TickMsg{N: n})

			// Check if JS called gameOver().
			a.checkGameOver()
		}
	}
}

func (a *Server) uptime() string {
	a.state.RLock()
	startTime := a.state.StartTime
	a.state.RUnlock()
	duration := time.Since(startTime).Truncate(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}


func (a *Server) SetConsoleProgram(p *tea.Program) {
	a.consoleProgramMu.Lock()
	a.consoleProgram = p
	a.consoleSender = p
	a.consoleProgramMu.Unlock()
}

// SetConsoleSender registers a message sender for the console (GUI mode).
// Use this instead of SetConsoleProgram when the console runs in an
// EbitenBackend rather than a tea.Program.
func (a *Server) SetConsoleSender(s msgSender) {
	a.consoleProgramMu.Lock()
	a.consoleSender = s
	a.consoleProgramMu.Unlock()
}

func (a *Server) registerBuiltins() {
	a.registry.Register(domain.Command{
		Name:        "invite",
		Description: "Show the shareable join command for this server",
		Handler: func(ctx domain.CommandContext, args []string) {
			cmd := a.inviteCommand()
			if qr, err := renderQR(cmd); err == nil {
				ctx.Reply(qr + cmd)
			} else {
				ctx.Reply(cmd)
			}
			if ctx.Clipboard != nil {
				slog.Debug("invite: setting clipboard", "len", len(cmd))
				ctx.Clipboard(cmd)
			} else {
				slog.Debug("invite: no clipboard callback")
			}
		},
	})

	helpHandler := func(ctx domain.CommandContext, args []string) {
		cmds := a.registry.All()
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
		var lines []string
		for _, cmd := range cmds {
			if cmd.AdminOnly && !ctx.IsAdmin {
				continue
			}
			lines = append(lines, fmt.Sprintf("/%s — %s", cmd.Name, cmd.Description))
		}
		ctx.Reply(strings.Join(lines, "\n"))
	}

	a.registry.Register(domain.Command{
		Name:        "help",
		Description: "List available commands",
		Handler:     helpHandler,
	})

	a.registry.Register(domain.Command{
		Name:        "commands",
		Description: "List available commands (alias for /help)",
		Handler:     helpHandler,
	})

	exitHandler := func(ctx domain.CommandContext, args []string) {
		if ctx.IsConsole {
			// Server console: shut down the server
			if a.shutdownFn != nil {
				a.shutdownFn()
			}
			return
		}
		// Close the SSH session (no-op if there is none, e.g. local mode).
		a.kickPlayer(ctx.PlayerID) //nolint:errcheck
		// Always quit the program directly — SSH close alone doesn't
		// propagate until the next key press triggers a read.
		a.programsMu.Lock()
		p := a.programs[ctx.PlayerID]
		a.programsMu.Unlock()
		if p != nil {
			safeSend(p, tea.QuitMsg{}) // async: called from inside the Bubble Tea update loop
		}
	}

	a.registry.Register(domain.Command{
		Name:        "exit",
		Description: "Disconnect from the server (stops server if used from console)",
		Handler:     exitHandler,
	})

	a.registry.Register(domain.Command{
		Name:        "quit",
		Description: "Disconnect from the server (stops server if used from console)",
		Handler:     exitHandler,
	})

	a.registry.Register(domain.Command{
		Name:        "who",
		Description: "List online players",
		Handler: func(ctx domain.CommandContext, args []string) {
			players := a.state.ListPlayers()
			names := make([]string, 0, len(players))
			for _, p := range players {
				label := p.Name
				if p.IsAdmin {
					label += " (admin)"
				}
				names = append(names, label)
			}
			sort.Strings(names)
			ctx.Reply(fmt.Sprintf("Players online (%d): %s", len(names), strings.Join(names, ", ")))
		},
	})

	a.registry.Register(domain.Command{
		Name:        "password",
		Description: "Set admin password (console) or authenticate as admin (player)",
		Handler: func(ctx domain.CommandContext, args []string) {
			if ctx.IsConsole {
				// Console: set the admin password.
				if len(args) != 1 {
					ctx.Reply("Usage: /password <new>")
					return
				}
				a.state.Lock()
				a.state.AdminPassword = args[0]
				a.state.Unlock()
				ctx.Reply("Admin password set.")
				return
			}
			// Player: authenticate with the admin password.
			if len(args) != 1 {
				ctx.Reply("Usage: /password <password>")
				return
			}
			a.state.RLock()
			pw := a.state.AdminPassword
			a.state.RUnlock()
			if pw == "" {
				ctx.Reply("No admin password set. Ask the server operator to set one.")
				return
			}
			if args[0] != pw {
				ctx.Reply("Invalid password.")
				return
			}
			a.state.SetPlayerAdmin(ctx.PlayerID, true)
			ctx.Reply("Admin privileges granted.")
		},
	})

	a.registry.Register(domain.Command{
		Name:             "kick",
		Description:      "Kick a player (admin only)",
		AdminOnly:        true,
		FirstArgIsPlayer: true,
		Handler: func(ctx domain.CommandContext, args []string) {
			if len(args) < 1 {
				ctx.Reply("Usage: /kick <player>")
				return
			}
			target := a.state.PlayerByName(args[0])
			if target == nil {
				ctx.Reply("Player not found.")
				return
			}
			if err := a.kickPlayer(target.ID); err != nil {
				ctx.Reply(fmt.Sprintf("Kick failed: %v", err))
				return
			}
			ctx.Broadcast(fmt.Sprintf("%s was kicked.", target.Name))
		},
	})

	// --- Canvas commands ---

	a.registry.Register(domain.Command{
		Name:        "canvas-scale",
		Description: "Set canvas pixel density (e.g. 4, 8, 16)",
		AdminOnly:   true,
		Handler: func(ctx domain.CommandContext, args []string) {
			if len(args) < 1 {
				ctx.Reply("Usage: /canvas-scale <pixels-per-cell>")
				return
			}
			n, err := strconv.Atoi(args[0])
			if err != nil || n < domain.MinCanvasScale || n > domain.MaxCanvasScale {
				ctx.Reply(fmt.Sprintf("Scale must be %d-%d.", domain.MinCanvasScale, domain.MaxCanvasScale))
				return
			}
			a.state.Lock()
			a.state.CanvasScale = n
			a.state.Unlock()
			viewW := 120
			viewH := viewW * 9 / 16
			bw := render.CanvasBandwidthMbps(viewW, viewH, n, 10)
			ctx.Reply(fmt.Sprintf("Canvas scale set to %d (%dx%d px). ~%.1f Mbps/user at %dx%d viewport.",
				n, viewW*n, viewH*n, bw, viewW, viewH))
		},
	})

	a.registry.Register(domain.Command{
		Name:        "canvas-off",
		Description: "Disable canvas rendering",
		AdminOnly:   true,
		Handler: func(ctx domain.CommandContext, args []string) {
			a.state.Lock()
			a.state.CanvasScale = 0
			a.state.Unlock()
			ctx.Reply("Canvas rendering disabled.")
		},
	})

	a.registry.Register(domain.Command{
		Name:        "canvas-info",
		Description: "Show canvas rendering settings",
		AdminOnly:   true,
		Handler: func(ctx domain.CommandContext, args []string) {
			a.state.RLock()
			scale := a.state.CanvasScale
			game := a.state.ActiveGame
			a.state.RUnlock()
			if scale == 0 {
				ctx.Reply("Canvas rendering: off. Use /canvas-scale <n> to enable.")
				return
			}
			hasCanvas := game != nil && game.HasCanvasMode()
			viewW := 120
			viewH := viewW * 9 / 16
			bw := render.CanvasBandwidthMbps(viewW, viewH, scale, 10)
			status := "no game loaded"
			if game != nil {
				if hasCanvas {
					status = "active (game has renderCanvas)"
				} else {
					status = "game has no renderCanvas hook"
				}
			}
			ctx.Reply(fmt.Sprintf("Canvas scale: %d (%dx%d px). ~%.1f Mbps/user. %s",
				scale, viewW*scale, viewH*scale, bw, status))
		},
	})

	// --- Game commands ---

	a.registry.Register(domain.Command{
		Name:        "game-list",
		Description: "List available games",
		Handler: func(ctx domain.CommandContext, args []string) {
			gamesDir := filepath.Join(a.dataDir, "games")
			available := engine.ListGames(gamesDir)
			if len(available) == 0 {
				ctx.Reply("No games found in games/")
				return
			}
			ctx.Reply(a.formatGameList(gamesDir, available))
		},
	})

	a.registry.Register(domain.Command{
		Name:        "game-load",
		Description: "Load a game by name or URL",
		AdminOnly:   true,
		Complete: func(before []string) []string {
			if len(before) == 0 {
				return engine.ListGames(filepath.Join(a.dataDir, "games"))
			}
			return nil
		},
		Handler: func(ctx domain.CommandContext, args []string) {
			if len(args) < 1 {
				ctx.Reply("Usage: /game-load <name>")
				return
			}
			var path string
			if network.IsURL(args[0]) {
				path = args[0]
			} else {
				path = engine.ResolveGamePath(filepath.Join(a.dataDir, "games"), args[0])
			}
			if err := a.loadGame(path); err != nil {
				ctx.Reply(fmt.Sprintf("Failed to load game: %v", err))
			}
		},
	})

	a.registry.Register(domain.Command{
		Name:        "game-unload",
		Description: "Unload the active game",
		AdminOnly:   true,
		Handler: func(ctx domain.CommandContext, args []string) {
			a.state.RLock()
			active := a.state.GameName
			a.state.RUnlock()
			if active == "" {
				ctx.Reply("No game is currently loaded.")
				return
			}
			a.unloadGame()
		},
	})

	a.registry.Register(domain.Command{
		Name:        "game-suspend",
		Description: "Save game state and unload",
		AdminOnly:   true,
		Handler: func(ctx domain.CommandContext, args []string) {
			saveName := ""
			if len(args) >= 1 {
				saveName = args[0]
			}
			if saveName == "" {
				saveName = a.clock.Now().Format(domain.TimeFormatFileSafe)
			}
			if err := a.suspendGame(saveName); err != nil {
				ctx.Reply(fmt.Sprintf("Failed to suspend: %v", err))
			}
		},
	})

	a.registry.Register(domain.Command{
		Name:        "game-resume",
		Description: "Resume a suspended game (gameName/saveName)",
		AdminOnly:   true,
		Complete: func(before []string) []string {
			if len(before) == 0 {
				return state.ListSuspendNames(a.dataDir)
			}
			return nil
		},
		Handler: func(ctx domain.CommandContext, args []string) {
			if len(args) < 1 {
				saves := state.ListSuspends(a.dataDir, "")
				if len(saves) == 0 {
					ctx.Reply("No suspended games found. Usage: /game-resume <gameName/saveName>")
					return
				}
				var lines []string
				lines = append(lines, "Suspended games:")
				for _, s := range saves {
					lines = append(lines, fmt.Sprintf("  %s/%s  (%d teams, %s)",
						s.GameName, s.SaveName, s.TeamCount, s.SavedAt.Format(domain.TimeFormatShort)))
				}
				lines = append(lines, "Usage: /game-resume <gameName/saveName>")
				ctx.Reply(strings.Join(lines, "\n"))
				return
			}
			ref := args[0]
			parts := strings.SplitN(ref, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				ctx.Reply("Usage: /game-resume <gameName/saveName>")
				return
			}
			if err := a.resumeGame(parts[0], parts[1]); err != nil {
				ctx.Reply(fmt.Sprintf("Failed to resume: %v", err))
			}
		},
	})

	a.registry.Register(domain.Command{
		Name:             "msg",
		Description:      "Send a private message to a player",
		FirstArgIsPlayer: true,
		Handler: func(ctx domain.CommandContext, args []string) {
			if len(args) < 2 {
				ctx.Reply("Usage: /msg <player> <text>")
				return
			}
			target := a.state.PlayerByName(args[0])
			if target == nil {
				ctx.Reply("Player not found.")
				return
			}
			text := strings.Join(args[1:], " ")
			fromName := "console"
			if !ctx.IsConsole {
				if p := a.state.GetPlayer(ctx.PlayerID); p != nil {
					fromName = p.Name
				}
			}
			pm := domain.Message{
				Author:    fromName,
				Text:      text,
				IsPrivate: true,
				ToID:      target.ID,
				FromID:    ctx.PlayerID,
			}
			// Send to recipient
			a.sendToPlayer(target.ID, domain.ChatMsg{Msg: pm})
			// Confirm to sender
			ctx.Reply(fmt.Sprintf("[PM to %s] %s", target.Name, text))
			// Log to console
			ctx.ServerLog(fmt.Sprintf("[PM %s -> %s] %s", fromName, target.Name, text))

			select {
			case a.chatCh <- pm:
			default:
			}
		},
	})

}

// requireAdmin checks admin privileges and replies with a denial if lacking.
// Returns true if the caller should return (i.e. not admin).
func requireAdmin(ctx domain.CommandContext) bool {
	if !ctx.IsAdmin {
		ctx.Reply("Permission denied (admin only)")
		return true
	}
	return false
}


// formatGameList builds the game list output with team range info and compatibility markers.
func (a *Server) formatGameList(gamesDir string, available []string) string {
	a.state.RLock()
	active := a.state.GameName
	a.state.RUnlock()

	teamCount := a.state.TeamCount()

	return engine.FormatGameList(gamesDir, available, active, teamCount)
}

// noDelayListener wraps a net.Listener and ensures TCP_NODELAY is set on
// every accepted connection, disabling Nagle's algorithm so that single
// keystrokes are sent immediately without waiting for more data.
type noDelayListener struct {
	net.Listener
}

func newNoDelayListener(address string) (net.Listener, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &noDelayListener{Listener: ln}, nil
}

func (l *noDelayListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		if err := tc.SetNoDelay(true); err != nil {
			slog.Debug("TCP_NODELAY failed", "error", err)
		}
	}
	return conn, nil
}
