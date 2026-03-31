package server

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

// Console uses the same lobby palettes: blue for server log, warm for chat.
var (
	consoleLogBarStyle  = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg).Bold(true)
	consoleLogBodyStyle = lipgloss.NewStyle().Background(lobbyTeamActiveBg).Foreground(lobbyTeamFg)
	consoleChatBarStyle = lipgloss.NewStyle().Background(lobbyChatBarActiveBg).Foreground(lobbyChatBarActiveFg).Bold(true)
	consoleChatStyle    = lipgloss.NewStyle().Background(lobbyChatActiveBg).Foreground(lobbyChatFg)
	consoleCmdStyle     = lipgloss.NewStyle().Background(lobbyChatBarActiveBg).Foreground(lobbyChatBarActiveFg)
)

type consoleModel struct {
	app    *Server
	cancel context.CancelFunc
	width  int
	height int

	logs  viewport.Model
	chat  viewport.Model
	input textinput.Model

	logLines  []string
	chatLines []string

	tabPrefix     string
	tabCandidates []string
	tabIndex      int
}

func NewConsoleModel(app *Server, cancel context.CancelFunc) *consoleModel {
	logs := viewport.New(viewport.WithWidth(80), viewport.WithHeight(8))
	logs.SoftWrap = true
	logs.MouseWheelEnabled = false

	chat := viewport.New(viewport.WithWidth(80), viewport.WithHeight(8))
	chat.SoftWrap = true
	chat.MouseWheelEnabled = false

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	input.Focus()

	return &consoleModel{
		app:    app,
		cancel: cancel,
		logs:   logs,
		chat:   chat,
		input:  input,
	}
}

func (m *consoleModel) Init() tea.Cmd {
	return listenForLogs(m.app.LogCh(), m.app.ChatCh())
}

func (m *consoleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(12, msg.Height)
		m.app.consoleWidth = m.width
		m.resize()
		return m, nil

	case common.TickMsg:
		// re-render for spinner update
		return m, nil

	case logLineMsg:
		m.logLines = append(m.logLines, string(msg))
		if len(m.logLines) > 500 {
			m.logLines = m.logLines[len(m.logLines)-500:]
		}
		m.logs.SetContent(strings.Join(m.logLines, "\n"))
		m.logs.GotoBottom()
		return m, listenForLogs(m.app.LogCh(), m.app.ChatCh())

	case chatLineMsg:
		chatMsg := common.Message(msg)
		var line string
		switch {
		case chatMsg.IsReply:
			line = chatMsg.Text
		case chatMsg.IsPrivate:
			fromName := chatMsg.FromID
			if p := m.app.state.GetPlayer(fromName); p != nil {
				fromName = p.Name
			}
			if fromName == "" {
				fromName = "admin"
			}
			toName := chatMsg.ToID
			if p := m.app.state.GetPlayer(toName); p != nil {
				toName = p.Name
			}
			if toName == "" {
				toName = "console"
			}
			line = fmt.Sprintf("[PM %s→%s] %s", fromName, toName, chatMsg.Text)
		case chatMsg.Author == "":
			line = fmt.Sprintf("[system] %s", chatMsg.Text)
		default:
			line = fmt.Sprintf("<%s> %s", chatMsg.Author, chatMsg.Text)
		}
		m.chatLines = append(m.chatLines, line)
		if len(m.chatLines) > 500 {
			m.chatLines = m.chatLines[len(m.chatLines)-500:]
		}
		m.chat.SetContent(strings.Join(m.chatLines, "\n"))
		m.chat.GotoBottom()
		return m, listenForLogs(m.app.LogCh(), m.app.ChatCh())

	case common.GamePhaseMsg, common.GameLoadedMsg, common.GameUnloadedMsg, common.TeamUpdatedMsg, common.PlayerJoinedMsg, common.PlayerLeftMsg:
		// These trigger re-render (status bar updates with phase/player count).
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
			m.tabCandidates = nil
			m.submitInput()
			return m, nil
		case "esc":
			m.tabCandidates = nil
			m.input.SetValue("")
			return m, nil
		case "tab":
			if strings.HasPrefix(m.input.Value(), "/") {
				if m.tabCandidates == nil {
					m.tabPrefix, m.tabCandidates = m.app.registry.TabCandidates(m.input.Value(), m.app.state.PlayerNames())
					m.tabIndex = 0
				}
				if len(m.tabCandidates) > 0 {
					m.input.SetValue(m.tabPrefix + m.tabCandidates[m.tabIndex])
					m.input.CursorEnd()
					m.tabIndex = (m.tabIndex + 1) % len(m.tabCandidates)
				}
			}
			return m, nil
		default:
			m.tabCandidates = nil
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// Forward to textinput for cursor blink etc.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *consoleModel) View() tea.View {
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading server console...")
		view.AltScreen = true
		return view
	}

	m.app.state.mu.RLock()
	gameName := m.app.state.GameName
	phase := m.app.state.GamePhase
	spinChar := string(m.app.state.SpinnerChar())
	m.app.state.mu.RUnlock()

	gameLabel := "(none)"
	if gameName != "" {
		gameLabel = gameName
		switch phase {
		case common.PhaseSplash:
			gameLabel += " [splash]"
		case common.PhasePlaying:
			gameLabel += " [playing]"
		case common.PhaseGameOver:
			gameLabel += " [game over]"
		}
	}

	// Layout: log title bar + log body + chat title bar + chat body + input bar
	availH := max(6, m.height-3) // 2 title bars + input bar
	logsH := max(3, availH/2)
	chatH := max(3, availH-logsH)

	logTitle := fmt.Sprintf("null-space | game: %s | teams: %d | uptime %s", gameLabel, m.app.state.TeamCount(), m.app.uptime())
	logsBar := consoleLogBarStyle.Width(m.width).Render(headerWithSpinner(logTitle, m.width, spinChar))
	logsView := fitStyledBlock(m.logs.View(), m.width, logsH, consoleLogBodyStyle)

	chatTitle := fmt.Sprintf("Chat (%d players online)", m.app.state.PlayerCount())
	chatBar := consoleChatBarStyle.Width(m.width).Render(truncateStyled(chatTitle, m.width))
	chatView := fitStyledBlock(m.chat.View(), m.width, chatH, consoleChatStyle)

	setInputStyle(&m.input, lobbyChatBarActiveBg, lobbyChatBarActiveFg)
	inputView := truncateStyled(m.input.View(), m.width)

	view.SetContent(lipgloss.JoinVertical(lipgloss.Left, logsBar, logsView, chatBar, chatView, inputView))
	view.AltScreen = true
	return view
}

