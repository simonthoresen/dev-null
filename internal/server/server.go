package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/ssh"

	"dev-null/internal/console"
	"dev-null/internal/datadir"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/network"
	"dev-null/internal/render"
	"dev-null/internal/state"
)

// msgSender is anything that can receive a tea.Msg (tea.Program, display.Backend, etc.).
type msgSender interface {
	Send(msg tea.Msg)
}

type Server struct {
	state    *state.CentralState
	registry *commandRegistry
	dataDir  string       // root of games/, logs/
	port     string       // SSH listen port, e.g. "23234"
	clock    domain.Clock // central server clock (mockable in tests)

	maxPlayers int                  // max concurrent SSH sessions (0 = unlimited)
	programs   map[string]msgSender // key = playerID
	programsMu sync.Mutex

	sessions   map[string]ssh.Session // SSH sessions; nil in local mode
	sessionsMu sync.RWMutex

	// channels for communicating events to the console
	chatCh chan domain.Message   // new chat messages
	slogCh chan console.SlogLine // slog records routed to console

	chatLogFile *os.File // persistent chat log (timestamp-chat.log)

	shutdownFn func()
	sshServer  *ssh.Server

	consoleProgramMu sync.Mutex
	consoleProgram   *tea.Program // TUI mode: the tea.Program for the console
	consoleSender    msgSender    // GUI or TUI: anything that can receive tea.Msg
	consoleWidth     int

	upnpMapping *network.UPnPMapping

	tickInterval time.Duration // how often the server ticks (default 100ms)
	lastUpdateMu sync.Mutex    // protects lastUpdate
	lastUpdate   time.Time     // last time Update() was called on the active game
	startingDone chan struct{} // closed to end starting phase early

	// Per-player game viewport dimensions, reported by chrome models on resize.
	// Used by preRenderAllPlayers to know what dimensions to render at.
	// localRenderers: chrome models that render locally (GUI client, Render-locally toggle) —
	// the tick goroutine skips pre-rendering for these players.
	// canvasNeeds: SSH players in Blocks mode post their desired canvas size here
	// so the tick goroutine can run the JS raycaster once and cache the RGBA;
	// View() then blits + quadrant-encodes without holding Runtime.mu.
	viewports      map[string][2]int // playerID → [gameW, gameH]
	localRenderers map[string]bool   // playerID → true if rendering locally
	canvasNeeds    map[string][2]int // playerID → [canvasW, canvasH]
	viewportMu     sync.RWMutex

	// Per-player pre-rendered frame cache. The tick goroutine renders into these
	// before broadcasting TickMsg, so player View() calls just blit the result.
	renderCaches   map[string]*playerRenderCache
	renderCachesMu sync.RWMutex

	// Per-tick marshaled Game.state snapshot. Refreshed once per tick by the
	// tick goroutine and read by every local-rendering player during View(),
	// so N players no longer each re-marshal the same state N times.
	stateSnapshot atomic.Pointer[domain.StateSnapshot]

	metrics metrics
}

