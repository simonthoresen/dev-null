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
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"

	"github.com/dop251/goja"

	"null-space/common"
)

type Server struct {
	state    *CentralState
	registry *commandRegistry
	dataDir  string // root of games/, plugins/, logs/
	port     string // SSH listen port, e.g. "23234"

	programs   map[string]*tea.Program // key = playerID
	programsMu sync.Mutex

	sessions   map[string]ssh.Session // SSH sessions; nil in local mode
	sessionsMu sync.RWMutex

	// channels for communicating events to the console
	logCh  chan string         // server log lines
	chatCh chan common.Message // new chat messages

	shutdownFn func()
	sshServer  *ssh.Server

	consoleProgramMu sync.Mutex
	consoleProgram   *tea.Program

	upnpMapping *upnpMapping

	splashDone    chan struct{} // closed to end splash phase early
	gameOverTimer chan struct{} // closed to end game-over phase early
}

func New(address, password, dataDir string) (*Server, error) {
	app := &Server{
		state:    newState(password),
		registry: newCommandRegistry(),
		dataDir:  dataDir,
		programs: make(map[string]*tea.Program),
		sessions: make(map[string]ssh.Session),
		logCh:    make(chan string, 256),
		chatCh:   make(chan common.Message, 256),
	}

	app.registerBuiltins()

	server, err := wish.NewServer(
		ssh.EmulatePty(),
		wish.WithAddress(address),
		wish.WithHostKeyPath("null-space_ed25519"),
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

func (a *Server) ChatCh() <-chan common.Message {
	return a.chatCh
}

func (a *Server) Start(ctx context.Context) error {
	go a.runTicker(ctx)

	errCh := make(chan error, 1)
	go func() {
		ln, err := newNoDelayListener(a.sshServer.Addr)
		if err != nil {
			errCh <- err
			return
		}
		slog.Info("TCP_NODELAY listener ready", "address", a.sshServer.Addr)
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
	a.upnpMapping.removeMapping()
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
	a.upnpMapping = tryUPnP(a.state, port)
	return a.state.Net.UPnPMapped
}

// SetupPublicIP detects the public IP and stores it in state.
// Returns the detected IP, or empty string if detection failed.
func (a *Server) SetupPublicIP() string {
	publicIP := detectPublicIP()
	if publicIP != "" {
		a.state.mu.Lock()
		a.state.Net.PublicIP = publicIP
		a.state.mu.Unlock()
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
	a.state.mu.RLock()
	n := a.state.Net
	a.state.mu.RUnlock()

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

const joinScriptURL = "https://raw.githubusercontent.com/simonthoresen/null-space/main/join.ps1"

// InviteCommand returns a PowerShell one-liner that downloads join.ps1
// from GitHub and runs it with the compact binary token.
func (a *Server) inviteCommand() string {
	return fmt.Sprintf(
		`powershell -c "$env:NS='%s';irm %s|iex"`,
		a.inviteToken(), joinScriptURL,
	)
}

// LogInviteCommand writes the current invite command to the server log.
func (a *Server) LogInviteCommand() {
	a.serverLog("Invite: " + a.inviteCommand())
}

func (a *Server) sessionMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			player := a.registerSession(sess)
			defer a.unregisterSession(player.ID)
			next(sess)
		}
	}
}

func (a *Server) programHandler(sess ssh.Session) *tea.Program {
	playerID := sess.Context().SessionID()
	program := tea.NewProgram(newChromeModel(a, playerID), a.sessionProgramOptions(sess)...)
	a.programsMu.Lock()
	a.programs[playerID] = program
	a.programsMu.Unlock()
	return program
}

func (a *Server) sessionProgramOptions(sess ssh.Session) []tea.ProgramOption {
	envs := sess.Environ()
	if pty, _, ok := sess.Pty(); ok && pty.Term != "" {
		envs = append(envs, "TERM="+pty.Term)
	}
	opts := wishbubbletea.MakeOptions(sess)
	opts = append(opts,
		tea.WithFPS(60),
		tea.WithColorProfile(colorprofile.Env(envs)),
		tea.WithOutput(newKittyStripWriter(sess)),
	)
	return opts
}

func (a *Server) registerSession(sess ssh.Session) *common.Player {
	player := &common.Player{
		ID:   sess.Context().SessionID(),
		Name: a.uniqueName(sess.User()),
	}

	a.sessionsMu.Lock()
	a.sessions[player.ID] = sess
	a.sessionsMu.Unlock()

	a.state.AddPlayer(player)
	slog.Info("player joined", "player_id", player.ID, "name", player.Name)

	joinMsg := common.Message{
		Author: "",
		Text:   fmt.Sprintf("%s joined.", player.Name),
	}
	a.broadcastChat(joinMsg)
	a.broadcastMsg(common.PlayerJoinedMsg{Player: player})

	// Assign a lobby team for the new player.
	a.state.EnsurePlayerTeam(player.ID)
	a.broadcastMsg(common.TeamUpdatedMsg{})

	// Check if this player was disconnected from a running game.
	a.state.mu.Lock()
	if oldID, ok := a.state.GameDisconnected[player.Name]; ok {
		a.state.replaceGamePlayerIDLocked(oldID, player.ID)
		delete(a.state.GameDisconnected, player.Name)
		a.state.mu.Unlock()
		a.serverLog(fmt.Sprintf("player %s rejoined game (was %s, now %s)", player.Name, oldID, player.ID))
	} else {
		a.state.mu.Unlock()
	}

	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		p.OnPlayerJoin(player.ID, player.Name)
	}
	return player
}

func (a *Server) unregisterSession(playerID string) {
	player := a.state.GetPlayer(playerID)
	if player != nil {
		slog.Info("player left", "player_id", playerID, "name", player.Name)
		a.broadcastChat(common.Message{
			Text: fmt.Sprintf("%s left.", player.Name),
		})
	}

	// Notify the game if this player was in the active game.
	if a.state.ActiveGame != nil && a.state.IsGamePlayer(playerID) {
		a.state.ActiveGame.OnPlayerLeave(playerID)
		if player != nil {
			a.state.mu.Lock()
			a.state.GameDisconnected[player.Name] = playerID
			a.state.mu.Unlock()
		}
	}

	// Always clean up lobby teams (game teams are a separate snapshot).
	a.state.RemovePlayerFromTeams(playerID)
	a.broadcastMsg(common.TeamUpdatedMsg{})

	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		p.OnPlayerLeave(playerID)
	}
	a.state.RemovePlayer(playerID)

	a.programsMu.Lock()
	delete(a.programs, playerID)
	a.programsMu.Unlock()

	a.sessionsMu.Lock()
	delete(a.sessions, playerID)
	a.sessionsMu.Unlock()

	a.broadcastMsg(common.PlayerLeftMsg{PlayerID: playerID})
}

