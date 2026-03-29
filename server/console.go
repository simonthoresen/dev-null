package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

const localConsolePlayerID = "local-admin"

var (
	localConsoleHeaderStyle  = lipgloss.NewStyle().Width(0).Background(lipgloss.Color("#B9D6F2")).Foreground(lipgloss.Color("#102A43")).Bold(true)
	localConsoleSectionStyle = lipgloss.NewStyle().Width(0).Background(lipgloss.Color("#D9EAF7")).Foreground(lipgloss.Color("#16324F")).Bold(true)
	localConsoleBodyStyle    = lipgloss.NewStyle().Width(0).Background(lipgloss.Color("#EEF6FC")).Foreground(lipgloss.Color("#16324F"))
)

type localConsoleModel struct {
	app      *App
	playerID string
	cancel   context.CancelFunc
	width    int
	height   int
	logs     viewport.Model
	chat     viewport.Model
	input    textinput.Model
}

func (a *App) EnableLocalConsole(ctx context.Context, cancel context.CancelFunc, reader io.Reader, writer io.Writer) {
	player := &common.Player{
		ID:          localConsolePlayerID,
		Name:        "admin",
		Position:    common.Point{X: 100, Y: 100},
		IsAdmin:     true,
		Color:       "#FFFFFF",
		ConnectedAt: time.Now(),
	}

	a.mu.Lock()
	a.consolePlayer = player.ID
	a.consoleIdentity = player
	a.consoleWriter = writer
	a.privateHistory[player.ID] = nil
	a.mu.Unlock()

	slog.Info("local console enabled", "player_id", player.ID, "name", player.Name)
	a.addSystemMessage("admin joined from the local console.")
	a.writeConsoleLine(formatPrivateLine("local admin console ready. Type chat text or /help for commands. Press Ctrl+C to stop."))

	model := newLocalConsoleModel(a, player.ID, cancel)
	program := tea.NewProgram(model, tea.WithInput(reader), tea.WithOutput(writer))

	a.mu.Lock()
	a.consoleProgram = program
	a.registerProgramLocked(program)
	a.mu.Unlock()

	go func() {
		<-ctx.Done()
		a.sendProgram(program, tea.QuitMsg{})
	}()

	go func() {
		_, err := program.Run()

		a.mu.Lock()
		if a.consoleProgram == program {
			a.consoleProgram = nil
		}
		a.unregisterProgramLocked(program)
		a.mu.Unlock()

		if err != nil && ctx.Err() == nil {
			slog.Error("local console program failed", "error", err)
			a.writeConsoleLine(formatPrivateLine(fmt.Sprintf("console error: %v", err)))
		}

		if ctx.Err() == nil {
			cancel()
		}

		a.disableLocalConsole(player.ID, false)
	}()
}

func newLocalConsoleModel(app *App, playerID string, cancel context.CancelFunc) localConsoleModel {
	logs := viewport.New(viewport.WithWidth(80), viewport.WithHeight(8))
	logs.SoftWrap = true
	logs.MouseWheelEnabled = false

	chat := viewport.New(viewport.WithWidth(80), viewport.WithHeight(8))
	chat.SoftWrap = true
	chat.MouseWheelEnabled = false

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Type global chat text or /command"
	input.CharLimit = 256
	input.SetWidth(78)
	input.Focus()

	model := localConsoleModel{
		app:      app,
		playerID: playerID,
		cancel:   cancel,
		logs:     logs,
		chat:     chat,
		input:    input,
	}
	model.sync()
	return model
}

func (m localConsoleModel) Init() tea.Cmd {
	return nil
}

func (m localConsoleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(40, msg.Width)
		m.height = maxInt(12, msg.Height)
		m.resize()
		m.sync()
		return m, nil
	case common.RefreshMsg, common.TickMsg:
		m.sync()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
			m.submitInput()
			return m, nil
		case "esc":
			m.input.SetValue("")
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m localConsoleModel) View() tea.View {
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading local server console...")
		view.AltScreen = true
		return view
	}

	header := localConsoleHeaderStyle.Width(m.width).Render(headerWithSpinner(
		fmt.Sprintf("[null-space server] | Game: %s | Players: %d | Tunnel: %s", m.app.gameName, m.app.state.PlayerCount(), m.app.uptime()),
		m.width,
		currentSpinnerFrame(),
	))

	availableHeight := maxInt(6, m.height-4)
	logsHeight := maxInt(3, availableHeight/2)
	chatHeight := maxInt(3, availableHeight-logsHeight)

	logsLabel := fitStyledBlock("Local Output", m.width, 1, localConsoleSectionStyle)
	chatLabel := fitStyledBlock("Global Chat", m.width, 1, chatChromeStyle)
	logsView := fitStyledBlock(m.logs.View(), m.width, logsHeight, localConsoleBodyStyle)
	chatView := fitStyledBlock(m.chat.View(), m.width, chatHeight, chatStyle)
	inputView := fitStyledBlock(m.input.View(), m.width, 1, localConsoleSectionStyle)

	view.SetContent(lipgloss.JoinVertical(lipgloss.Left, header, logsLabel, logsView, chatLabel, chatView, inputView))
	view.AltScreen = true
	return view
}

func (m *localConsoleModel) resize() {
	availableHeight := maxInt(6, m.height-4)
	logsHeight := maxInt(3, availableHeight/2)
	chatHeight := maxInt(3, availableHeight-logsHeight)

	m.logs.SetWidth(m.width)
	m.logs.SetHeight(logsHeight)
	m.chat.SetWidth(m.width)
	m.chat.SetHeight(chatHeight)
	m.input.SetWidth(maxInt(1, m.width-2))
}

func (m *localConsoleModel) sync() {
	m.logs.SetContent(m.app.renderLocalLogs())
	m.logs.GotoBottom()
	m.chat.SetContent(m.app.renderGlobalChat())
	m.chat.GotoBottom()
}

func (m *localConsoleModel) submitInput() {
	text := m.input.Value()
	m.input.SetValue("")
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		slog.Info("local console command input", "text", text)
		m.app.executeCommand(m.playerID, text)
		m.sync()
		return
	}
	slog.Info("local console chat input", "text", text)
	m.app.addChatMessage(m.playerID, text)
	m.sync()
}

func (a *App) disableLocalConsole(playerID string, announce bool) {
	player := a.playerIdentity(playerID)
	if player == nil {
		return
	}

	if announce {
		slog.Info("local console disabled", "player_id", playerID)
		a.appendChatLine(formatSystemLine(fmt.Sprintf("%s left the local console.", player.Name)))
	}

	a.mu.Lock()
	if a.consolePlayer == playerID {
		a.unregisterProgramLocked(a.consoleProgram)
		a.consolePlayer = ""
		a.consoleIdentity = nil
		a.consoleWriter = nil
		a.consoleProgram = nil
	}
	delete(a.privateHistory, playerID)
	a.mu.Unlock()

	a.broadcast(common.RefreshMsg{})
}