func (m *consoleModel) resize() {
	availH := max(6, m.height-3) // 2 title bars + input bar
	logsH := max(3, availH/2)
	chatH := max(3, availH-logsH)

	m.logs.SetWidth(m.width)
	m.logs.SetHeight(logsH)
	m.chat.SetWidth(m.width)
	m.chat.SetHeight(chatH)
	m.input.SetWidth(max(1, m.width-2))
}

func (m *consoleModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if text == "" {
		return
	}
	ctx := common.CommandContext{
		PlayerID: "", // console = admin
		IsAdmin:  true,
		Reply: func(s string) {
			m.chatLines = append(m.chatLines, s)
			m.chat.SetContent(strings.Join(m.chatLines, "\n"))
			m.chat.GotoBottom()
		},
		Broadcast: func(s string) {
			m.app.broadcastChat(common.Message{Text: s})
		},
		ServerLog: func(s string) {
			m.logLines = append(m.logLines, s)
			m.logs.SetContent(strings.Join(m.logLines, "\n"))
			m.logs.GotoBottom()
		},
	}
	if strings.HasPrefix(text, "/") {
		m.app.registry.Dispatch(text, ctx)
		return
	}
	// Plain text = chat as [admin]
	m.app.broadcastChat(common.Message{Author: "[admin]", Text: text})
}

// tea.Msg types for channel-based updates
type logLineMsg string
type chatLineMsg common.Message

func listenForLogs(logCh <-chan string, chatCh <-chan common.Message) tea.Cmd {
	return func() tea.Msg {
		select {
		case line, ok := <-logCh:
			if !ok {
				return nil
			}
			return logLineMsg(line)
		case msg, ok := <-chatCh:
			if !ok {
				return nil
			}
			return chatLineMsg(msg)
		}
	}
}