// playerRenderCache holds the most recently pre-rendered game frame for one player.
// The per-player mutexes ensure the tick goroutine (writer) and player goroutine
// (reader/blit) don't overlap on the same buffer.
//
// Canvas is pre-rendered alongside the buffer when the player's chrome has
// posted a canvas need (i.e. SSH players in Blocks mode). View() then only
// blits — the per-frame JS raycaster contention with the tick goroutine that
// dominated wolf3d at 16 players is gone.
//
// The buffer and canvas use separate mutexes because chrome's renderPlaying
// holds the buffer lock through the whole View() (via releaseCache) and
// reaches for the canvas inside the same call — one mutex would deadlock.
type playerRenderCache struct {
	mu     sync.Mutex
	buf    *render.ImageBuffer
	status string
	ncTree *domain.WidgetNode
	w, h   int

	canvasMu         sync.Mutex
	canvasImg        *image.RGBA
	canvasW, canvasH int
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
		chatCh:       make(chan domain.Message, 256),
		slogCh:       make(chan console.SlogLine, 256),
		viewports:    make(map[string][2]int),
		renderCaches: make(map[string]*playerRenderCache),
	}

	app.registerBuiltins()
	engine.LoadFigletFonts(dataDir)

	hostKeyPath := filepath.Join(datadir.ConfigDir(), "DevNull_ed25519")
	if err := os.MkdirAll(filepath.Dir(hostKeyPath), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	server, err := wish.NewServer(
		ssh.EmulatePty(),
		wish.WithAddress(address),
		wish.WithHostKeyPath(hostKeyPath),
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

func (a *Server) SlogCh() <-chan console.SlogLine { return a.slogCh }
func (a *Server) ChatCh() <-chan domain.Message {
	return a.chatCh
}

// --- Interface methods for chrome.ServerAPI and console.ServerAPI ---

func (a *Server) State() *state.CentralState { return a.state }
func (a *Server) Clock() domain.Clock        { return a.clock }
func (a *Server) DataDir() string            { return a.dataDir }
func (a *Server) Uptime() string             { return a.uptime() }

func (a *Server) BroadcastChat(msg domain.Message)          { a.broadcastChat(msg) }
func (a *Server) BroadcastMsg(msg tea.Msg)                  { a.broadcastMsg(msg) }
func (a *Server) SendToPlayer(playerID string, msg tea.Msg) { a.sendToPlayer(playerID, msg) }
func (a *Server) ServerLog(text string)                     { a.serverLog(text) }
func (a *Server) KickPlayer(playerID string) error          { return a.kickPlayer(playerID) }

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

const joinScriptURL = "https://raw.githubusercontent.com/simonthoresen/DevNullCore/main/join.ps1"

// inviteWinCommand returns the Windows invite command.
// The command is wrapped in "powershell -Command ..." so it runs directly from
// Win+R, a cmd prompt, or the Run dialog without opening PowerShell first.
func (a *Server) inviteWinCommand() string {
	token := a.inviteToken()
	inner := fmt.Sprintf("$env:NS='%s';irm %s|iex", token, joinScriptURL)
	return fmt.Sprintf(`powershell -Command "%s"`, inner)
}

// inviteSSHCommand returns the SSH command to join the server,
// choosing the best available endpoint (Pinggy > public IP > LAN IP > localhost).
func (a *Server) inviteSSHCommand() string {
	a.state.RLock()
	n := a.state.Net
	a.state.RUnlock()

	var sshPort int
	if p, err := net.LookupPort("tcp", a.port); err == nil {
		sshPort = p
	}

	portFlag := func(port int) string {
		if port == 22 || port == 0 {
			return ""
		}
		return fmt.Sprintf(" -p %d", port)
	}

	const sshOpts = " -o StrictHostKeyChecking=no"

	// Prefer Pinggy tunnel.
	if n.PinggyURL != "" {
		host := n.PinggyURL
		if idx := strings.Index(host, "://"); idx >= 0 {
			host = host[idx+3:]
		}
		port := 22
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			if p, err := net.LookupPort("tcp", host[idx+1:]); err == nil {
				port = p
			}
			host = host[:idx]
		}
		return "ssh" + sshOpts + portFlag(port) + " " + host
	}

	// Public IP via UPnP.
	if n.PublicIP != "" && n.UPnPMapped {
		return "ssh" + sshOpts + portFlag(sshPort) + " " + n.PublicIP
	}

	// LAN IP.
	if n.LANIP != "" {
		return "ssh" + sshOpts + portFlag(sshPort) + " " + n.LANIP
	}

	return "ssh" + sshOpts + portFlag(sshPort) + " localhost"
}

// InviteLinks returns the Windows and SSH join commands for sharing.
func (a *Server) InviteLinks() (win, ssh string) {
	return a.inviteWinCommand(), a.inviteSSHCommand()
}

// LogInviteCommand writes the current invite command to the server log,
// preceded by an ASCII QR code of the same string rendered with quadrant chars.
func (a *Server) LogInviteCommand() {
	cmd := a.inviteWinCommand()
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
			tickStart := time.Now()
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
				updateStart := time.Now()
				game.Update(dt)
				a.metrics.updateDurationNs.Add(uint64(time.Since(updateStart).Nanoseconds()))
			}

			// Pre-render game frames for all playing players sequentially.
			// This moves JS render calls out of 16 concurrent player goroutines
			// (where they serialize on Runtime.mu) into a single ordered pass here.
			// By completing before broadcastMsg, View() only needs a fast buffer blit.
			preRenderStart := time.Now()
			a.preRenderAllPlayers(game, phase)
			a.metrics.preRenderNs.Add(uint64(time.Since(preRenderStart).Nanoseconds()))

			// Marshal Game.state once per tick. Every local-rendering player
			// consumes the same snapshot from View() instead of re-marshaling.
			snapStart := time.Now()
			a.refreshStateSnapshot(game, phase)
			a.metrics.snapshotNs.Add(uint64(time.Since(snapStart).Nanoseconds()))

			bcStart := time.Now()
			a.broadcastMsg(domain.TickMsg{N: n})
			a.metrics.broadcastNs.Add(uint64(time.Since(bcStart).Nanoseconds()))

			// Check if JS called gameOver().
			a.checkGameOver()

			a.metrics.tickCount.Add(1)
			a.metrics.tickDurationNs.Add(uint64(time.Since(tickStart).Nanoseconds()))
		}
	}
}

