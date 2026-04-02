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
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"

	"null-space/common"
	"null-space/internal/chrome"
	"null-space/internal/console"
	"null-space/internal/engine"
	"null-space/internal/network"
	"null-space/internal/state"
	"null-space/internal/widget"
)

type Server struct {
	state    *state.CentralState
	registry *commandRegistry
	dataDir  string // root of games/, logs/
	port     string // SSH listen port, e.g. "23234"
	clock    common.Clock // central server clock (mockable in tests)

	programs   map[string]*tea.Program // key = playerID
	programsMu sync.Mutex

	sessions   map[string]ssh.Session // SSH sessions; nil in local mode
	sessionsMu sync.RWMutex

	// channels for communicating events to the console
	logCh   chan string         // server log lines (legacy, used by serverLog)
	chatCh  chan common.Message // new chat messages
	slogCh  chan console.SlogLine // slog records routed to console

	chatLogFile *os.File // persistent chat log (timestamp-chat.log)

	shutdownFn func()
	sshServer  *ssh.Server

	consoleProgramMu sync.Mutex
	consoleProgram   *tea.Program
	consoleWidth     int

	upnpMapping *network.UPnPMapping

	lastUpdate    time.Time      // last time Update() was called on the active game
	splashDone    chan struct{}   // closed to end splash phase early
	gameOverTimer chan struct{}   // closed to end game-over phase early
}

func New(address, password, dataDir string) (*Server, error) {
	app := &Server{
		state:    state.New(password),
		registry: newCommandRegistry(),
		dataDir:  dataDir,
		clock:    common.RealClock{},
		programs: make(map[string]*tea.Program),
		sessions: make(map[string]ssh.Session),
		logCh:    make(chan string, 256),
		chatCh:   make(chan common.Message, 256),
		slogCh:   make(chan console.SlogLine, 256),
	}

	app.registerBuiltins()
	engine.LoadFigletFonts(dataDir)

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

func (a *Server) SlogCh() <-chan console.SlogLine { return a.slogCh }
func (a *Server) ChatCh() <-chan common.Message {
	return a.chatCh
}

// --- Interface methods for chrome.ServerAPI and console.ServerAPI ---

func (a *Server) State() *state.CentralState { return a.state }
func (a *Server) Clock() common.Clock         { return a.clock }
func (a *Server) DataDir() string             { return a.dataDir }
func (a *Server) Uptime() string              { return a.uptime() }

func (a *Server) BroadcastChat(msg common.Message) { a.broadcastChat(msg) }
func (a *Server) BroadcastMsg(msg tea.Msg)          { a.broadcastMsg(msg) }
func (a *Server) SendToPlayer(playerID string, msg tea.Msg) { a.sendToPlayer(playerID, msg) }
func (a *Server) ServerLog(text string)              { a.serverLog(text) }
func (a *Server) KickPlayer(playerID string) error   { return a.kickPlayer(playerID) }

func (a *Server) TabCandidates(input string, playerNames []string) (string, []string) {
	return a.registry.TabCandidates(input, playerNames)
}
func (a *Server) DispatchCommand(input string, ctx common.CommandContext) {
	a.registry.Dispatch(input, ctx)
}
func (a *Server) SetConsoleWidth(w int) { a.consoleWidth = w }

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

// OpenChatLog derives the chat log path from NULL_SPACE_LOG_FILE by inserting
// "-chat" before the extension. E.g. "logs/20260401-152702.log" → "logs/20260401-152702-chat.log".
// If no log file is configured, no chat log is created.
func (a *Server) OpenChatLog() {
	serverLog := os.Getenv("NULL_SPACE_LOG_FILE")
	if serverLog == "" {
		return
	}
	ext := filepath.Ext(serverLog)
	path := strings.TrimSuffix(serverLog, ext) + "-chat" + ext
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Warn("failed to open chat log", "path", path, "error", err)
		return
	}
	a.chatLogFile = f
	slog.Info("chat log opened", "path", path)
}

