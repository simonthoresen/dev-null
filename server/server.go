package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
)

type App struct {
	address       string
	gameName      string
	adminPassword string
	state         *CentralState
	server        *ssh.Server

	mu              sync.RWMutex
	programs        map[string]*tea.Program
	programInFlight map[*tea.Program]chan struct{}
	sessions        map[string]ssh.Session
	privateHistory  map[string][]string
	registry        map[string]common.Command
	paletteIndex    int
	consolePlayer   string
	consoleIdentity *common.Player
	playerActivity  map[string]time.Time
	consoleWriter   io.Writer
	consoleProgram  *tea.Program
	localLogs       []string
	publicIP        string
	tunnelAddress   string
	tunnelJoinCmd   string
	shutdownFn      context.CancelFunc
	consoleMu       sync.Mutex
}

const (
	gameTickInterval = 100 * time.Millisecond
	uiTickInterval   = 125 * time.Millisecond
	spinnerIdleAfter = 5 * time.Second
)

func New(address, gameName string, game common.Game, adminPassword string) (*App, error) {
	app := &App{
		address:         address,
		gameName:        gameName,
		adminPassword:   adminPassword,
		state:           newCentralState(game),
		programs:        make(map[string]*tea.Program),
		programInFlight: make(map[*tea.Program]chan struct{}),
		sessions:        make(map[string]ssh.Session),
		privateHistory:  make(map[string][]string),
		playerActivity:  make(map[string]time.Time),
		localLogs:       make([]string, 0, 100),
	}
	app.publicIP = detectPublicIP()
	app.registerCommands(game.GetCommands())

	server, err := wish.NewServer(
		ssh.AllocatePty(),
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
	app.server = server
	slog.Info("server app created", "address", address, "game", gameName)

	for _, cmd := range game.Init() {
		app.executeCmd(cmd, "")
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go a.runTicker(ctx)
	slog.Info("server listen loop starting", "address", a.address)
	go func() {
		ln, err := newNoDelayListener(a.address)
		if err != nil {
			errCh <- err
			return
		}
		slog.Info("TCP_NODELAY listener ready", "address", a.address)
		err = a.server.Serve(ln)
		if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		slog.Info("server context cancelled, shutting down")
		return a.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = ctx
	slog.Info("server shutdown requested")
	if err := a.server.Close(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		slog.Error("server shutdown failed", "error", err)
		return err
	}
	slog.Info("server shutdown completed")
	return nil
}

func (a *App) SetShutdownFunc(shutdownFn context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.shutdownFn = shutdownFn
}

func (a *App) sessionMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			player := a.registerSession(sess)
			defer a.unregisterSession(player.ID)
			next(sess)
		}
	}
}

func (a *App) programHandler(sess ssh.Session) *tea.Program {
	playerID := sess.Context().SessionID()
	program := tea.NewProgram(newChromeModel(a, playerID), a.sessionProgramOptions(sess)...)
	a.mu.Lock()
	a.programs[playerID] = program
	a.registerProgramLocked(program)
	a.mu.Unlock()
	return program
}

func (a *App) sessionProgramOptions(sess ssh.Session) []tea.ProgramOption {
	envs := sess.Environ()
	if pty, _, ok := sess.Pty(); ok && pty.Term != "" {
		envs = append(envs, "TERM="+pty.Term)
	}

	// Wish does not currently force a color profile for Windows-hosted SSH
	// sessions, so derive it from the remote environment here.
	return append(wishbubbletea.MakeOptions(sess), tea.WithFPS(60), tea.WithColorProfile(colorprofile.Env(envs)))
}

func (a *App) registerSession(sess ssh.Session) *common.Player {
	player := &common.Player{
		ID:          sess.Context().SessionID(),
		Name:        a.uniqueName(sess.User()),
		Position:    common.Point{X: 100, Y: 100},
		Color:       a.nextColor(),
		ConnectedAt: time.Now(),
	}

	a.mu.Lock()
	a.sessions[player.ID] = sess
	a.privateHistory[player.ID] = nil
	a.playerActivity[player.ID] = time.Now()
	a.mu.Unlock()

	a.state.AddPlayer(player)
	slog.Info("player joined", "player_id", player.ID, "name", player.Name)
	a.appendChatLine(formatSystemLine(fmt.Sprintf("%s joined the tunnel.", player.Name)))
	a.handleGameMessage(common.PlayerJoinedMsg{
		PlayerID: player.ID,
		Name:     player.Name,
		Position: player.Position,
		Color:    player.Color,
	}, player.ID)
	a.broadcast(common.RefreshMsg{})
	return player
}

