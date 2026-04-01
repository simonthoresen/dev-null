package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

// Console uses the same lobby palettes: blue for the menu/status bars, warm for log body.
var (
	consoleMenuStyle = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg).Bold(true)
	consoleLogStyle  = lipgloss.NewStyle().Background(lobbyTeamActiveBg).Foreground(lobbyTeamFg)
	consoleCmdStyle  = lipgloss.NewStyle().Background(lobbyChatBarActiveBg).Foreground(lobbyChatBarActiveFg)
)

type consoleModel struct {
	app    *Server
	cancel context.CancelFunc
	width  int
	height int

	log  viewport.Model
	input textinput.Model

	logLines []string

	inputHistory []string
	historyIdx   int
	historyDraft string

	tabPrefix     string
	tabCandidates []string
	tabIndex      int
}

func NewConsoleModel(app *Server, cancel context.CancelFunc) *consoleModel {
	log := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	log.SoftWrap = true
	log.MouseWheelEnabled = false

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	input.Focus()

	return &consoleModel{
		app:        app,
		cancel:     cancel,
		log:        log,
		input:      input,
		historyIdx: -1,
	}
}

func (m *consoleModel) Init() tea.Cmd {
	return listenForLogs(m.app.LogCh(), m.app.ChatCh())
}

func (m *consoleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(6, msg.Height)
		m.app.consoleWidth = m.width
		m.resize()
		return m, nil

	case common.TickMsg:
		// re-render for spinner and clock update
		return m, nil

	case logLineMsg:
		m.appendLog(string(msg))
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
		m.appendLog(line)
		return m, listenForLogs(m.app.LogCh(), m.app.ChatCh())

	case common.GamePhaseMsg, common.GameLoadedMsg, common.GameUnloadedMsg, common.TeamUpdatedMsg, common.PlayerJoinedMsg, common.PlayerLeftMsg:
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "pgup":
			m.log.PageUp()
			return m, nil
		case "pgdown":
			m.log.PageDown()
			return m, nil
		case "enter":
			m.tabCandidates = nil
			m.historyIdx = -1
			m.historyDraft = ""
			m.submitInput()
			return m, nil
		case "esc":
			m.tabCandidates = nil
			m.historyIdx = -1
			m.historyDraft = ""
			m.input.SetValue("")
			return m, nil
		case "up":
			if len(m.inputHistory) == 0 {
				return m, nil
			}
			if m.historyIdx == -1 {
				m.historyDraft = m.input.Value()
				m.historyIdx = len(m.inputHistory) - 1
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.input.SetValue(m.inputHistory[m.historyIdx])
			m.input.CursorEnd()
			return m, nil
		case "down":
			if m.historyIdx == -1 {
				return m, nil
			}
			if m.historyIdx < len(m.inputHistory)-1 {
				m.historyIdx++
				m.input.SetValue(m.inputHistory[m.historyIdx])
			} else {
				m.historyIdx = -1
				m.input.SetValue(m.historyDraft)
			}
			m.input.CursorEnd()
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

	gameLabel := "none"
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

	menuTitle := fmt.Sprintf("null-space | game: %s | teams: %d | uptime %s", gameLabel, m.app.state.TeamCount(), m.app.uptime())
	menuBar := consoleMenuStyle.Width(m.width).Render(headerWithSpinner(menuTitle, m.width, spinChar))

	logView := fitStyledBlock(m.log.View(), m.width, m.log.Height(), consoleLogStyle)

	setInputStyle(&m.input, lobbyChatBarActiveBg, lobbyChatBarActiveFg)
	cmdBar := consoleCmdStyle.Width(m.width).Render(truncateStyled(m.input.View(), m.width))

	statusBar := consoleMenuStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04"))

	view.SetContent(lipgloss.JoinVertical(lipgloss.Left, menuBar, logView, cmdBar, statusBar))
	view.AltScreen = true

	if cursor := m.input.Cursor(); cursor != nil {
		cursor.Position.Y = m.height - 2
		view.Cursor = cursor
	}

	return view
}

func (m *consoleModel) resize() {
	logH := max(1, m.height-3) // menu bar + command bar + status bar
	m.log.SetWidth(m.width)
	m.log.SetHeight(logH)
	m.input.SetWidth(max(1, m.width-2))
}

func (m *consoleModel) appendLog(line string) {
	for _, l := range strings.Split(line, "\n") {
		m.logLines = append(m.logLines, l)
	}
	if len(m.logLines) > 500 {
		m.logLines = m.logLines[len(m.logLines)-500:]
	}
	m.log.SetContent(strings.Join(m.logLines, "\n"))
	m.log.GotoBottom()
}

func (m *consoleModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if text == "" {
		return
	}
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > 50 {
			m.inputHistory = m.inputHistory[1:]
		}
	}
	ctx := common.CommandContext{
		PlayerID: "", // console = admin
		IsAdmin:  true,
		Reply: func(s string) {
			m.appendLog(s)
		},
		Broadcast: func(s string) {
			m.app.broadcastChat(common.Message{Text: s})
		},
		ServerLog: func(s string) {
			m.appendLog(s)
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