// CloseChatLog closes the chat log file.
func (a *Server) CloseChatLog() {
	if a.chatLogFile != nil {
		a.chatLogFile.Close()
		a.chatLogFile = nil
	}
}

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

const joinScriptURL = "https://raw.githubusercontent.com/simonthoresen/null-space/main/join.ps1"

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

// LogInviteCommand writes the current invite command to the server log.
func (a *Server) LogInviteCommand() {
	a.serverLog("Invite:\n" + a.inviteCommand())
}

// LogGameList broadcasts the available game list to chat.
func (a *Server) LogGameList() {
	gamesDir := filepath.Join(a.dataDir, "games")
	available := engine.ListGames(gamesDir)
	if len(available) == 0 {
		a.broadcastChat(common.Message{Text: "No games found in games/"})
		return
	}
	a.broadcastChat(common.Message{Text: a.formatGameList(gamesDir, available)})
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
	model := chrome.NewModel(a, playerID)

	// Check for init commands and enhanced client flag from SSH env vars.
	for _, e := range sess.Environ() {
		if strings.HasPrefix(e, "NULL_SPACE_INIT=") {
			encoded := strings.TrimPrefix(e, "NULL_SPACE_INIT=")
			if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(decoded)), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						model.InitCommands = append(model.InitCommands, line)
					}
				}
			}
		}
		if e == "NULL_SPACE_CLIENT=enhanced" {
			model.IsEnhancedClient = true
		}
	}

	program := tea.NewProgram(model, a.sessionProgramOptions(sess)...)
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
	// Default to TrueColor if the client didn't send COLORTERM.
	// Most modern terminals (Windows Terminal, iTerm2, etc.) support it,
	// and without this the UI degrades to ugly ANSI-16 approximations.
	hasColorTerm := false
	for _, e := range envs {
		if strings.HasPrefix(e, "COLORTERM=") {
			hasColorTerm = true
			break
		}
	}
	if !hasColorTerm {
		envs = append(envs, "COLORTERM=truecolor")
	}
	cp := colorprofile.Env(envs)
	slog.Info("SSH session color profile", "profile", cp.String(), "envs_count", len(envs))
	opts := wishbubbletea.MakeOptions(sess)
	opts = append(opts,
		tea.WithFPS(60),
		tea.WithEnvironment(envs), // override MakeOptions' env to include COLORTERM
		tea.WithColorProfile(cp),
		tea.WithOutput(network.NewKittyStripWriter(sess)),
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

	a.broadcastMsg(common.TeamUpdatedMsg{})

	// Check if this player was disconnected from a running game.
	a.state.Lock()
	if oldID, ok := a.state.GameDisconnected[player.Name]; ok {
		a.state.ReplaceGamePlayerID(oldID, player.ID)
		delete(a.state.GameDisconnected, player.Name)
		game := a.state.ActiveGame
		a.state.Unlock()
		a.serverLog(fmt.Sprintf("player %s rejoined game (was %s, now %s)", player.Name, oldID, player.ID))
		// Refresh the teams cache so JS sees the updated player ID.
		if jrt, ok := game.(*engine.JSRuntime); ok {
			jrt.SetTeamsCache(a.buildTeamsCache())
		}
	} else {
		a.state.Unlock()
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
			a.state.Lock()
			a.state.GameDisconnected[player.Name] = playerID
			a.state.Unlock()
		}
	}

	// Always clean up lobby teams (game teams are a separate snapshot).
	a.state.RemovePlayerFromTeams(playerID)
	a.broadcastMsg(common.TeamUpdatedMsg{})

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
			a.state.Lock()
			a.state.TickN++
			n := a.state.TickN
			game := a.state.ActiveGame
			phase := a.state.GamePhase
			a.state.Unlock()

			// Call Update(dt) once per tick — before broadcast so game
			// state is fresh when players render.
			if game != nil && phase == common.PhasePlaying {
				now := a.clock.Now()
				dt := now.Sub(a.lastUpdate).Seconds()
				a.lastUpdate = now
				game.Update(dt)
			}

			a.broadcastMsg(common.TickMsg{N: n})

			// Check if JS called gameOver().
			a.checkGameOver()
		}
	}
}