func (a *App) unregisterSession(playerID string) {
	player := a.state.GetPlayer(playerID)
	if player != nil {
		slog.Info("player left", "player_id", playerID, "name", player.Name)
		a.appendChatLine(formatSystemLine(fmt.Sprintf("%s left the tunnel.", player.Name)))
	}
	a.handleGameMessage(common.PlayerLeftMsg{PlayerID: playerID}, playerID)
	a.state.RemovePlayer(playerID)

	a.mu.Lock()
	program := a.programs[playerID]
	delete(a.programs, playerID)
	delete(a.sessions, playerID)
	delete(a.privateHistory, playerID)
	delete(a.playerActivity, playerID)
	a.unregisterProgramLocked(program)
	a.mu.Unlock()

	a.broadcast(common.RefreshMsg{})
}

func (a *App) runTicker(ctx context.Context) {
	gameTicker := time.NewTicker(gameTickInterval)
	uiTicker := time.NewTicker(uiTickInterval)
	defer gameTicker.Stop()
	defer uiTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tick := <-gameTicker.C:
			a.handleGameMessage(common.TickMsg{Time: tick}, "")
		case tick := <-uiTicker.C:
			a.broadcastIdleTick(tick)
		}
	}
}

func (a *App) broadcastIdleTick(tick time.Time) {
	a.mu.RLock()
	programs := make(map[string]*tea.Program, len(a.programs))
	for playerID, program := range a.programs {
		programs[playerID] = program
	}
	activity := make(map[string]time.Time, len(a.playerActivity))
	for playerID, at := range a.playerActivity {
		activity[playerID] = at
	}
	consoleProgram := a.consoleProgram
	a.mu.RUnlock()

	for playerID, program := range programs {
		lastActivity, ok := activity[playerID]
		if !ok || tick.Sub(lastActivity) < spinnerIdleAfter {
			continue
		}
		a.sendProgram(program, common.TickMsg{Time: tick})
	}

	if consoleProgram != nil {
		a.sendProgram(consoleProgram, common.TickMsg{Time: tick})
	}
}

func (a *App) handleGameMessage(msg tea.Msg, playerID string) {
	cmds := a.state.ActiveGame.Update(msg, playerID)
	for _, cmd := range cmds {
		a.executeCmd(cmd, playerID)
	}
}

func (a *App) executeCmd(cmd tea.Cmd, playerID string) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg == nil {
			return
		}
		a.handleGameMessage(msg, playerID)
		a.broadcast(common.RefreshMsg{})
	}()
}

func (a *App) broadcast(msg tea.Msg) {
	a.mu.RLock()
	programs := make([]*tea.Program, 0, len(a.programs))
	playerIDs := make([]string, 0, len(a.programs))
	for _, program := range a.programs {
		programs = append(programs, program)
	}
	for playerID := range a.programs {
		playerIDs = append(playerIDs, playerID)
	}
	consoleProgram := a.consoleProgram
	a.mu.RUnlock()

	a.notePlayersActivity(playerIDs...)

	for _, program := range programs {
		a.sendProgram(program, msg)
	}

	if consoleProgram != nil {
		a.sendProgram(consoleProgram, msg)
	}
}

func (a *App) broadcastExcept(excludedPlayerID string, msg tea.Msg) {
	a.mu.RLock()
	programs := make([]*tea.Program, 0, len(a.programs))
	playerIDs := make([]string, 0, len(a.programs))
	for playerID, program := range a.programs {
		if playerID == excludedPlayerID {
			continue
		}
		programs = append(programs, program)
		playerIDs = append(playerIDs, playerID)
	}
	consoleProgram := a.consoleProgram
	a.mu.RUnlock()

	a.notePlayersActivity(playerIDs...)

	for _, program := range programs {
		a.sendProgram(program, msg)
	}

	if consoleProgram != nil {
		a.sendProgram(consoleProgram, msg)
	}
}

