package server

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
)

var (
	statusBarStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#D8C7A0")).Foreground(lipgloss.Color("#4A2D18")).Bold(true)
	chatStyle       = lipgloss.NewStyle().Background(lipgloss.Color("#EADFC7")).Foreground(lipgloss.Color("#2C1810"))
	commandBarStyle = lipgloss.NewStyle().Background(lipgloss.Color("#D8C7A0")).Foreground(lipgloss.Color("#4A2D18"))
)

const (
	modeIdle  = 0
	modeInput = 1
)

var spinnerFramesChrome = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type chromeModel struct {
	app      *App
	playerID string
	width    int
	height   int
	mode     int

	chat  viewport.Model
	input textinput.Model

	chatLines []string // buffered chat lines visible to this player
}

func newChromeModel(app *App, playerID string) chromeModel {
	chat := viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	chat.MouseWheelEnabled = false
	chat.SoftWrap = true

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Type a message or /command"
	input.CharLimit = 256
	input.SetWidth(78)
	input.Blur()

	m := chromeModel{
		app:      app,
		playerID: playerID,
		chat:     chat,
		input:    input,
	}
	m.syncChat()
	return m
}

func (m chromeModel) Init() tea.Cmd {
	return nil
}

func (m chromeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(1, msg.Width)
		m.height = maxInt(8, msg.Height)
		m.resizeViewports()
		m.syncChat()
		return m, nil

	case common.TickMsg:
		m.syncChat()
		return m, nil

	case common.ChatMsg:
		chatMsg := msg.Msg
		// filter private messages
		if chatMsg.IsPrivate {
			if chatMsg.ToID != m.playerID && chatMsg.FromID != m.playerID {
				return m, nil
			}
		}
		var line string
		if chatMsg.IsPrivate {
			from := chatMsg.FromID
			if p := m.app.state.GetPlayer(from); p != nil {
				from = p.Name
			}
			if from == "" {
				from = "admin"
			}
			line = fmt.Sprintf("[PM from %s] %s", from, chatMsg.Text)
		} else if chatMsg.Author == "" {
			line = fmt.Sprintf("[system] %s", chatMsg.Text)
		} else {
			line = fmt.Sprintf("<%s> %s", chatMsg.Author, chatMsg.Text)
		}
		m.chatLines = append(m.chatLines, line)
		if len(m.chatLines) > 200 {
			m.chatLines = m.chatLines[len(m.chatLines)-200:]
		}
		m.chat.SetContent(strings.Join(m.chatLines, "\n"))
		m.chat.GotoBottom()
		return m, nil

	case common.PlayerJoinedMsg, common.PlayerLeftMsg, common.GameLoadedMsg, common.GameUnloadedMsg:
		m.syncChat()
		return m, nil

	case tea.KeyPressMsg:
		if m.mode == modeIdle {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.mode = modeInput
				m.input.Focus()
				return m, nil
			default:
				// route to game
				m.app.state.mu.RLock()
				game := m.app.state.ActiveApp
				m.app.state.mu.RUnlock()
				if game != nil {
					game.OnInput(m.playerID, msg.String())
				}
				return m, nil
			}
		}

		// modeInput
		switch msg.String() {
		case "esc":
			m.mode = modeIdle
			m.input.Blur()
			m.input.SetValue("")
			return m, nil
		case "enter":
			m.submitInput()
			return m, nil
		case "tab":
			if strings.HasPrefix(m.input.Value(), "/") {
				completed, changed := m.app.registry.TabComplete(m.input.Value(), m.app.state.PlayerNames())
				if changed {
					m.input.SetValue(completed)
					m.input.CursorEnd()
				}
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// Forward other messages to textinput in input mode (cursor blink etc.)
	if m.mode == modeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m chromeModel) View() tea.View {
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading null-space...")
		view.AltScreen = true
		return view
	}

	m.app.state.mu.RLock()
	game := m.app.state.ActiveApp
	gameName := m.app.state.AppName
	spinChar := string(m.app.state.SpinnerChar())
	m.app.state.mu.RUnlock()

	var content string
	if game == nil {
		// Lobby layout
		statusText := fmt.Sprintf("null-space | %d players | uptime %s", m.app.state.PlayerCount(), m.app.uptime())
		statusBar := statusBarStyle.Width(m.width).Render(headerWithSpinner(statusText, m.width, spinChar))

		chatH := m.height - 2
		if chatH < 1 {
			chatH = 1
		}
		chatView := fitStyledBlock(m.chat.View(), m.width, chatH, chatStyle)

		cmdBarText := "[Enter] to chat  /help for commands"
		if m.mode == modeInput {
			cmdBarText = m.input.View()
		}
		cmdBar := commandBarStyle.Width(m.width).Render(truncateStyled(cmdBarText, m.width))

		content = lipgloss.JoinVertical(lipgloss.Left, statusBar, chatView, cmdBar)
	} else {
		// In-game layout
		statusText := game.StatusBar(m.playerID)
		statusBar := statusBarStyle.Width(m.width).Render(headerWithSpinner(statusText, m.width, spinChar))

		gameH := m.width * 9 / 16
		chatH := m.height - 1 - gameH - 1
		if chatH < 5 {
			// reduce gameH to make room for chat
			chatH = 5
			gameH = m.height - 1 - chatH - 1
			if gameH < 0 {
				gameH = 0
			}
		}

		gameView := fitBlock(game.View(m.playerID, m.width, gameH), m.width, gameH)
		chatView := fitStyledBlock(m.chat.View(), m.width, chatH, chatStyle)

		cmdBarText := game.CommandBar(m.playerID)
		if cmdBarText == "" {
			cmdBarText = fmt.Sprintf("[Enter] to chat  | game: %s", gameName)
		}
		if m.mode == modeInput {
			cmdBarText = m.input.View()
		}
		cmdBar := commandBarStyle.Width(m.width).Render(truncateStyled(cmdBarText, m.width))

		content = lipgloss.JoinVertical(lipgloss.Left, statusBar, gameView, chatView, cmdBar)
	}

	view.SetContent(content)
	view.AltScreen = true
	return view
}