// checkGameOver detects if the JS runtime signaled game over and initiates the transition.
func (a *Server) checkGameOver() {
	a.state.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	a.state.RUnlock()

	if game == nil || phase != common.PhasePlaying {
		return
	}
	rt, ok := game.(*engine.JSRuntime)
	if !ok || !rt.IsGameOverPending() {
		return
	}

	// Save state if the game passed one as the second arg to gameOver().
	gameOverState := rt.GameOverStateExport()

	a.state.RLock()
	gameName := a.state.GameName
	a.state.RUnlock()

	if gameOverState != nil {
		if err := state.SaveGameState(a.dataDir, gameName, gameOverState); err != nil {
			a.serverLog(fmt.Sprintf("warning: could not save game state: %v", err))
		} else {
			a.serverLog(fmt.Sprintf("game state saved: %s", gameName))
		}
	}

	a.state.SetGamePhase(common.PhaseGameOver)
	a.state.Lock()
	a.state.GameOverResults = rt.GameOverResults()
	a.state.Unlock()

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
	a.state.AddChat(msg)

	// Write to chat log file.
	if a.chatLogFile != nil {
		ts := time.Now().Format("2006-01-02 15:04:05")
		var line string
		switch {
		case msg.IsPrivate:
			line = fmt.Sprintf("%s [PM %s→%s] %s\n", ts, msg.FromID, msg.ToID, msg.Text)
		case msg.Author == "":
			line = fmt.Sprintf("%s [system] %s\n", ts, msg.Text)
		default:
			line = fmt.Sprintf("%s <%s> %s\n", ts, msg.Author, msg.Text)
		}
		_, _ = a.chatLogFile.WriteString(line)
	}

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

// ShowDialog sends a modal dialog request to the specified player's TUI program.
func (a *Server) ShowDialog(playerID string, d common.DialogRequest) {
	a.programsMu.Lock()
	prog := a.programs[playerID]
	a.programsMu.Unlock()
	if prog != nil {
		prog.Send(widget.ShowDialogMsg{Dialog: d})
	}
}

func (a *Server) serverLog(line string) {
	slog.Info(line)
}

// InstallConsoleSlogHandler wraps the current default slog handler to also
// route records to the server's slogCh. Call after server creation.
func (a *Server) InstallConsoleSlogHandler() {
	existing := slog.Default().Handler()
	handler := console.NewSlogHandler(a.slogCh, existing)
	slog.SetDefault(slog.New(handler))
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
	a.state.RLock()
	_, isReconnect := a.state.GameDisconnected[base]
	a.state.RUnlock()
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
	if network.IsURL(path) {
		if network.IsZipURL(path) {
			gamesDir := filepath.Join(a.dataDir, "games")
			local, err := network.DownloadAndExtractZip(path, gamesDir)
			if err != nil {
				return fmt.Errorf("download game zip: %w", err)
			}
			path = local
		} else {
			cacheDir := filepath.Join(a.dataDir, "games", ".cache")
			local, err := network.DownloadToCache(path, cacheDir)
			if err != nil {
				return fmt.Errorf("download game: %w", err)
			}
			path = local
		}
	}
	if a.state.ActiveGame != nil {
		a.unloadGame()
	}

	// Derive game name: for folder games (games/nethack/main.js) use the folder name,
	// for flat games (games/example.js) use the filename stem.
	name := strings.TrimSuffix(filepath.Base(path), ".js")
	if name == "main" {
		name = filepath.Base(filepath.Dir(path))
	}

	// Create a buffered channel for JS→server chat; drained by a goroutine below.
	gameChatCh := make(chan common.Message, 64)

	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return err
	}
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.ShowDialogFn = a.ShowDialog
	}

	// Validate team count against game's declared range.
	teams := a.state.GetTeams()
	tr := rt.TeamRange()
	teamCount := len(teams)
	if tr.Min > 0 && teamCount < tr.Min {
		close(gameChatCh)
		return fmt.Errorf("game requires at least %d teams (have %d)", tr.Min, teamCount)
	}
	if tr.Max > 0 && teamCount > tr.Max {
		close(gameChatCh)
		return fmt.Errorf("game supports at most %d teams (have %d)", tr.Max, teamCount)
	}

	a.state.Lock()
	// Snapshot teams for the game — lobby teams stay independent.
	a.state.GameTeams = teams
	a.state.GameDisconnected = make(map[string]string)
	a.state.ActiveGame = rt
	a.state.GameName = name
	a.state.GamePhase = common.PhaseSplash
	a.state.Unlock()

	// Populate the teams cache so JS teams() returns correct data.
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat messages on a background goroutine.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call init — teams() now returns game participants via cached snapshot.
	savedState, err := state.LoadGameState(a.dataDir, name)
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
	a.state.Lock()
	if a.state.GamePhase != common.PhaseSplash {
		a.state.Unlock()
		return
	}

	a.state.GamePhase = common.PhasePlaying
	game := a.state.ActiveGame
	a.state.Unlock()

	a.lastUpdate = a.clock.Now()
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

	a.state.Lock()
	game := a.state.ActiveGame
	if game == nil {
		a.state.Unlock()
		return // already unloaded
	}
	a.state.ActiveGame = nil
	a.state.GameName = ""
	a.state.GamePhase = common.PhaseNone
	a.state.GameOverReady = nil
	a.state.Unlock()

	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}
	game.Unload()

	// Close the JS chat channel so the drainer goroutine exits.
	if jrt, ok := game.(*engine.JSRuntime); ok && jrt.ChatCh != nil {
		close(jrt.ChatCh)
	}

	a.broadcastMsg(common.GameUnloadedMsg{})
	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhaseNone})
	a.broadcastChat(common.Message{Text: "Game unloaded."})
	a.serverLog("game unloaded")
}