func (a *Server) runTicker(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.state.mu.Lock()
			a.state.TickN++
			n := a.state.TickN
			a.state.mu.Unlock()
			a.broadcastMsg(common.TickMsg{N: n})

			// Check if JS called gameOver().
			a.checkGameOver()
		}
	}
}

// checkGameOver detects if the JS runtime signaled game over and initiates the transition.
func (a *Server) checkGameOver() {
	a.state.mu.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	a.state.mu.RUnlock()

	if game == nil || phase != common.PhasePlaying {
		return
	}
	rt, ok := game.(*jsRuntime)
	if !ok || !rt.IsGameOverPending() {
		return
	}

	// Save state if the game passed one as the second arg to gameOver().
	gameOverState := rt.GameOverStateExport()

	a.state.mu.RLock()
	gameName := a.state.GameName
	a.state.mu.RUnlock()

	if gameOverState != nil {
		if err := saveGameState(a.dataDir, gameName, gameOverState); err != nil {
			a.serverLog(fmt.Sprintf("warning: could not save game state: %v", err))
		} else {
			a.serverLog(fmt.Sprintf("game state saved: %s", gameName))
		}
	}

	a.state.SetGamePhase(common.PhaseGameOver)
	a.state.mu.Lock()
	a.state.GameOverResults = rt.GameOverResults()
	a.state.mu.Unlock()

	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhaseGameOver})
	a.broadcastChat(common.Message{Text: "Game over!"})
	a.serverLog("game over — waiting for players to acknowledge")

	// Start 15s timeout for game-over acknowledgment.
	a.gameOverTimer = make(chan struct{})
	go a.gameOverTimeout()
}

