package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"

	"null-space/common"
)

type App struct {
	address       string
	gameName      string
	adminPassword string
	state         *CentralState
	server        *ssh.Server

	mu             sync.RWMutex
	programs       map[string]*tea.Program
	sessions       map[string]ssh.Session
	privateHistory map[string][]string
	registry       map[string]common.Command
	paletteIndex   int
	consolePlayer  string
	consoleWriter  io.Writer
	consoleProgram *tea.Program
	localLogs      []string
	consoleMu      sync.Mutex
}

func New(address, gameName string, game common.Game, adminPassword string) (*App, error) {
	app := &App{
		address:        address,
		gameName:       gameName,
		adminPassword:  adminPassword,
		state:          newCentralState(game),
		programs:       make(map[string]*tea.Program),
		sessions:       make(map[string]ssh.Session),
		privateHistory: make(map[string][]string),
		localLogs:      make([]string, 0, 100),
	}
	app.registerCommands(game.GetCommands())

	server, err := wish.NewServer(
		ssh.AllocatePty(),
		wish.WithAddress(address),
		wish.WithHostKeyPath("null-space_ed25519"),
		wish.WithMiddleware(
			wishbubbletea.MiddlewareWithProgramHandler(app.programHandler),
			app.sessionMiddleware(),
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		return nil, err
	}
	app.server = server

	for _, cmd := range game.Init() {
		app.executeCmd(cmd, "")
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go a.runTicker(ctx)
	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	if err := a.server.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		return err
	}
	return nil
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
	program := tea.NewProgram(newChromeModel(a, playerID), wishbubbletea.MakeOptions(sess)...)
	a.mu.Lock()
	a.programs[playerID] = program
	a.mu.Unlock()
	return program
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
	a.mu.Unlock()

	a.state.AddPlayer(player)
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
		a.appendChatLine(formatSystemLine(fmt.Sprintf("%s left the tunnel.", player.Name)))
	}
	a.handleGameMessage(common.PlayerLeftMsg{PlayerID: playerID}, playerID)
	a.state.RemovePlayer(playerID)

	a.mu.Lock()
	delete(a.programs, playerID)
	delete(a.sessions, playerID)
	delete(a.privateHistory, playerID)
	a.mu.Unlock()

	a.broadcast(common.RefreshMsg{})
}

func (a *App) runTicker(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tick := <-ticker.C:
			a.handleGameMessage(common.TickMsg{Time: tick}, "")
			a.broadcast(common.TickMsg{Time: tick})
		}
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
	for _, program := range a.programs {
		programs = append(programs, program)
	}
	a.mu.RUnlock()

	for _, program := range programs {
		program.Send(msg)
	}
}

func (a *App) addSystemMessage(text string) {
	a.appendChatLine(formatSystemLine(text))
	a.broadcast(common.RefreshMsg{})
}

func (a *App) addPinggyMessage(text string) {
	a.writeConsoleLine(formatPinggyLine(text))
}

func (a *App) addPrivateMessage(playerID, text string) {
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
	player := a.state.GetPlayer(playerID)
	author := "unknown"
	if player != nil {
		author = player.Name
	}
	a.appendChatLine(formatChatLine(author, text))
	a.broadcast(common.RefreshMsg{})
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
		program.Send(common.RefreshMsg{})
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
		program.Send(common.RefreshMsg{})
	}
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

func (a *App) sendToPlayer(playerID string, msg tea.Msg) {
	a.mu.RLock()
	program := a.programs[playerID]
	a.mu.RUnlock()
	if program != nil {
		program.Send(msg)
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