// suspendGame suspends the active game, persisting its session state.
func (a *Server) suspendGame(saveName string) error {
	a.state.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	gameName := a.state.GameName
	a.state.RUnlock()

	if game == nil || phase != common.PhasePlaying {
		return fmt.Errorf("no game is currently playing")
	}
	if !game.CanSuspend() {
		return fmt.Errorf("this game does not support suspend/resume")
	}

	// Call suspend() on the game to get session state.
	sessionState := game.Suspend()

	// Build the save.
	a.state.RLock()
	teams := make([]common.Team, len(a.state.GameTeams))
	for i, t := range a.state.GameTeams {
		teams[i] = common.Team{
			Name:    t.Name,
			Color:   t.Color,
			Players: append([]string(nil), t.Players...),
		}
	}
	disc := make(map[string]string)
	for k, v := range a.state.GameDisconnected {
		disc[k] = v
	}
	a.state.RUnlock()

	save := &state.SuspendSave{
		GameName:     gameName,
		SaveName:     saveName,
		SavedAt:      a.clock.Now(),
		Teams:        teams,
		Disconnected: disc,
		GameState:    sessionState,
	}
	if err := state.SaveSuspend(a.dataDir, save); err != nil {
		return fmt.Errorf("save suspend state: %w", err)
	}

	// Unregister game commands.
	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}

	// Transition to suspended phase — runtime stays alive.
	a.state.Lock()
	a.state.GamePhase = common.PhaseSuspended
	a.state.Unlock()

	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhaseSuspended})
	a.broadcastMsg(common.GameSuspendedMsg{Name: gameName})
	a.broadcastChat(common.Message{Text: fmt.Sprintf("Game suspended: %s (save: %s)", gameName, saveName)})
	a.serverLog(fmt.Sprintf("game suspended: %s/%s", gameName, saveName))
	return nil
}