func (a *Server) gameOverTimeout() {
	select {
	case <-time.After(15 * time.Second):
	case <-a.gameOverTimer:
	}
	// Only unload if still in game-over phase.
	if a.state.GetGamePhase() == common.PhaseGameOver {
		a.unloadGame()
	}
}

// AcknowledgeGameOver marks a player as ready and ends game-over if all are ready.
func (a *Server) AcknowledgeGameOver(playerID string) {
	a.state.MarkPlayerReady(playerID)
	if a.state.AllPlayersReady() {
		select {
		case <-a.gameOverTimer:
		default:
			close(a.gameOverTimer)
		}
	}
}

func (a *Server) broadcastMsg(msg tea.Msg) {
	a.programsMu.Lock()
	progs := make([]*tea.Program, 0, len(a.programs))
	for _, p := range a.programs {
		progs = append(progs, p)
	}
	a.programsMu.Unlock()

	for _, p := range progs {
		go p.Send(msg)
	}

	a.consoleProgramMu.Lock()
	cp := a.consoleProgram
	a.consoleProgramMu.Unlock()
	if cp != nil {
		go cp.Send(msg)
	}
}

func (a *Server) broadcastChat(msg common.Message) {
	start := time.Now()
	// run through plugin pipeline before committing
	current := &msg
	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		current = p.OnChatMessage(current)
		if current == nil {
			return // message dropped by a plugin
		}
	}
	msg = *current

	a.state.AddChat(msg)

	select {
	case a.chatCh <- msg:
	default:
	}

	a.broadcastMsg(common.ChatMsg{Msg: msg})
	if dur := time.Since(start); dur > 100*time.Millisecond {
		slog.Warn("broadcastChat slow", "duration", dur, "text", msg.Text)
	}
}

func (a *Server) sendToPlayer(playerID string, msg tea.Msg) {
	a.programsMu.Lock()
	p := a.programs[playerID]
	a.programsMu.Unlock()
	if p != nil {
		go p.Send(msg)
	}
}

func (a *Server) kickPlayer(playerID string) error {
	a.sessionsMu.RLock()
	sess := a.sessions[playerID]
	a.sessionsMu.RUnlock()
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	return sess.Close()
}

func (a *Server) serverLog(line string) {
	slog.Info(line)
	select {
	case a.logCh <- line:
	default:
	}
}