func (a *App) addSystemMessage(text string) {
	a.appendChatLine(formatSystemLine(text))
	a.broadcast(common.RefreshMsg{})
}

func (a *App) addPinggyMessage(text string) {
	slog.Info("pinggy message", "text", text)
	a.writeConsoleLine(formatPinggyLine(text))
}

func (a *App) addPrivateMessage(playerID, text string) {
	slog.Debug("private message", "player_id", playerID, "text", text)
	line := formatPrivateLine(text)
	a.mu.Lock()
	a.privateHistory[playerID] = append(a.privateHistory[playerID], line)
	if len(a.privateHistory[playerID]) > 20 {
		a.privateHistory[playerID] = append([]string(nil), a.privateHistory[playerID][len(a.privateHistory[playerID])-20:]...)
	}
	consolePlayer := a.consolePlayer
	a.mu.Unlock()
	if playerID == consolePlayer {
		a.writeConsoleLine(line)
	}
	a.sendToPlayer(playerID, common.RefreshMsg{})
}

func (a *App) addChatMessage(playerID, text string) {
	player := a.playerIdentity(playerID)
	author := "unknown"
	if player != nil {
		author = player.Name
	}
	slog.Info("chat message", "player_id", playerID, "author", author, "text", text)
	a.appendChatLine(formatChatLine(author, text))
	a.broadcast(common.RefreshMsg{})
}