// resumeGame resumes a suspended game. If the runtime is still alive (warm),
// calls Resume(nil). If loading from disk (cold), loads the game JS fresh,
// calls init(globalState)+start(), then Resume(sessionState).
func (a *Server) resumeGame(gameName, saveName string) error {
	save, err := state.LoadSuspend(a.dataDir, gameName, saveName)
	if err != nil {
		return fmt.Errorf("load suspend save: %w", err)
	}

	// Validate team count against the lobby teams if this is a cold resume
	// (warm resume keeps the original game teams).
	a.state.RLock()
	currentGame := a.state.ActiveGame
	currentName := a.state.GameName
	currentPhase := a.state.GamePhase
	a.state.RUnlock()

	isWarm := currentGame != nil && currentName == gameName && currentPhase == common.PhaseSuspended

	if isWarm {
		// Warm resume — runtime is alive, just unpause.
		// Re-register game commands.
		for _, cmd := range currentGame.Commands() {
			a.registry.Register(cmd)
		}

		currentGame.Resume(nil)

		a.state.Lock()
		a.state.GamePhase = common.PhasePlaying
		a.state.Unlock()

		a.lastUpdate = a.clock.Now()
		a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhasePlaying})
		a.broadcastMsg(common.GameResumedMsg{Name: gameName})
		a.broadcastChat(common.Message{Text: fmt.Sprintf("Game resumed: %s", gameName)})
		a.serverLog(fmt.Sprintf("game resumed (warm): %s/%s", gameName, saveName))
		return nil
	}

	// Cold resume — load the game fresh and restore from save.
	if currentGame != nil {
		a.unloadGame()
	}

	// Validate team count.
	tr := common.TeamRange{} // will be updated after loading
	_ = tr

	gamesDir := filepath.Join(a.dataDir, "games")
	path := engine.ResolveGamePath(gamesDir, gameName)

	gameChatCh := make(chan common.Message, 64)
	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return fmt.Errorf("load game for resume: %w", err)
	}
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.ShowDialogFn = a.ShowDialog
	}

	// Validate team count against game's declared range.
	tr = rt.TeamRange()
	teamCount := len(save.Teams)
	if tr.Min > 0 && teamCount < tr.Min {
		close(gameChatCh)
		return fmt.Errorf("saved session has %d teams but game requires at least %d", teamCount, tr.Min)
	}
	if tr.Max > 0 && teamCount > tr.Max {
		close(gameChatCh)
		return fmt.Errorf("saved session has %d teams but game supports at most %d", teamCount, tr.Max)
	}

	// Restore teams from save.
	a.state.Lock()
	a.state.GameTeams = save.Teams
	a.state.GameDisconnected = save.Disconnected
	if a.state.GameDisconnected == nil {
		a.state.GameDisconnected = make(map[string]string)
	}
	a.state.ActiveGame = rt
	a.state.GameName = gameName
	a.state.GamePhase = common.PhasePlaying // skip splash
	a.state.Unlock()

	// Populate teams cache.
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call init with global saved state (high scores etc), then start, then resume.
	globalState, err := state.LoadGameState(a.dataDir, gameName)
	if err != nil {
		a.serverLog(fmt.Sprintf("warning: could not load global state: %v", err))
	}
	rt.Init(globalState)
	rt.Start()
	rt.Resume(save.GameState)

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.lastUpdate = a.clock.Now()
	a.broadcastMsg(common.GameLoadedMsg{Name: gameName})
	a.broadcastMsg(common.GamePhaseMsg{Phase: common.PhasePlaying})
	a.broadcastMsg(common.GameResumedMsg{Name: gameName})
	a.broadcastChat(common.Message{Text: fmt.Sprintf("Game resumed: %s (from save: %s)", gameName, saveName)})
	a.serverLog(fmt.Sprintf("game resumed (cold): %s/%s", gameName, saveName))

	// Clean up the suspend save after successful resume.
	if err := state.DeleteSuspend(a.dataDir, gameName, saveName); err != nil {
		a.serverLog(fmt.Sprintf("warning: could not delete suspend save: %v", err))
	}

	return nil
}