// UpdatePlayerGameViewport records the game viewport dimensions for a player.
// Called by the chrome model on every terminal resize so the tick goroutine
// knows what size to pre-render at.
func (a *Server) UpdatePlayerGameViewport(playerID string, w, h int) {
	a.viewportMu.Lock()
	a.viewports[playerID] = [2]int{w, h}
	a.viewportMu.Unlock()
}

// SetPlayerLocalRenderer records that a player's chrome will render the game
// locally (GUI client with Render-locally toggle on). The tick goroutine skips
// pre-rendering for that player — Layout/RenderAscii/StatusBar/canvas calls
// are wasted because View() fills the viewport with placeholder cells.
func (a *Server) SetPlayerLocalRenderer(playerID string, isLocal bool) {
	a.viewportMu.Lock()
	if a.localRenderers == nil {
		a.localRenderers = make(map[string]bool)
	}
	if isLocal {
		a.localRenderers[playerID] = true
	} else {
		delete(a.localRenderers, playerID)
	}
	a.viewportMu.Unlock()
}

// SetPlayerCanvasNeed posts the canvas dimensions a player's chrome wants
// pre-rendered each tick. Chrome calls this on every View() so the tick loop
// can render the canvas once and View() just blits — converting per-View()
// JS raycasts (under Runtime.mu, contending with the tick goroutine) into
// one serial pass per tick.
//
// Pass w=0 or h=0 to clear the request (e.g. when switching to Ascii mode).
func (a *Server) SetPlayerCanvasNeed(playerID string, w, h int) {
	a.viewportMu.Lock()
	if a.canvasNeeds == nil {
		a.canvasNeeds = make(map[string][2]int)
	}
	if w > 0 && h > 0 {
		a.canvasNeeds[playerID] = [2]int{w, h}
	} else {
		delete(a.canvasNeeds, playerID)
	}
	a.viewportMu.Unlock()
}

// GetPreRenderedCanvas returns the most recent pre-rendered canvas image for
// a player. The caller must call release() when done. Returns nil/nil if no
// canvas is cached, or if dimensions don't match the request.
func (a *Server) GetPreRenderedCanvas(playerID string, expectW, expectH int) (*image.RGBA, func()) {
	a.renderCachesMu.RLock()
	c := a.renderCaches[playerID]
	a.renderCachesMu.RUnlock()
	if c == nil {
		return nil, nil
	}
	c.canvasMu.Lock()
	if c.canvasImg == nil || c.canvasW != expectW || c.canvasH != expectH {
		c.canvasMu.Unlock()
		return nil, nil
	}
	return c.canvasImg, func() { c.canvasMu.Unlock() }
}

// GetPreRenderedFrame returns the most recent pre-rendered game frame for a
// player. The caller must call release() when done blitting to allow the tick
// goroutine to write the next frame. Returns (nil, "", false) if no frame is
// cached yet or if dimensions don't match the expected size.
func (a *Server) GetPreRenderedFrame(playerID string, expectW, expectH int) (*render.ImageBuffer, *domain.WidgetNode, string, func()) {
	a.renderCachesMu.RLock()
	c := a.renderCaches[playerID]
	a.renderCachesMu.RUnlock()
	if c == nil {
		return nil, nil, "", nil
	}
	c.mu.Lock()
	if c.buf == nil || c.w != expectW || c.h != expectH {
		c.mu.Unlock()
		return nil, nil, "", nil
	}
	// Return the locked cache; caller must call release to unlock.
	return c.buf, c.ncTree, c.status, func() { c.mu.Unlock() }
}