func (a *Server) uptime() string {
	duration := time.Since(a.state.StartTime).Truncate(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

var romanNumerals = []string{
	"", "", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X",
	"XI", "XII", "XIII", "XIV", "XV", "XVI", "XVII", "XVIII", "XIX", "XX",
}

func (a *Server) uniqueName(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = "pilot"
	}
	// Replace spaces with hyphens.
	base = strings.ReplaceAll(base, " ", "-")

	// If this name belongs to a disconnected game player, let them reclaim it.
	a.state.mu.RLock()
	_, isReconnect := a.state.GameDisconnected[base]
	a.state.mu.RUnlock()
	if isReconnect {
		return base
	}

	name := base
	index := 2
	for a.state.PlayerByName(name) != nil {
		if index < len(romanNumerals) {
			name = fmt.Sprintf("%s-%s", base, romanNumerals[index])
		} else {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		index++
	}
	return name
}

func (a *Server) loadGame(path string) error {
	if isURL(path) {
		cacheDir := filepath.Join(a.dataDir, "games", ".cache")
		local, err := downloadToCache(path, cacheDir)
		if err != nil {
			return fmt.Errorf("download game: %w", err)
		}
		path = local
	}
	if a.state.ActiveGame != nil {
		a.unloadGame()
	}

	name := strings.TrimSuffix(filepath.Base(path), ".js")

	rt, err := LoadGame(path, a.state, a.serverLog, func(msg common.Message) {
		a.broadcastChat(msg)
	})
	if err != nil {
		return err
	}

	// Validate team count against game's declared range.
	teams := a.state.GetTeams()
	tr := rt.TeamRange()
	teamCount := len(teams)
	if tr.Min > 0 && teamCount < tr.Min {
		return fmt.Errorf("game requires at least %d teams (have %d)", tr.Min, teamCount)
	}
	if tr.Max > 0 && teamCount > tr.Max {
		return fmt.Errorf("game supports at most %d teams (have %d)", tr.Max, teamCount)
	}

	a.state.mu.Lock()
	// Snapshot teams for the game — lobby teams stay independent.
	a.state.GameTeams = teams
	a.state.GameDisconnected = make(map[string]string)
	a.state.ActiveGame = rt
	a.state.GameName = name
	a.state.GamePhase = common.PhaseSplash
	a.state.mu.Unlock()

	// Call init — players() and teams() now return game participants.
	savedState, err := loadGameState(a.dataDir, name)
	if err != nil {
		a.serverLog(fmt.Sprintf("warning: could not load saved state: %v", err))
	}
	rt.Init(savedState)

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.broadcastMsg(common.GameLoadedMsg{Name: name})
	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhaseSplash})
	a.broadcastChat(common.Message{Text: fmt.Sprintf("Game loaded: %s", name)})
	a.serverLog(fmt.Sprintf("game loaded: %s (splash)", name))

	// Start splash goroutine: waits up to 10s or until admin triggers start.
	a.splashDone = make(chan struct{})
	go a.splashTimer()

	return nil
}

func (a *Server) splashTimer() {
	select {
	case <-time.After(10 * time.Second):
	case <-a.splashDone:
	}
	// Only transition if still in splash phase.
	a.state.mu.Lock()
	if a.state.GamePhase != common.PhaseSplash {
		a.state.mu.Unlock()
		return
	}

	a.state.GamePhase = common.PhasePlaying
	game := a.state.ActiveGame
	a.state.mu.Unlock()

	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhasePlaying})
	a.serverLog("game started (playing)")

	if game != nil {
		game.Start()
	}
}

// StartGame is called when an admin acknowledges the splash screen.
func (a *Server) StartGame() {
	select {
	case <-a.splashDone:
		// already closed
	default:
		close(a.splashDone)
	}
}

func (a *Server) unloadGame() {
	// Cancel any pending splash or game-over timers.
	if a.splashDone != nil {
		select {
		case <-a.splashDone:
		default:
			close(a.splashDone)
		}
	}
	if a.gameOverTimer != nil {
		select {
		case <-a.gameOverTimer:
		default:
			close(a.gameOverTimer)
		}
	}

	a.state.mu.Lock()
	game := a.state.ActiveGame
	a.state.ActiveGame = nil
	a.state.GameName = ""
	a.state.GamePhase = common.PhaseNone
	a.state.GameOverReady = nil
	a.state.mu.Unlock()

	if game != nil {
		for _, cmd := range game.Commands() {
			a.registry.Unregister(cmd.Name)
		}
		game.Unload()
	}

	a.broadcastMsg(common.GameUnloadedMsg{})
	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhaseNone})
	a.broadcastChat(common.Message{Text: "Game unloaded."})
	a.serverLog("game unloaded")
}

func (a *Server) loadPlugin(name, path string) error {
	if isURL(path) {
		cacheDir := filepath.Join(a.dataDir, "plugins", ".cache")
		local, err := downloadToCache(path, cacheDir)
		if err != nil {
			return fmt.Errorf("download plugin: %w", err)
		}
		name = strings.TrimSuffix(filepath.Base(local), ".js")
		path = local
	}
	// don't load the same plugin twice
	_, names := a.state.GetPlugins()
	for _, n := range names {
		if n == name {
			return fmt.Errorf("plugin '%s' is already loaded", name)
		}
	}

	p, err := LoadPlugin(path, a.state, a.serverLog, func(msg common.Message) {
		a.broadcastChat(msg)
	})
	if err != nil {
		return err
	}

	a.state.AddPlugin(name, p)
	for _, cmd := range p.Commands() {
		a.registry.Register(cmd)
	}
	a.serverLog(fmt.Sprintf("plugin loaded: %s", name))
	return nil
}