func (m *chromeModel) syncChat() {
	// Rebuild chat from state
	history := m.app.state.GetChatHistory()
	lines := make([]string, 0, len(history))
	for _, msg := range history {
		if msg.IsPrivate {
			if msg.ToID != m.playerID && msg.FromID != m.playerID {
				continue
			}
			from := msg.FromID
			if p := m.app.state.GetPlayer(from); p != nil {
				from = p.Name
			}
			if from == "" {
				from = "admin"
			}
			lines = append(lines, fmt.Sprintf("[PM from %s] %s", from, msg.Text))
		} else if msg.Author == "" {
			lines = append(lines, fmt.Sprintf("[system] %s", msg.Text))
		} else {
			lines = append(lines, fmt.Sprintf("<%s> %s", msg.Author, msg.Text))
		}
	}
	m.chatLines = lines
	m.chat.SetContent(strings.Join(lines, "\n"))
	m.chat.GotoBottom()
}

func (m *chromeModel) resizeViewports() {
	m.app.state.mu.RLock()
	game := m.app.state.ActiveApp
	m.app.state.mu.RUnlock()

	if game == nil {
		chatH := m.height - 2
		if chatH < 1 {
			chatH = 1
		}
		m.chat.SetWidth(m.width)
		m.chat.SetHeight(chatH)
	} else {
		gameH := m.width * 9 / 16
		chatH := m.height - 1 - gameH - 1
		if chatH < 5 {
			chatH = 5
			gameH = m.height - 1 - chatH - 1
			if gameH < 0 {
				gameH = 0
			}
		}
		m.chat.SetWidth(m.width)
		m.chat.SetHeight(chatH)
	}
	m.input.SetWidth(maxInt(1, m.width-2))
}

func (m *chromeModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	m.mode = modeIdle
	m.input.Blur()
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		player := m.app.state.GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		ctx := common.CommandContext{
			PlayerID: m.playerID,
			IsAdmin:  isAdmin,
			Reply: func(s string) {
				msg := common.Message{IsPrivate: true, ToID: m.playerID, Text: s}
				m.app.sendToPlayer(m.playerID, common.ChatMsg{Msg: msg})
			},
			Broadcast: func(s string) {
				m.app.broadcastChat(common.Message{Text: s})
			},
			ServerLog: func(s string) {
				m.app.serverLog(s)
			},
		}
		m.app.registry.Dispatch(text, ctx)
		return
	}
	// Regular chat
	playerName := "unknown"
	if p := m.app.state.GetPlayer(m.playerID); p != nil {
		playerName = p.Name
	}
	m.app.broadcastChat(common.Message{Author: playerName, Text: text})
}

func headerWithSpinner(text string, width int, spinner string) string {
	if width <= 0 {
		return ""
	}
	spinnerWidth := ansi.StringWidth(spinner)
	if width <= spinnerWidth {
		return truncateStyled(spinner, width)
	}
	left := truncateStyled(text, width-spinnerWidth-1)
	spaces := width - ansi.StringWidth(left) - spinnerWidth
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + spinner
}

func currentSpinnerFrame() string {
	interval := int64(125) // ms
	frame := (time.Now().UnixMilli() / interval) % int64(len(spinnerFramesChrome))
	return spinnerFramesChrome[frame]
}

func fitBlock(content string, width, height int) string {
	return fitStyledBlock(content, width, height, lipgloss.NewStyle())
}

func fitStyledBlock(content string, width, height int, style lipgloss.Style) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = style.Width(width).MaxWidth(width).Render(truncateStyled(line, width))
	}
	for len(lines) < height {
		lines = append(lines, style.Width(width).Render(strings.Repeat(" ", width)))
	}
	return strings.Join(lines, "\n")
}

func truncateStyled(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "")
}