// preRenderAllPlayers runs Layout/RenderAscii/StatusBar (and Canvas, when
// requested) for every playing player sequentially from the tick goroutine.
// By doing this before broadcastMsg, players' View() calls only need to blit
// the cached result instead of calling into the JS VM — eliminating
// Runtime.mu contention from the 16 concurrent player goroutines.
//
// Two skip rules:
//
//   - localRenderers: chrome that fills the viewport with placeholder cells
//     because the actual game is rendered client-side (GUI / Pixels / Blocks-
//     local). Pre-rendering for them is pure waste.
//   - canvasNeeds: only SSH-Blocks chromes post a canvas request. We render
//     the canvas RGBA here once and View() reuses it; the per-View() raycast
//     that previously serialized 16 goroutines on Runtime.mu is gone.
func (a *Server) preRenderAllPlayers(game domain.Game, phase domain.GamePhase) {
	if game == nil || phase != domain.PhasePlaying {
		return
	}

	// Snapshot viewport sizes (and which players want canvas / are local).
	a.viewportMu.RLock()
	type vpEntry struct {
		id      string
		w, h    int
		isLocal bool
		canvasW int
		canvasH int
	}
	vps := make([]vpEntry, 0, len(a.viewports))
	for id, dims := range a.viewports {
		if dims[0] <= 0 || dims[1] <= 0 {
			continue
		}
		e := vpEntry{id: id, w: dims[0], h: dims[1]}
		if a.localRenderers[id] {
			e.isLocal = true
		}
		if cn, ok := a.canvasNeeds[id]; ok {
			e.canvasW, e.canvasH = cn[0], cn[1]
		}
		vps = append(vps, e)
	}
	a.viewportMu.RUnlock()

	for _, v := range vps {
		if v.isLocal {
			// Client renders locally — server-side pre-render is wasted work.
			continue
		}
		// Get or create the per-player cache entry.
		a.renderCachesMu.RLock()
		c := a.renderCaches[v.id]
		a.renderCachesMu.RUnlock()
		if c == nil {
			c = &playerRenderCache{}
			a.renderCachesMu.Lock()
			// Double-check after acquiring write lock.
			if existing := a.renderCaches[v.id]; existing != nil {
				c = existing
			} else {
				a.renderCaches[v.id] = c
			}
			a.renderCachesMu.Unlock()
		}

		c.mu.Lock()
		if c.buf == nil || c.w != v.w || c.h != v.h {
			c.buf = render.NewImageBuffer(v.w, v.h)
			c.w, c.h = v.w, v.h
		} else {
			// Reset the buffer so stale ascii/overlay cells don't ghost
			// when the game's renderAscii leaves cells untouched (the
			// transparency rule for canvas+ascii compositing).
			c.buf.Clear()
		}
		// Pre-render Layout tree (if the game uses NC widgets) and the
		// ascii layer. When the game has both canvas and ascii hooks,
		// c.buf becomes the ascii overlay (cells the game leaves
		// default are transparent) blitted on top of the canvas.
		// Note: previously RenderAscii was skipped whenever a canvas
		// overlay was about to draw. Now dual-hook games re-incur one
		// JS RenderAscii call per player per tick — accepted as the
		// price of supporting HUD/minimap overlays.
		c.ncTree = game.Layout(v.id, v.w, v.h)
		if c.ncTree == nil && game.HasAsciiMode() {
			game.RenderAscii(c.buf, v.id, 0, 0, v.w, v.h)
		}
		c.status = game.StatusBar(v.id)
		c.mu.Unlock()

		// Canvas pre-render for SSH-Blocks. Caches the RGBA image; View() will
		// blit + quadrant-encode without re-entering the JS VM.
		// Separate mutex from buf so chrome's View() can hold buf-mu while
		// reading the canvas without deadlocking.
		if v.canvasW > 0 && v.canvasH > 0 && game.HasCanvasMode() {
			img := game.RenderCanvasImage(v.id, v.canvasW, v.canvasH)
			c.canvasMu.Lock()
			c.canvasImg = img
			c.canvasW, c.canvasH = v.canvasW, v.canvasH
			c.canvasMu.Unlock()
		} else {
			c.canvasMu.Lock()
			c.canvasImg = nil
			c.canvasW, c.canvasH = 0, 0
			c.canvasMu.Unlock()
		}
	}
}

