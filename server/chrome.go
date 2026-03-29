package server

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

type chromeModel struct {
	app      *App
	playerID string
	width    int
	height   int
	chatMode bool
	chat     viewport.Model
	input    textinput.Model
}

func newChromeModel(app *App, playerID string) chromeModel {
	chat := viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	chat.MouseWheelEnabled = false
	chat.SoftWrap = true

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Press Enter to chat"
	input.CharLimit = 256
	input.SetWidth(78)
	input.Blur()

	model := chromeModel{
		app:      app,
		playerID: playerID,
		chat:     chat,
		input:    input,
	}
	model.syncChat()
	return model
}

func (m chromeModel) Init() tea.Cmd {
	return nil
}

func (m chromeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(1, msg.Width)
		m.height = maxInt(7, msg.Height)
		m.chat.SetWidth(m.width)
		m.chat.SetHeight(5)
		m.input.SetWidth(maxInt(1, m.width-2))
		m.syncChat()
		return m, nil
	case common.TickMsg, common.RefreshMsg:
		m.syncChat()
		return m, nil
	case tea.KeyPressMsg:
		if !m.chatMode {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.enterChatMode()
				return m, nil
			default:
				m.app.handleGameMessage(msg, m.playerID)
				m.app.broadcast(common.RefreshMsg{})
				return m, nil
			}
		}

		switch msg.String() {
		case "esc":
			m.exitChatMode()
			return m, nil
		case "enter":
			m.submitInput()
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m chromeModel) View() tea.View {
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("\x1b[HLoading null-space...")
		view.AltScreen = true
		return view
	}

	gameHeight := maxInt(1, m.height-7)
	header := m.renderHeader()
	game := fitBlock(m.app.renderGame(m.playerID, m.width, gameHeight), m.width, gameHeight)
	chat := fitBlock(m.chat.View(), m.width, 5)
	input := fitBlock(m.renderInputLine(), m.width, 1)

	content := lipgloss.JoinVertical(lipgloss.Left, header, game, chat, input)
	view.SetContent("\x1b[H" + content)
	view.AltScreen = true
	return view
}

func (m *chromeModel) syncChat() {
	content := m.app.renderChatForPlayer(m.playerID)
	m.chat.SetContent(content)
	m.chat.GotoBottom()
	if m.chatMode {
		m.input.Placeholder = "Type a message or /command"
	} else {
		m.input.Placeholder = "Press Enter to chat"
	}
}

func (m *chromeModel) enterChatMode() {
	m.chatMode = true
	m.input.SetValue("")
	m.input.Focus()
	m.syncChat()
}

func (m *chromeModel) exitChatMode() {
	m.chatMode = false
	m.input.Blur()
	m.syncChat()
}

func (m *chromeModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	m.exitChatMode()
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		m.app.executeCommand(m.playerID, text)
		m.syncChat()
		return
	}
	m.app.addChatMessage(m.playerID, text)
	m.syncChat()
}

func (m chromeModel) renderHeader() string {
	label := fmt.Sprintf("[null-space] | Game: %s | Players: %d | Tunnel: %s", m.app.gameName, m.app.state.PlayerCount(), m.app.uptime())
	label = truncateRunes(label, m.width)
	return lipgloss.NewStyle().Width(m.width).Reverse(true).Render(label)
}

func (m chromeModel) renderInputLine() string {
	if !m.chatMode {
		player := m.app.state.GetPlayer(m.playerID)
		name := "pilot"
		if player != nil {
			name = player.Name
		}
		hint := fmt.Sprintf("%s | arrows move, space builds, enter chats, esc cancels", name)
		return lipgloss.NewStyle().Faint(true).Width(m.width).Render(truncateRunes(hint, m.width))
	}
	return m.input.View()
}

func fitBlock(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(truncateRunes(line, width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width])
}