func (a *Server) unloadPlugin(name string) error {
	p := a.state.RemovePlugin(name)
	if p == nil {
		return fmt.Errorf("plugin '%s' is not loaded", name)
	}
	for _, cmd := range p.Commands() {
		a.registry.Unregister(cmd.Name)
	}
	p.Unload()
	a.serverLog(fmt.Sprintf("plugin unloaded: %s", name))
	return nil
}

func (a *Server) SetConsoleProgram(p *tea.Program) {
	a.consoleProgramMu.Lock()
	a.consoleProgram = p
	a.consoleProgramMu.Unlock()
}

func (a *Server) registerBuiltins() {
	a.registry.Register(common.Command{
		Name:        "invite",
		Description: "Show the shareable join command for this server",
		Handler: func(ctx common.CommandContext, args []string) {
			ctx.Reply(a.inviteCommand())
		},
	})

	helpHandler := func(ctx common.CommandContext, args []string) {
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

	a.registry.Register(common.Command{
		Name:        "help",
		Description: "List available commands",
		Handler:     helpHandler,
	})

	a.registry.Register(common.Command{
		Name:        "commands",
		Description: "List available commands (alias for /help)",
		Handler:     helpHandler,
	})

	exitHandler := func(ctx common.CommandContext, args []string) {
		if ctx.PlayerID == "" {
			// Server console: shut down the server
			if a.shutdownFn != nil {
				a.shutdownFn()
			}
			return
		}
		// Try to close the SSH session; if there is none (local mode),
		// quit the player's Bubble Tea program directly.
		if err := a.kickPlayer(ctx.PlayerID); err != nil {
			a.programsMu.Lock()
			p := a.programs[ctx.PlayerID]
			a.programsMu.Unlock()
			if p != nil {
				// Async to avoid deadlock — this runs inside
				// the Bubble Tea update loop.
				go p.Quit()
			}
		}
	}

	a.registry.Register(common.Command{
		Name:        "exit",
		Description: "Disconnect from the server (stops server if used from console)",
		Handler:     exitHandler,
	})

	a.registry.Register(common.Command{
		Name:        "quit",
		Description: "Disconnect from the server (stops server if used from console)",
		Handler:     exitHandler,
	})

	a.registry.Register(common.Command{
		Name:        "who",
		Description: "List online players",
		Handler: func(ctx common.CommandContext, args []string) {
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

	a.registry.Register(common.Command{
		Name:        "admin",
		Description: "Elevate to admin (/admin <password>)",
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) != 1 {
				ctx.Reply("Usage: /admin <password>")
				return
			}
			a.state.mu.RLock()
			pw := a.state.AdminPassword
			a.state.mu.RUnlock()
			if args[0] != pw {
				ctx.Reply("Invalid password.")
				return
			}
			if ctx.PlayerID != "" {
				a.state.SetPlayerAdmin(ctx.PlayerID, true)
			}
			ctx.Reply("Admin privileges granted.")
		},
	})

	a.registry.Register(common.Command{
		Name:        "password",
		Description: "Change admin password (admin only)",
		AdminOnly:   true,
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) != 1 {
				ctx.Reply("Usage: /password <new>")
				return
			}
			a.state.mu.Lock()
			a.state.AdminPassword = args[0]
			a.state.mu.Unlock()
			ctx.Reply("Admin password changed.")
		},
	})

	a.registry.Register(common.Command{
		Name:             "kick",
		Description:      "Kick a player (admin only)",
		AdminOnly:        true,
		FirstArgIsPlayer: true,
		Handler: func(ctx common.CommandContext, args []string) {
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

	a.registry.Register(common.Command{
		Name:        "game",
		Description: "Game management. No args = list available. /game load|unload [name]",
		Complete: func(before []string) []string {
			switch len(before) {
			case 0:
				return []string{"list", "load", "unload"}
			case 1:
				switch before[0] {
				case "load":
					return listDir(filepath.Join(a.dataDir, "games"), ".js")
				case "unload":
					a.state.mu.RLock()
					name := a.state.GameName
					a.state.mu.RUnlock()
					if name != "" {
						return []string{strings.TrimSuffix(filepath.Base(name), ".js")}
					}
				}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			gamesDir := filepath.Join(a.dataDir, "games")
			if len(args) == 0 {
				available := listDir(gamesDir, ".js")
				if len(available) == 0 {
					ctx.Reply("No games found in games/")
					return
				}
				ctx.Reply(a.formatGameList(gamesDir, available))
				return
			}
			switch args[0] {
			case "list":
				available := listDir(gamesDir, ".js")
				if len(available) == 0 {
					ctx.Reply("No games found in games/")
					return
				}
				ctx.Reply(a.formatGameList(gamesDir, available))
			case "load":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /game load <name>")
					return
				}
				var path string
				if isURL(args[1]) {
					path = args[1]
				} else {
					path = filepath.Join(a.dataDir, "games", args[1]+".js")
				}
				if err := a.loadGame(path); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to load game: %v", err))
					return
				}
			case "unload":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				a.state.mu.RLock()
				active := a.state.GameName
				a.state.mu.RUnlock()
				if active == "" {
					ctx.Reply("No game is currently loaded.")
					return
				}
				a.unloadGame()
				ctx.Reply("Game unloaded.")
			default:
				ctx.Reply(fmt.Sprintf("Unknown subcommand '%s'. Use: list, load, unload", args[0]))
			}
		},
	})

	a.registry.Register(common.Command{
		Name:        "plugin",
		Description: "Plugin management. No args = list available. /plugin load|unload <name>",
		Complete: func(before []string) []string {
			switch len(before) {
			case 0:
				return []string{"list", "load", "unload"}
			case 1:
				switch before[0] {
				case "load":
					// offer available plugins not yet loaded
					_, loaded := a.state.GetPlugins()
					loadedSet := make(map[string]bool, len(loaded))
					for _, n := range loaded {
						loadedSet[n] = true
					}
					var out []string
					for _, name := range listDir(filepath.Join(a.dataDir, "plugins"), ".js") {
						if !loadedSet[name] {
							out = append(out, name)
						}
					}
					return out
				case "unload":
					_, names := a.state.GetPlugins()
					return names
				}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) == 0 {
				// List available plugins with loaded marker.
				available := listDir(filepath.Join(a.dataDir, "plugins"), ".js")
				_, loaded := a.state.GetPlugins()
				loadedSet := make(map[string]bool, len(loaded))
				for _, n := range loaded {
					loadedSet[n] = true
				}
				if len(available) == 0 && len(loaded) == 0 {
					ctx.Reply("No plugins found in plugins/")
					return
				}
				var lines []string
				for _, name := range available {
					line := "  " + name
					if loadedSet[name] {
						line += "  [loaded]"
					}
					lines = append(lines, line)
				}
				ctx.Reply("Available plugins:\n" + strings.Join(lines, "\n"))
				return
			}
			switch args[0] {
			case "load":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /plugin load <name>")
					return
				}
				name := args[1]
				var path string
				if isURL(name) {
					path = name
				} else {
					path = filepath.Join(a.dataDir, "plugins", name+".js")
				}
				if err := a.loadPlugin(name, path); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to load plugin: %v", err))
					return
				}
				// loadPlugin may have derived a new name from a cached URL filename.
				_, pluginNames := a.state.GetPlugins()
				loadedName := name
				if len(pluginNames) > 0 {
					loadedName = pluginNames[len(pluginNames)-1]
				}
				ctx.Reply(fmt.Sprintf("Plugin loaded: %s", loadedName))
			case "unload":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /plugin unload <name>")
					return
				}
				if err := a.unloadPlugin(args[1]); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to unload plugin: %v", err))
					return
				}
				ctx.Reply(fmt.Sprintf("Plugin unloaded: %s", args[1]))
			case "list":
				_, names := a.state.GetPlugins()
				if len(names) == 0 {
					ctx.Reply("No plugins currently loaded.")
					return
				}
				ctx.Reply("Loaded plugins: " + strings.Join(names, ", "))
			default:
				ctx.Reply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", args[0]))
			}
		},
	})

	a.registry.Register(common.Command{
		Name:             "msg",
		Description:      "Send a private message to a player",
		FirstArgIsPlayer: true,
		Handler: func(ctx common.CommandContext, args []string) {
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
			fromName := "admin"
			if ctx.PlayerID != "" {
				if p := a.state.GetPlayer(ctx.PlayerID); p != nil {
					fromName = p.Name
				}
			}
			pm := common.Message{
				Author:    fromName,
				Text:      text,
				IsPrivate: true,
				ToID:      target.ID,
				FromID:    ctx.PlayerID,
			}
			// Send to recipient
			a.sendToPlayer(target.ID, common.ChatMsg{Msg: pm})
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

// probeGameTeamRange reads a game JS file and extracts the Game.teamRange property
// without fully initializing the runtime. Returns zero TeamRange if not defined.
func probeGameTeamRange(path string) common.TeamRange {
	src, err := os.ReadFile(path)
	if err != nil {
		return common.TeamRange{}
	}
	vm := goja.New()
	_, err = vm.RunScript(path, string(src))
	if err != nil {
		return common.TeamRange{}
	}
	gameVal := vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return common.TeamRange{}
	}
	gameObj := gameVal.ToObject(vm)
	if gameObj == nil {
		return common.TeamRange{}
	}
	v := gameObj.Get("teamRange")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return common.TeamRange{}
	}
	obj := v.ToObject(vm)
	if obj == nil {
		return common.TeamRange{}
	}
	var tr common.TeamRange
	if mv := obj.Get("min"); mv != nil && !goja.IsUndefined(mv) {
		tr.Min = int(mv.ToInteger())
	}
	if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) {
		tr.Max = int(mv.ToInteger())
	}
	return tr
}