// refreshStateSnapshot marshals the active game's Game.state into a shared
// per-tick snapshot. Only marshals keys whose JSON bytes changed since the
// previous snapshot — unchanged keys reuse their backing slices, so Models
// holding references through diff logic continue to compare by bytes.Equal
// correctly across ticks.
func (a *Server) refreshStateSnapshot(game domain.Game, phase domain.GamePhase) {
	if game == nil || phase != domain.PhasePlaying {
		a.stateSnapshot.Store(nil)
		return
	}
	srt, ok := game.(engine.ScriptRuntime)
	if !ok {
		a.stateSnapshot.Store(nil)
		return
	}
	stateObj := srt.State()
	if stateObj == nil {
		a.stateSnapshot.Store(nil)
		return
	}

	full, err := json.Marshal(stateObj)
	if err != nil {
		a.stateSnapshot.Store(nil)
		return
	}

	m, isMap := stateObj.(map[string]any)
	if !isMap {
		a.stateSnapshot.Store(&domain.StateSnapshot{
			Full: full,
			Keys: map[string][]byte{"_root": full},
		})
		return
	}

	// Reuse the previous snapshot's byte slices for keys whose marshaled
	// form is unchanged. Keeps allocations proportional to the churning
	// subset of the state.
	prev := a.stateSnapshot.Load()
	keys := make(map[string][]byte, len(m))
	ordered := make([]string, 0, len(m))
	for k := range m {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		data, err := json.Marshal(m[k])
		if err != nil {
			continue
		}
		if prev != nil {
			if old, ok := prev.Keys[k]; ok && bytes.Equal(old, data) {
				keys[k] = old
				continue
			}
		}
		keys[k] = data
	}

	a.stateSnapshot.Store(&domain.StateSnapshot{Full: full, Keys: keys})
}

// StateSnapshot returns the most recently marshaled Game.state, or nil when
// no game is playing. Returned pointers are immutable — the tick goroutine
// publishes a new pointer rather than mutating in place.
func (a *Server) StateSnapshot() *domain.StateSnapshot {
	return a.stateSnapshot.Load()
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

// SetConsoleSender registers a message sender for the console.
// Use this instead of SetConsoleProgram when the console runs without a full tea.Program.
func (a *Server) SetConsoleSender(s msgSender) {
	a.consoleProgramMu.Lock()
	a.consoleSender = s
	a.consoleProgramMu.Unlock()
}

func (a *Server) registerBuiltins() {
	a.registry.Register(domain.Command{
		Name:        "invite-win",
		Description: "Show QR code and copy the Windows join command to the clipboard",
		Handler: func(ctx domain.CommandContext, args []string) {
			cmd := a.inviteWinCommand()
			msg := "Windows invite link copied to clipboard"
			if qr, err := renderQR(cmd); err == nil {
				ctx.Reply(qr + msg)
			} else {
				ctx.Reply(msg)
			}
			if ctx.Clipboard != nil {
				ctx.Clipboard(cmd)
			}
		},
	})

	a.registry.Register(domain.Command{
		Name:        "invite-ssh",
		Description: "Show QR code and copy the SSH join command to the clipboard",
		Handler: func(ctx domain.CommandContext, args []string) {
			cmd := a.inviteSSHCommand()
			msg := "SSH invite link copied to clipboard"
			if qr, err := renderQR(cmd); err == nil {
				ctx.Reply(qr + msg)
			} else {
				ctx.Reply(msg)
			}
			if ctx.Clipboard != nil {
				ctx.Clipboard(cmd)
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
			go func() { p.Send(tea.QuitMsg{}) }() // async: called from inside the Bubble Tea update loop
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

	// --- Game commands ---

	a.registry.Register(domain.Command{
		Name:        "game-list",
		Description: "List available games",
		Handler: func(ctx domain.CommandContext, args []string) {
			available := engine.ListAllGames(a.dataDir)
			if len(available) == 0 {
				ctx.Reply("No games found.")
				return
			}
			ctx.Reply(a.formatGameList(available))
		},
	})

	a.registry.Register(domain.Command{
		Name:        "game-load",
		Description: "Load a game by name or URL",
		AdminOnly:   true,
		Complete: func(before []string) []string {
			if len(before) == 0 {
				items := engine.ListAllGames(a.dataDir)
				names := make([]string, len(items))
				for i, item := range items {
					names[i] = item.Name
				}
				return names
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
				path = engine.ResolveGamePathAll(a.dataDir, args[0])
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

// formatGameList builds the game list output with team range info, compatibility markers, and source attribution.
func (a *Server) formatGameList(available []engine.Item) string {
	a.state.RLock()
	active := a.state.GameName
	a.state.RUnlock()

	teamCount := a.state.TeamCount()

	return engine.FormatGameList(a.dataDir, available, active, teamCount)
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