// SuspendGame exposes suspendGame to the chrome/command layer.
func (a *Server) SuspendGame(saveName string) error { return a.suspendGame(saveName) }

// ResumeGame exposes resumeGame to the chrome/command layer.
func (a *Server) ResumeGame(gameName, saveName string) error { return a.resumeGame(gameName, saveName) }

// ListSuspends returns all suspend saves for the resume menu.
func (a *Server) ListSuspends() []state.SuspendInfo { return state.ListSuspends(a.dataDir, "") }

// buildTeamsCache builds a pre-resolved teams snapshot for JS teams().
// Resolves player IDs to names so JSRuntime doesn't need CentralState access.
func (a *Server) buildTeamsCache() []map[string]any {
	teams := a.state.GetGameTeams()
	result := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		playerList := make([]any, 0, len(t.Players))
		for _, pid := range t.Players {
			entry := map[string]any{"id": pid}
			if p := a.state.GetPlayer(pid); p != nil {
				entry["name"] = p.Name
			} else {
				entry["name"] = pid
			}
			playerList = append(playerList, entry)
		}
		result = append(result, map[string]any{
			"name":    t.Name,
			"color":   t.Color,
			"players": playerList,
		})
	}
	return result
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
		if ctx.IsConsole {
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
			a.state.RLock()
			pw := a.state.AdminPassword
			a.state.RUnlock()
			if pw == "" {
				ctx.Reply("No admin password set. Ask the server operator to set one with /password.")
				return
			}
			if args[0] != pw {
				ctx.Reply("Invalid password.")
				return
			}
			if !ctx.IsConsole {
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
			a.state.Lock()
			a.state.AdminPassword = args[0]
			a.state.Unlock()
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
		Name:        "canvas",
		Description: "Canvas rendering. /canvas scale <n> | /canvas off | /canvas info",
		AdminOnly:   true,
		Complete: func(before []string) []string {
			if len(before) == 0 {
				return []string{"scale", "off", "info"}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) == 0 {
				args = []string{"info"}
			}
			switch args[0] {
			case "scale":
				if len(args) < 2 {
					ctx.Reply("Usage: /canvas scale <pixels-per-cell> (e.g. 4, 8, 16)")
					return
				}
				n, err := strconv.Atoi(args[1])
				if err != nil || n < 1 || n > 32 {
					ctx.Reply("Scale must be 1-32.")
					return
				}
				a.state.Lock()
				a.state.CanvasScale = n
				a.state.Unlock()
				// Estimate bandwidth for the console's dimensions.
				viewW := 120 // typical console width
				viewH := viewW * 9 / 16
				bw := common.CanvasBandwidthMbps(viewW, viewH, n, 10)
				ctx.Reply(fmt.Sprintf("Canvas scale set to %d (%dx%d px). ~%.1f Mbps/user at %dx%d viewport.",
					n, viewW*n, viewH*n, bw, viewW, viewH))
			case "off":
				a.state.Lock()
				a.state.CanvasScale = 0
				a.state.Unlock()
				ctx.Reply("Canvas rendering disabled.")
			case "info":
				a.state.RLock()
				scale := a.state.CanvasScale
				game := a.state.ActiveGame
				a.state.RUnlock()
				if scale == 0 {
					ctx.Reply("Canvas rendering: off. Use /canvas scale <n> to enable.")
					return
				}
				hasCanvas := game != nil && game.HasCanvasMode()
				viewW := 120
				viewH := viewW * 9 / 16
				bw := common.CanvasBandwidthMbps(viewW, viewH, scale, 10)
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
			default:
				ctx.Reply("Usage: /canvas scale <n> | /canvas off | /canvas info")
			}
		},
	})

	a.registry.Register(common.Command{
		Name:        "game",
		Description: "Game management. /game list|load|unload|suspend|resume",
		Complete: func(before []string) []string {
			switch len(before) {
			case 0:
				return []string{"list", "load", "unload", "suspend", "resume"}
			case 1:
				switch before[0] {
				case "load":
					return engine.ListGames(filepath.Join(a.dataDir, "games"))
				case "unload":
					a.state.RLock()
					name := a.state.GameName
					a.state.RUnlock()
					if name != "" {
						return []string{strings.TrimSuffix(filepath.Base(name), ".js")}
					}
				case "resume":
					return state.ListSuspendNames(a.dataDir)
				}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			gamesDir := filepath.Join(a.dataDir, "games")
			if len(args) == 0 {
				available := engine.ListGames(gamesDir)
				if len(available) == 0 {
					ctx.Reply("No games found in games/")
					return
				}
				ctx.Reply(a.formatGameList(gamesDir, available))
				return
			}
			switch args[0] {
			case "list":
				available := engine.ListGames(gamesDir)
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
				if network.IsURL(args[1]) {
					path = args[1]
				} else {
					path = engine.ResolveGamePath(filepath.Join(a.dataDir, "games"), args[1])
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
				a.state.RLock()
				active := a.state.GameName
				a.state.RUnlock()
				if active == "" {
					ctx.Reply("No game is currently loaded.")
					return
				}
				a.unloadGame()
			case "suspend":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				saveName := ""
				if len(args) >= 2 {
					saveName = args[1]
				}
				if saveName == "" {
					// Auto-generate a save name from the current time.
					saveName = a.clock.Now().Format("2006-01-02_15-04-05")
				}
				if err := a.suspendGame(saveName); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to suspend: %v", err))
					return
				}
			case "resume":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					// List available saves.
					saves := state.ListSuspends(a.dataDir, "")
					if len(saves) == 0 {
						ctx.Reply("No suspended games found. Usage: /game resume <gameName/saveName>")
						return
					}
					var lines []string
					lines = append(lines, "Suspended games:")
					for _, s := range saves {
						lines = append(lines, fmt.Sprintf("  %s/%s  (%d teams, %s)",
							s.GameName, s.SaveName, s.TeamCount, s.SavedAt.Format("Jan 2 15:04")))
					}
					lines = append(lines, "Usage: /game resume <gameName/saveName>")
					ctx.Reply(strings.Join(lines, "\n"))
					return
				}
				ref := args[1]
				parts := strings.SplitN(ref, "/", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					ctx.Reply("Usage: /game resume <gameName/saveName>")
					return
				}
				if err := a.resumeGame(parts[0], parts[1]); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to resume: %v", err))
					return
				}
			default:
				ctx.Reply(fmt.Sprintf("Unknown subcommand '%s'. Use: list, load, unload, suspend, resume", args[0]))
			}
		},
	})

	// /games → /game list, /load <name> → /game load <name>
	a.registry.Register(common.Command{
		Name:        "games",
		Description: "Alias for /game (list available games)",
		Handler: func(ctx common.CommandContext, args []string) {
			a.registry.Dispatch("/game "+strings.Join(args, " "), ctx)
		},
	})
	a.registry.Register(common.Command{
		Name:        "load",
		Description: "Alias for /game load <name>",
		AdminOnly:   true,
		Complete: func(before []string) []string {
			if len(before) == 0 {
				return engine.ListGames(filepath.Join(a.dataDir, "games"))
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			a.registry.Dispatch("/game load "+strings.Join(args, " "), ctx)
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
			fromName := "console"
			if !ctx.IsConsole {
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
		_ = tc.SetNoDelay(true)
	}
	return conn, nil
}