func (a *App) playerIdentity(playerID string) *common.Player {
	if player := a.state.GetPlayer(playerID); player != nil {
		return player
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.consoleIdentity != nil && a.consoleIdentity.ID == playerID {
		clone := *a.consoleIdentity
		return &clone
	}
	return nil
}

func (a *App) appendChatLine(line string) {
	a.state.AppendChat(line)
	a.notifyLocalConsole()
}

func (a *App) writeConsoleLine(line string) {
	a.mu.Lock()
	a.localLogs = append(a.localLogs, line)
	if len(a.localLogs) > 100 {
		a.localLogs = append([]string(nil), a.localLogs[len(a.localLogs)-100:]...)
	}
	program := a.consoleProgram
	writer := a.consoleWriter
	a.mu.Unlock()

	if program != nil {
		a.sendProgram(program, common.RefreshMsg{})
		return
	}
	if writer == nil {
		return
	}

	a.consoleMu.Lock()
	defer a.consoleMu.Unlock()
	_, _ = fmt.Fprintln(writer, line)
}

func (a *App) notifyLocalConsole() {
	a.mu.RLock()
	program := a.consoleProgram
	a.mu.RUnlock()
	if program != nil {
		a.sendProgram(program, common.RefreshMsg{})
	}
}

func (a *App) requestShutdown() bool {
	a.mu.RLock()
	shutdownFn := a.shutdownFn
	a.mu.RUnlock()
	if shutdownFn == nil {
		return false
	}
	shutdownFn()
	return true
}

func (a *App) renderGlobalChat() string {
	lines := a.state.ChatLines()
	if len(lines) == 0 {
		return "[system] chat is quiet"
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderLocalLogs() string {
	a.mu.RLock()
	lines := append([]string(nil), a.localLogs...)
	a.mu.RUnlock()
	if len(lines) == 0 {
		return formatPrivateLine("server console ready")
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderChatForPlayer(playerID string) string {
	globalLines := a.state.ChatLines()
	a.mu.RLock()
	privateLines := append([]string(nil), a.privateHistory[playerID]...)
	a.mu.RUnlock()
	lines := append(globalLines, privateLines...)
	if len(lines) == 0 {
		return "[system] chat is quiet"
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderGame(playerID string, width, height int) string {
	return a.state.ActiveGame.View(playerID, width, height)
}

func (a *App) listenPort() string {
	if a.address != "" {
		parts := strings.SplitN(a.address, ":", 2)
		if len(parts) == 2 && parts[1] != "" {
			return parts[1]
		}
	}
	return "23234"
}

const sshNoHostCheck = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

func (a *App) localSSHCommand() string {
	host := "localhost"
	if a.address != "" {
		parts := strings.SplitN(a.address, ":", 2)
		if len(parts) == 2 && parts[0] != "" {
			host = parts[0]
		}
	}
	return fmt.Sprintf("ssh -t -p %s %s %s", a.listenPort(), sshNoHostCheck, host)
}

func (a *App) directSSHCommand() string {
	if a.publicIP == "" {
		return ""
	}
	return fmt.Sprintf("ssh -t -p %s %s %s", a.listenPort(), sshNoHostCheck, a.publicIP)
}

func (a *App) smartConnectOneLiner() string {
	direct := a.directSSHCommand()
	a.mu.RLock()
	relay := a.tunnelJoinCmd
	a.mu.RUnlock()
	if relay != "" {
		relay = ensureSSHFlag(relay, "-t")
	}
	if direct != "" && relay != "" {
		return fmt.Sprintf("%s; if($LASTEXITCODE -ne 0){%s}", direct, relay)
	}
	if direct != "" {
		return direct
	}
	if relay != "" {
		return relay
	}
	return a.localSSHCommand()
}

func (a *App) connectionInfo() (localCmd, directCmd, tunnelJoin, oneLiner string) {
	localCmd = a.localSSHCommand()
	directCmd = a.directSSHCommand()
	a.mu.RLock()
	tunnelJoin = a.tunnelJoinCmd
	a.mu.RUnlock()
	oneLiner = a.smartConnectOneLiner()
	return
}

func (a *App) sendToPlayer(playerID string, msg tea.Msg) {
	a.notePlayerActivity(playerID)
	a.mu.RLock()
	program := a.programs[playerID]
	a.mu.RUnlock()
	if program != nil {
		a.sendProgram(program, msg)
	}
}

func (a *App) notePlayerActivity(playerID string) {
	if playerID == "" {
		return
	}
	a.mu.Lock()
	if _, ok := a.playerActivity[playerID]; ok {
		a.playerActivity[playerID] = time.Now()
	}
	a.mu.Unlock()
}

func (a *App) notePlayersActivity(playerIDs ...string) {
	if len(playerIDs) == 0 {
		return
	}
	now := time.Now()
	a.mu.Lock()
	for _, playerID := range playerIDs {
		if _, ok := a.playerActivity[playerID]; ok {
			a.playerActivity[playerID] = now
		}
	}
	a.mu.Unlock()
}

func (a *App) sendProgram(program *tea.Program, msg tea.Msg) {
	if program == nil {
		return
	}

	if _, ok := msg.(tea.QuitMsg); ok {
		go program.Send(msg)
		return
	}

	a.mu.RLock()
	inFlight := a.programInFlight[program]
	a.mu.RUnlock()
	if inFlight == nil {
		go program.Send(msg)
		return
	}

	if !isCoalescableUpdate(msg) {
		go program.Send(msg)
		return
	}

	select {
	case inFlight <- struct{}{}:
		go func() {
			defer func() { <-inFlight }()
			program.Send(msg)
		}()
	default:
		return
	}
}

func (a *App) registerProgramLocked(program *tea.Program) {
	if program == nil {
		return
	}
	if _, exists := a.programInFlight[program]; exists {
		return
	}
	a.programInFlight[program] = make(chan struct{}, 1)
}

func (a *App) unregisterProgramLocked(program *tea.Program) {
	if program == nil {
		return
	}
	if _, exists := a.programInFlight[program]; !exists {
		return
	}
	delete(a.programInFlight, program)
}

func isCoalescableUpdate(msg tea.Msg) bool {
	switch msg.(type) {
	case common.TickMsg:
		return true
	default:
		return false
	}
}

func (a *App) kickPlayer(playerID string) error {
	a.mu.RLock()
	sess := a.sessions[playerID]
	a.mu.RUnlock()
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	return sess.Close()
}

func (a *App) uptime() string {
	duration := time.Since(a.state.StartTime).Truncate(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func (a *App) uniqueName(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = "pilot"
	}
	name := base
	index := 2
	for a.state.PlayerByName(name) != nil {
		name = fmt.Sprintf("%s-%d", base, index)
		index++
	}
	return name
}

func (a *App) nextColor() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	palette := []string{"#FF6B6B", "#4ECDC4", "#FFD166", "#7B9ACC", "#95D67B", "#F4A261"}
	color := palette[a.paletteIndex%len(palette)]
	a.paletteIndex++
	return color
}

func maxInt(aValue, bValue int) int {
	if aValue > bValue {
		return aValue
	}
	return bValue
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
