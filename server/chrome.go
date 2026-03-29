package server

import (
	"fmt"
	"log/slog"
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
	playerHeaderStyle = lipgloss.NewStyle().Width(0).Background(lipgloss.Color("#D8C7A0")).Foreground(lipgloss.Color("#4A2D18")).Bold(true)
	playerStatusStyle = lipgloss.NewStyle().Width(0).Background(lipgloss.Color("#D8C7A0")).Foreground(lipgloss.Color("#4A2D18"))
	chatStyle         = lipgloss.NewStyle().Background(lipgloss.Color("#EADFC7")).Foreground(lipgloss.Color("#2C1810"))
	chatChromeStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#D8C7A0")).Foreground(lipgloss.Color("#4A2D18")).Bold(true)
	spinnerFrames     = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

const (
	spinnerFrameInterval = 125 * time.Millisecond
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
		m.app.notePlayerActivity(m.playerID)
		m.width = maxInt(1, msg.Width)
		m.height = maxInt(8, msg.Height)
		m.chat.SetWidth(m.width)
		_, chatHeight := clientLayoutHeights(m.width, m.height)
		m.chat.SetHeight(chatHeight)
		m.input.SetWidth(maxInt(1, m.width-2))
		m.syncChat()
		return m, nil
	case common.TickMsg, common.RefreshMsg:
		m.syncChat()
		return m, nil
	case tea.KeyPressMsg:
		m.app.notePlayerActivity(m.playerID)
		if !m.chatMode {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.enterChatMode()
				return m, nil
			default:
				slog.Debug("client input received", "player_id", m.playerID, "key", msg.String())
				if direction := movementDirection(msg.String()); direction != "" {
					m.dispatchMovement(direction)
					return m, nil
				}
				m.app.handleGameMessage(msg, m.playerID)
				m.app.sendToPlayer(m.playerID, common.RefreshMsg{})
				m.app.broadcastExcept(m.playerID, common.RefreshMsg{})
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

	gameHeight, chatHeight := clientLayoutHeights(m.width, m.height)
	header := m.renderHeader()
	game := fitBlock(m.app.renderGame(m.playerID, m.width, gameHeight), m.width, gameHeight)
	status := fitStyledBlock(m.renderGameStatusBar(), m.width, 1, playerStatusStyle)
	chat := fitStyledBlock(m.chat.View(), m.width, chatHeight, chatStyle)

	content := lipgloss.JoinVertical(lipgloss.Left, header, game, status, chat)
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
	return playerHeaderStyle.Width(m.width).Render(headerWithSpinner(label, m.width, currentSpinnerFrame()))
}

func (m chromeModel) renderGameStatusBar() string {
	if m.chatMode {
		return m.input.View()
	}
	if statusProvider, ok := m.app.state.ActiveGame.(common.PlayerStatusProvider); ok {
		return statusProvider.PlayerStatus(m.playerID, m.width)
	}
	return "Enter to chat | /help for commands"
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
	interval := spinnerFrameInterval.Milliseconds()
	if interval <= 0 {
		interval = 100
	}
	frame := (time.Now().UnixMilli() / interval) % int64(len(spinnerFrames))
	return spinnerFrames[frame]
}

func clientLayoutHeights(width, height int) (int, int) {
	const fixedRows = 2
	const minChatHeight = 5
	const wideAspectWidth = 16
	const wideAspectHeight = 9
	const terminalCellWidthUnits = 2

	availableHeight := maxInt(0, height-fixedRows)
	chatHeight := minChatHeight
	if availableHeight <= minChatHeight {
		return 0, minChatHeight
	}

	gameHeight := availableHeight - minChatHeight
	// Terminal cells are taller than they are wide, so use a wider grid ratio
	// than the visual target to keep the map from growing vertically too fast.
	targetGameHeight := maxInt(1, width*wideAspectHeight/(wideAspectWidth*terminalCellWidthUnits))
	if gameHeight > targetGameHeight {
		gameHeight = targetGameHeight
	}
	if gameHeight < 1 {
		gameHeight = 1
	}

	chatHeight = maxInt(minChatHeight, availableHeight-gameHeight)
	return gameHeight, chatHeight
}

func movementDirection(key string) string {
	switch key {
	case "up", "down", "left", "right":
		return key
	default:
		return ""
	}
}

func (m *chromeModel) dispatchMovement(direction string) {
	m.app.handleGameMessage(common.MoveMsg{Direction: direction}, m.playerID)
	m.app.sendToPlayer(m.playerID, common.RefreshMsg{})
	m.app.broadcastExcept(m.playerID, common.RefreshMsg{})
}