// formatGameList builds the game list output with team range info and compatibility markers.
func (a *Server) formatGameList(gamesDir string, available []string) string {
	a.state.mu.RLock()
	active := a.state.GameName
	a.state.mu.RUnlock()

	teamCount := a.state.TeamCount()

	var lines []string
	for _, name := range available {
		path := filepath.Join(gamesDir, name+".js")
		tr := probeGameTeamRange(path)

		// Compatibility check.
		compatible := true
		if tr.Min > 0 && teamCount < tr.Min {
			compatible = false
		}
		if tr.Max > 0 && teamCount > tr.Max {
			compatible = false
		}

		// Build the line.
		marker := "  "
		if tr.Min > 0 || tr.Max > 0 {
			if compatible {
				marker = "+ "
			} else {
				marker = "- "
			}
		}

		line := marker + name

		// Team range label.
		if tr.Min > 0 && tr.Max > 0 {
			if tr.Min == tr.Max {
				line += fmt.Sprintf("  [%d teams]", tr.Min)
			} else {
				line += fmt.Sprintf("  [%d-%d teams]", tr.Min, tr.Max)
			}
		} else if tr.Min > 0 {
			line += fmt.Sprintf("  [%d+ teams]", tr.Min)
		} else if tr.Max > 0 {
			line += fmt.Sprintf("  [up to %d teams]", tr.Max)
		}

		if name == active {
			line += "  [active]"
		}

		lines = append(lines, line)
	}

	header := fmt.Sprintf("Available games (%d teams configured):", teamCount)
	return header + "\n" + strings.Join(lines, "\n")
}

// listDir returns the name (without extension) of every file in dir that ends
// with ext, sorted alphabetically.
func listDir(dir, ext string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(names)
	return names
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
		_ = tc.SetNoDelay(true)
	}
	return conn, nil
}
