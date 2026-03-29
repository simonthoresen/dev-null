package server

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

const localConsolePlayerID = "local-admin"

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
	a.consoleWriter = writer
	a.privateHistory[player.ID] = nil
	a.mu.Unlock()

	a.state.AddPlayer(player)
	a.handleGameMessage(common.PlayerJoinedMsg{
		PlayerID: player.ID,
		Name:     player.Name,
		Position: player.Position,
		Color:    player.Color,
	}, player.ID)
	a.addSystemMessage("admin joined from the local console.")
	a.writeConsoleLine(formatPrivateLine("local admin console ready. Type chat text or /help for commands. Press Ctrl+C to stop."))

	model := newLocalConsoleModel(a, player.ID, cancel)
	program := tea.NewProgram(model, tea.WithInput(reader), tea.WithOutput(writer))

	a.mu.Lock()
	a.consoleProgram = program
	a.mu.Unlock()

	go func() {
		<-ctx.Done()
		program.Send(tea.QuitMsg{})
	}()

	go func() {
		_, err := program.Run()

		a.mu.Lock()
		if a.consoleProgram == program {
			a.consoleProgram = nil
		}
		a.mu.Unlock()

		if err != nil && ctx.Err() == nil {
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

	header := lipgloss.NewStyle().Width(m.width).Reverse(true).Render(truncateRunes(
		fmt.Sprintf("[null-space server] | Game: %s | Players: %d | Tunnel: %s", m.app.gameName, m.app.state.PlayerCount(), m.app.uptime()),
		m.width,
	))

	availableHeight := maxInt(6, m.height-4)
	logsHeight := maxInt(3, availableHeight/2)
	chatHeight := maxInt(3, availableHeight-logsHeight)

	logsLabel := lipgloss.NewStyle().Bold(true).Width(m.width).Render("Local Output")
	chatLabel := lipgloss.NewStyle().Bold(true).Width(m.width).Render("Global Chat")
	logsView := fitBlock(m.logs.View(), m.width, logsHeight)
	chatView := fitBlock(m.chat.View(), m.width, chatHeight)
	inputView := fitBlock(m.input.View(), m.width, 1)

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
		m.app.executeCommand(m.playerID, text)
		m.sync()
		return
	}
	m.app.addChatMessage(m.playerID, text)
	m.sync()
}

func (a *App) disableLocalConsole(playerID string, announce bool) {
	player := a.state.GetPlayer(playerID)
	if player == nil {
		return
	}

	if announce {
		a.appendChatLine(formatSystemLine(fmt.Sprintf("%s left the local console.", player.Name)))
	}

	a.handleGameMessage(common.PlayerLeftMsg{PlayerID: playerID}, playerID)
	a.state.RemovePlayer(playerID)

	a.mu.Lock()
	if a.consolePlayer == playerID {
		a.consolePlayer = ""
		a.consoleWriter = nil
		a.consoleProgram = nil
	}
	delete(a.privateHistory, playerID)
	a.mu.Unlock()

	a.broadcast(common.RefreshMsg{})
}
