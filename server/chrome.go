package server

import (
	"fmt"
	"image/color"
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
	titleBg = lipgloss.Color("#D8C7A0")
	titleFg = lipgloss.Color("#4A2D18")
	cmdBg   = lipgloss.Color("#B8AA88") // dimmer variant of titleBg

	statusBarStyle = lipgloss.NewStyle().Background(titleBg).Foreground(titleFg).Bold(true)
	chatStyle      = lipgloss.NewStyle().Background(lipgloss.Color("#EADFC7")).Foreground(lipgloss.Color("#2C1810"))
	cmdIdleStyle = lipgloss.NewStyle().Background(cmdBg).Foreground(titleFg)
)

// setInputStyle applies matching background/foreground to all textinput sub-styles
// and switches to the real terminal cursor (not the virtual cursor).
//
// The virtual cursor's TextStyle (used during blink-hide) has no background by
// default, causing the character under the cursor to flash to terminal default
// (black) on every blink. Using the real cursor avoids this entirely: all text
// renders with a solid background, and the terminal handles cursor blinking.
func setInputStyle(input *textinput.Model, bg, fg color.Color) {
	base := lipgloss.NewStyle().Background(bg).Foreground(fg)
	s := input.Styles()
	s.Focused.Prompt = base
	s.Focused.Text = base
	s.Focused.Placeholder = base.Faint(true)
	s.Blurred.Prompt = base
	s.Blurred.Text = base
	s.Blurred.Placeholder = base.Faint(true)
	s.Cursor.Color = fg
	s.Cursor.Blink = true
	input.SetStyles(s)
	input.SetVirtualCursor(false) // use real terminal cursor; see comment above
}

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

	tabPrefix     string
	tabCandidates []string
	tabIndex      int
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

	m := chromeModel{
		app:      app,
		playerID: playerID,
		chat:     chat,
		input:    input,
	}
	m.syncChat()
	// Start in input mode when in the lobby; idle mode when a game is active.
	app.state.mu.RLock()
	inGame := app.state.ActiveApp != nil
	app.state.mu.RUnlock()
	if inGame {
		setInputStyle(&m.input, cmdBg, titleFg)
		m.input.Blur()
	} else {
		setInputStyle(&m.input, titleBg, titleFg)
		m.mode = modeInput
		m.input.Focus()
	}
	return m
}

func (m chromeModel) Init() tea.Cmd {
	if m.mode == modeInput {
		return m.input.Focus() // starts cursor blink
	}
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
		switch {
		case chatMsg.IsReply:
			line = chatMsg.Text
		case chatMsg.IsPrivate:
			from := chatMsg.FromID
			if p := m.app.state.GetPlayer(from); p != nil {
				from = p.Name
			}
			if from == "" {
				from = "admin"
			}
			line = fmt.Sprintf("[PM from %s] %s", from, chatMsg.Text)
		case chatMsg.Author == "":
			line = fmt.Sprintf("[system] %s", chatMsg.Text)
		default:
			line = fmt.Sprintf("<%s> %s", chatMsg.Author, chatMsg.Text)
		}
		m.chatLines = append(m.chatLines, line)
		if len(m.chatLines) > 200 {
			m.chatLines = m.chatLines[len(m.chatLines)-200:]
		}
		m.chat.SetContent(strings.Join(m.chatLines, "\n"))
		m.chat.GotoBottom()
		return m, nil

	case common.PlayerJoinedMsg, common.PlayerLeftMsg:
		m.syncChat()
		return m, nil

	case common.GameLoadedMsg:
		// App started — switch to game mode (idle so keys route to the game).
		setInputStyle(&m.input, cmdBg, titleFg)
		m.mode = modeIdle
		m.input.Blur()
		m.syncChat()
		return m, nil

	case common.GameUnloadedMsg:
		// Back to lobby — stay in typing mode.
		setInputStyle(&m.input, titleBg, titleFg)
		m.mode = modeInput
		cmd := m.input.Focus()
		m.syncChat()
		return m, cmd

	case tea.KeyPressMsg:
		if m.mode == modeIdle {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				setInputStyle(&m.input, titleBg, titleFg)
				m.mode = modeInput
				cmd := m.input.Focus()
				return m, cmd
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
			m.tabCandidates = nil
			m.input.SetValue("")
			// In-game: return to idle so keys route to the game.
			// Lobby: stay in input mode.
			m.app.state.mu.RLock()
			inGame := m.app.state.ActiveApp != nil
			m.app.state.mu.RUnlock()
			if inGame {
				setInputStyle(&m.input, cmdBg, titleFg)
				m.mode = modeIdle
				m.input.Blur()
			}
			return m, nil
		case "enter":
			m.tabCandidates = nil
			m.submitInput()
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

		var cmdBar string
		if m.mode == modeInput {
			cmdBar = truncateStyled(m.input.View(), m.width)
		} else {
			cmdBar = cmdIdleStyle.Width(m.width).Render("[Enter] to chat  /help for commands")
		}

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

		var cmdBar string
		if m.mode == modeInput {
			cmdBar = truncateStyled(m.input.View(), m.width)
		} else {
			idleText := game.CommandBar(m.playerID)
			if idleText == "" {
				idleText = fmt.Sprintf("[Enter] to chat  | game: %s", gameName)
			}
			cmdBar = cmdIdleStyle.Width(m.width).Render(idleText)
		}

		content = lipgloss.JoinVertical(lipgloss.Left, statusBar, gameView, chatView, cmdBar)
	}

	view.SetContent(content)
	view.AltScreen = true
	// Position the real terminal cursor on the command bar (last row).
	if m.mode == modeInput {
		if cursor := m.input.Cursor(); cursor != nil {
			cursor.Position.Y = m.height - 1
			view.Cursor = cursor
		}
	}
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
	// In-game: return to idle after submit so keys route to the game.
	// Lobby: stay in input mode.
	m.app.state.mu.RLock()
	inGame := m.app.state.ActiveApp != nil
	m.app.state.mu.RUnlock()
	if inGame {
		setInputStyle(&m.input, cmdBg, titleFg)
		m.mode = modeIdle
		m.input.Blur()
	}
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
				msg := common.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: s}
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
