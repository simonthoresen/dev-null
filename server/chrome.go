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

// Default framework chrome colors.
var (
	defaultStatusBg = lipgloss.Color("#D8C7A0")
	defaultStatusFg = lipgloss.Color("#4A2D18")
	defaultCmdBg    = lipgloss.Color("#B8AA88")
	defaultChatBg   = lipgloss.Color("#EADFC7")
	defaultChatFg   = lipgloss.Color("#2C1810")

	// Used by setInputStyle call sites that run before View() (newChromeModel).
	titleBg = defaultStatusBg
	titleFg = defaultStatusFg
	cmdBg   = defaultCmdBg
)

// Lobby panel colors — chat (warm/white variant).
var (
	lobbyChatBarActiveBg   = lipgloss.Color("#D8C7A0")
	lobbyChatBarActiveFg   = lipgloss.Color("#4A2D18")
	lobbyChatBarInactiveBg = lipgloss.Color("#C4B898")
	lobbyChatBarInactiveFg = lipgloss.Color("#8A7A68")
	lobbyChatActiveBg      = lipgloss.Color("#EADFC7")
	lobbyChatInactiveBg    = lipgloss.Color("#E0D6BE")
	lobbyChatFg            = lipgloss.Color("#2C1810")
)

// Lobby panel colors — teams (blue variant).
var (
	lobbyTeamBarActiveBg   = lipgloss.Color("#5B7BA5")
	lobbyTeamBarActiveFg   = lipgloss.Color("#FFFFFF")
	lobbyTeamBarInactiveBg = lipgloss.Color("#8898B0")
	lobbyTeamBarInactiveFg = lipgloss.Color("#C0C8D8")
	lobbyTeamActiveBg      = lipgloss.Color("#CEDAEA")
	lobbyTeamInactiveBg    = lipgloss.Color("#C4D0E0")
	lobbyTeamFg            = lipgloss.Color("#1A2A40")
)

const lobbyTeamPanelW = 32

type chromeColors struct {
	statusBg, statusFg color.Color
	chatBg, chatFg     color.Color
	cmdBg, cmdFg       color.Color
	inputBg, inputFg   color.Color
}

func resolveColors(skin *common.SkinColors) chromeColors {
	c := chromeColors{
		statusBg: defaultStatusBg,
		statusFg: defaultStatusFg,
		chatBg:   defaultChatBg,
		chatFg:   defaultChatFg,
		cmdBg:    defaultCmdBg,
		cmdFg:    defaultStatusFg,
		inputBg:  defaultStatusBg,
		inputFg:  defaultStatusFg,
	}
	if skin == nil {
		return c
	}
	if skin.StatusBg != "" {
		c.statusBg = lipgloss.Color(skin.StatusBg)
	}
	if skin.StatusFg != "" {
		c.statusFg = lipgloss.Color(skin.StatusFg)
	}
	if skin.ChatBg != "" {
		c.chatBg = lipgloss.Color(skin.ChatBg)
	}
	if skin.ChatFg != "" {
		c.chatFg = lipgloss.Color(skin.ChatFg)
	}
	if skin.CmdBg != "" {
		c.cmdBg = lipgloss.Color(skin.CmdBg)
	}
	if skin.CmdFg != "" {
		c.cmdFg = lipgloss.Color(skin.CmdFg)
	}
	if skin.InputBg != "" {
		c.inputBg = lipgloss.Color(skin.InputBg)
	}
	if skin.InputFg != "" {
		c.inputFg = lipgloss.Color(skin.InputFg)
	}
	return c
}

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

const (
	lobbyFocusChat  = 0
	lobbyFocusTeams = 1
)

type chromeModel struct {
	app      *Server
	playerID string
	width    int
	height   int
	mode     int

	// inActiveGame is true when this player is participating in the current game.
	// Late joiners (connected after GameLoadedMsg) stay in lobby mode.
	inActiveGame bool

	chat  viewport.Model
	input textinput.Model

	chatLines        []string // buffered chat lines visible to this player (max 200)
	chatScrollOffset int      // lines scrolled up from bottom (0 = bottom)
	chatH            int      // current chat panel height (updated in resizeViewports)

	inputHistory []string // submitted inputs, oldest first (max 50)
	historyIdx   int      // index into inputHistory while browsing; -1 = not browsing
	historyDraft string   // input text saved before starting history browse

	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Lobby team panel state
	lobbyFocus    int  // lobbyFocusChat or lobbyFocusTeams
	teamEditing   bool // true when renaming a team
	teamEditInput textinput.Model

	// Game-over countdown tracking
	gameOverStart time.Time
}

func newChromeModel(app *Server, playerID string) chromeModel {
	chat := viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	chat.MouseWheelEnabled = false
	chat.SoftWrap = true

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)

	teamInput := textinput.New()
	teamInput.Prompt = ""
	teamInput.CharLimit = 20
	teamInput.SetWidth(20)

	m := chromeModel{
		app:           app,
		playerID:      playerID,
		chat:          chat,
		input:         input,
		teamEditInput: teamInput,
		historyIdx:    -1,
	}
	m.syncChat()
	// Always start in lobby/input mode. GameLoadedMsg will transition
	// participating players into game mode. Late joiners stay in lobby.
	setInputStyle(&m.input, titleBg, titleFg)
	m.mode = modeInput
	m.input.Focus()
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
		m.width = max(1, msg.Width)
		m.height = max(8, msg.Height)
		m.resizeViewports()
		m.syncChat()
		return m, nil

	case common.TickMsg:
		return m, nil

	case common.ChatMsg:
		chatMsg := msg.Msg
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
		for _, l := range strings.Split(line, "\n") {
			m.chatLines = append(m.chatLines, l)
		}
		if len(m.chatLines) > 200 {
			m.chatLines = m.chatLines[len(m.chatLines)-200:]
		}
		m.chat.SetContent(strings.Join(m.chatLines, "\n"))
		m.chat.GotoBottom()
		return m, nil

	case common.PlayerJoinedMsg, common.PlayerLeftMsg:
		m.syncChat()
		return m, nil

	case common.TeamUpdatedMsg:
		return m, nil // just triggers re-render

	case common.GameLoadedMsg:
		// This player was connected when the game loaded — they're in the game.
		m.inActiveGame = true
		setInputStyle(&m.input, cmdBg, titleFg)
		m.mode = modeIdle
		m.lobbyFocus = lobbyFocusChat
		m.input.Blur()
		m.resizeViewports()
		m.syncChat()
		return m, nil

	case common.GameUnloadedMsg:
		m.inActiveGame = false
		setInputStyle(&m.input, titleBg, titleFg)
		m.mode = modeInput
		cmd := m.input.Focus()
		m.resizeViewports()
		m.syncChat()
		return m, cmd

	case common.GamePhaseMsg:
		if msg.Phase == common.PhaseGameOver {
			m.gameOverStart = time.Now()
		}
		if msg.Phase == common.PhaseNone {
			m.inActiveGame = false
			setInputStyle(&m.input, titleBg, titleFg)
			m.mode = modeInput
			cmd := m.input.Focus()
			m.resizeViewports()
			return m, cmd
		}
		m.resizeViewports()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Forward other messages to textinput in input mode (cursor blink etc.)
	if m.mode == modeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m chromeModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	phase := m.app.state.GetGamePhase()

	// Chat scroll — handled in all modes.
	switch msg.String() {
	case "pgup":
		chatH := max(1, m.chatH)
		m.chatScrollOffset += chatH - 1
		maxOffset := len(m.chatLines) - chatH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.chatScrollOffset > maxOffset {
			m.chatScrollOffset = maxOffset
		}
		return m, nil
	case "pgdown":
		chatH := max(1, m.chatH)
		m.chatScrollOffset -= chatH - 1
		if m.chatScrollOffset < 0 {
			m.chatScrollOffset = 0
		}
		return m, nil
	}

	// Team rename mode — capture all keys for the team name input.
	if m.teamEditing {
		return m.handleTeamEditKey(msg)
	}

	// Lobby team panel focus — handle team navigation keys.
	if !m.inActiveGame && m.lobbyFocus == lobbyFocusTeams {
		return m.handleTeamKey(msg)
	}

	// Splash phase — admin can press Enter to start, others wait.
	if m.inActiveGame && phase == common.PhaseSplash {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			player := m.app.state.GetPlayer(m.playerID)
			if player != nil && player.IsAdmin {
				m.app.StartGame()
			}
		}
		return m, nil
	}

	// Game-over phase — Enter acknowledges.
	if m.inActiveGame && phase == common.PhaseGameOver {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			m.app.AcknowledgeGameOver(m.playerID)
		}
		return m, nil
	}

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
			game := m.app.state.ActiveGame
			m.app.state.mu.RUnlock()
			if game != nil {
				key := msg.String()
				// Bubble Tea v2 returns "space" for spacebar; normalize to " "
				// so game scripts can use the intuitive key === " " check.
				if key == "space" {
					key = " "
				}
				game.OnInput(m.playerID, key)
			}
			return m, nil
		}
	}

	// modeInput
	switch msg.String() {
	case "esc":
		m.tabCandidates = nil
		m.historyIdx = -1
		m.historyDraft = ""
		m.input.SetValue("")
		if m.inActiveGame {
			setInputStyle(&m.input, cmdBg, titleFg)
			m.mode = modeIdle
			m.input.Blur()
		}
		return m, nil
	case "enter":
		m.tabCandidates = nil
		m.historyIdx = -1
		m.historyDraft = ""
		m.submitInput()
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
		// In lobby with chat focused: toggle to team panel (if input is empty).
		if !m.inActiveGame && m.input.Value() == "" {
			m.lobbyFocus = lobbyFocusTeams
			m.input.Blur()
			return m, nil
		}
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

func (m chromeModel) handleTeamKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "esc":
		// Switch back to chat.
		m.lobbyFocus = lobbyFocusChat
		m.mode = modeInput
		cmd := m.input.Focus()
		return m, cmd
	case "up":
		idx := m.app.state.PlayerTeamIndex(m.playerID)
		if idx == 0 {
			// At first team — become unassigned.
			m.app.state.MovePlayerToTeam(m.playerID, -1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		} else if idx > 0 {
			// Move to team above.
			m.app.state.MovePlayerToTeam(m.playerID, idx-1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		}
		// idx == -1 (unassigned) — block, already at top.
		return m, nil
	case "down":
		idx := m.app.state.PlayerTeamIndex(m.playerID)
		teamCount := m.app.state.TeamCount()
		if idx < 0 {
			// Unassigned — join first team, or create one if none exist.
			if teamCount > 0 {
				m.app.state.MovePlayerToTeam(m.playerID, 0)
			} else {
				m.app.state.MovePlayerToTeam(m.playerID, 0) // creates new team
			}
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		} else if idx < teamCount-1 {
			// Move to team below.
			m.app.state.MovePlayerToTeam(m.playerID, idx+1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		} else if !m.app.state.IsSoleMemberOfTeam(m.playerID) {
			// On last team with others — create new solo team.
			m.app.state.MovePlayerToTeam(m.playerID, teamCount)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		}
		// Sole member of last team — block to avoid drop/recreate.
		return m, nil
	case "enter":
		// If first player of team, start renaming.
		if m.app.state.IsFirstInTeam(m.playerID) {
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			teams := m.app.state.GetTeams()
			if idx >= 0 && idx < len(teams) {
				m.teamEditing = true
				m.teamEditInput.SetValue(teams[idx].Name)
				m.teamEditInput.Focus()
				m.teamEditInput.CursorEnd()
			}
		}
		return m, nil
	case "left":
		if m.app.state.IsFirstInTeam(m.playerID) {
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			m.app.state.NextTeamColor(idx, -1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		}
		return m, nil
	case "right":
		if m.app.state.IsFirstInTeam(m.playerID) {
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			m.app.state.NextTeamColor(idx, 1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		}
		return m, nil
	}
	return m, nil
}

func (m chromeModel) handleTeamEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := strings.TrimSpace(m.teamEditInput.Value())
		if name != "" {
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			if m.app.state.RenameTeam(idx, name) {
				m.app.broadcastMsg(common.TeamUpdatedMsg{})
			}
		}
		m.teamEditing = false
		m.teamEditInput.Blur()
		return m, nil
	case "esc":
		m.teamEditing = false
		m.teamEditInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.teamEditInput, cmd = m.teamEditInput.Update(msg)
		return m, cmd
	}
}

func (m chromeModel) View() tea.View {
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading null-space...")
		view.AltScreen = true
		return view
	}

	m.app.state.mu.RLock()
	game := m.app.state.ActiveGame
	gameName := m.app.state.GameName
	phase := m.app.state.GamePhase
	spinChar := string(m.app.state.SpinnerChar())
	m.app.state.mu.RUnlock()

	col := resolveColors(m.app.state.ActiveSkin())
	sbStyle := lipgloss.NewStyle().Background(col.statusBg).Foreground(col.statusFg).Bold(true)
	chStyle := lipgloss.NewStyle().Background(col.chatBg).Foreground(col.chatFg)
	ciStyle := lipgloss.NewStyle().Background(col.cmdBg).Foreground(col.cmdFg)

	if !m.inActiveGame || phase == common.PhaseNone {
		// Lobby: input uses chat panel active colors.
		if m.mode == modeInput {
			setInputStyle(&m.input, lobbyChatBarActiveBg, lobbyChatBarActiveFg)
		}
		if m.teamEditing {
			setInputStyle(&m.teamEditInput, lobbyTeamActiveBg, lobbyTeamFg)
		}
	} else if m.mode == modeInput {
		setInputStyle(&m.input, col.inputBg, col.inputFg)
	} else {
		setInputStyle(&m.input, col.cmdBg, col.cmdFg)
	}

	var content string

	if !m.inActiveGame || phase == common.PhaseNone {
		// === LOBBY LAYOUT (with team panel) ===
		content = m.viewLobby(sbStyle, chStyle, ciStyle, spinChar)
	} else if phase == common.PhaseSplash {
		content = m.viewSplash(game, gameName, sbStyle, chStyle, ciStyle, spinChar)
	} else if phase == common.PhaseGameOver {
		content = m.viewGameOver(game, gameName, sbStyle, chStyle, ciStyle, spinChar)
	} else {
		// === PLAYING LAYOUT ===
		content = m.viewPlaying(game, gameName, sbStyle, chStyle, ciStyle, spinChar)
	}

	view.SetContent(content)
	view.AltScreen = true
	if m.mode == modeInput {
		if cursor := m.input.Cursor(); cursor != nil {
			cursor.Position.Y = m.height - 1
			view.Cursor = cursor
		}
	}
	if m.teamEditing {
		if cursor := m.teamEditInput.Cursor(); cursor != nil {
			// Position cursor on the team name row in the right panel.
			teamW := lobbyTeamPanelW
			if teamW > m.width-10 {
				teamW = m.width - 10
			}
			teams := m.app.state.GetTeams()
			unassigned := m.app.state.UnassignedPlayers()
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			row := 0
			if len(unassigned) > 0 {
				row += 1 + len(unassigned)
			}
			for i := 0; i < idx && i < len(teams); i++ {
				row += 1 + len(teams[i].Players)
			}
			cursor.Position.Y = 1 + row // +1 for status bar
			cursor.Position.X += (m.width - teamW) + 4
			view.Cursor = cursor
		}
	}
	return view
}

func (m chromeModel) viewLobby(_, _, _ lipgloss.Style, spinChar string) string {
	contentH := m.height - 2 // status bar + command bar
	if contentH < 1 {
		contentH = 1
	}

	teamW := lobbyTeamPanelW
	if teamW > m.width-10 {
		teamW = m.width - 10
	}
	chatW := m.width - teamW

	chatActive := m.lobbyFocus == lobbyFocusChat

	// Per-panel styles based on focus.
	var chatBarStyle, teamBarStyle lipgloss.Style
	var chatBodyStyle, teamBodyStyle lipgloss.Style
	var chatCmdStyle, teamCmdStyle lipgloss.Style

	if chatActive {
		chatBarStyle = lipgloss.NewStyle().Background(lobbyChatBarActiveBg).Foreground(lobbyChatBarActiveFg).Bold(true)
		chatBodyStyle = lipgloss.NewStyle().Background(lobbyChatActiveBg).Foreground(lobbyChatFg)
		chatCmdStyle = lipgloss.NewStyle().Background(lobbyChatBarActiveBg).Foreground(lobbyChatBarActiveFg)
		teamBarStyle = lipgloss.NewStyle().Background(lobbyTeamBarInactiveBg).Foreground(lobbyTeamBarInactiveFg)
		teamBodyStyle = lipgloss.NewStyle().Background(lobbyTeamInactiveBg).Foreground(lobbyTeamFg)
		teamCmdStyle = lipgloss.NewStyle().Background(lobbyTeamBarInactiveBg).Foreground(lobbyTeamBarInactiveFg)
	} else {
		chatBarStyle = lipgloss.NewStyle().Background(lobbyChatBarInactiveBg).Foreground(lobbyChatBarInactiveFg)
		chatBodyStyle = lipgloss.NewStyle().Background(lobbyChatInactiveBg).Foreground(lobbyChatFg)
		chatCmdStyle = lipgloss.NewStyle().Background(lobbyChatBarInactiveBg).Foreground(lobbyChatBarInactiveFg)
		teamBarStyle = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg).Bold(true)
		teamBodyStyle = lipgloss.NewStyle().Background(lobbyTeamActiveBg).Foreground(lobbyTeamFg)
		teamCmdStyle = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg)
	}

	// Status bar (split across panels). Spinner lives in the teams bar (far right).
	statusText := fmt.Sprintf("null-space | %d players | uptime %s", m.app.state.PlayerCount(), m.app.uptime())
	chatStatus := chatBarStyle.Width(chatW).Render(truncateStyled(statusText, chatW))
	teamStatus := teamBarStyle.Width(teamW).Render(headerWithSpinner(" Teams", teamW, spinChar))
	statusBar := chatStatus + teamStatus

	// Content area.
	chatView := renderChatLines(m.chatLines, chatW, contentH, m.chatScrollOffset, chatBodyStyle)
	teamView := m.renderTeamPanel(teamW, contentH, teamBodyStyle)
	middle := lipgloss.JoinHorizontal(lipgloss.Top, chatView, teamView)

	// Command bar (split across panels).
	var cmdBar string
	if m.teamEditing {
		cmdBar = chatCmdStyle.Width(chatW).Render("") +
			teamCmdStyle.Width(teamW).Render(truncateStyled("[Enter] Save  [Esc] Cancel", teamW))
	} else if m.lobbyFocus == lobbyFocusTeams {
		cmdBar = chatCmdStyle.Width(chatW).Render("[Tab] Chat") +
			teamCmdStyle.Width(teamW).Render(truncateStyled("[↑↓] Move [←→] Color [⏎] Rename", teamW))
	} else if m.mode == modeInput {
		m.input.SetWidth(max(1, chatW-2))
		inputView := truncateStyled(m.input.View(), chatW)
		cmdBar = inputView + teamCmdStyle.Width(teamW).Render("[Tab] Teams")
	} else {
		cmdBar = chatCmdStyle.Width(chatW).Render("[Enter] Chat  /help for commands") +
			teamCmdStyle.Width(teamW).Render("[Tab] Teams")
	}

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, middle, cmdBar)
}

func (m chromeModel) viewSplash(game common.Game, gameName string, sbStyle, chStyle, ciStyle lipgloss.Style, spinChar string) string {
	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	statusBar := sbStyle.Width(m.width).Render(headerWithSpinner(displayName, m.width, spinChar))

	viewportH := m.height - 2
	if viewportH < 1 {
		viewportH = 1
	}

	splashContent := game.SplashScreen()
	if splashContent == "" {
		splashContent = m.defaultSplashScreen(displayName, m.width, viewportH)
	}

	viewport := fitBlock(splashContent, m.width, viewportH)

	player := m.app.state.GetPlayer(m.playerID)
	isAdmin := player != nil && player.IsAdmin
	var cmdBar string
	if isAdmin {
		cmdBar = ciStyle.Width(m.width).Render("[Enter] Start game")
	} else {
		cmdBar = ciStyle.Width(m.width).Render("Waiting for host to start...")
	}

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, viewport, cmdBar)
}

func (m chromeModel) viewGameOver(game common.Game, gameName string, sbStyle, chStyle, ciStyle lipgloss.Style, spinChar string) string {
	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	statusText := displayName + " - Game Over"
	statusBar := sbStyle.Width(m.width).Render(headerWithSpinner(statusText, m.width, spinChar))

	viewportH := m.height - 2
	if viewportH < 1 {
		viewportH = 1
	}

	m.app.state.mu.RLock()
	results := m.app.state.GameOverResults
	m.app.state.mu.RUnlock()

	goContent := m.defaultGameOverScreen(results, m.width, viewportH)
	viewport := fitBlock(goContent, m.width, viewportH)

	remaining := 15 - int(time.Since(m.gameOverStart).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	cmdText := fmt.Sprintf("[Enter] Continue to lobby (%ds remaining)", remaining)
	cmdBar := ciStyle.Width(m.width).Render(cmdText)

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, viewport, cmdBar)
}

func (m chromeModel) viewPlaying(game common.Game, gameName string, sbStyle, chStyle, ciStyle lipgloss.Style, spinChar string) string {
	statusText := game.StatusBar(m.playerID)
	statusBar := sbStyle.Width(m.width).Render(headerWithSpinner(statusText, m.width, spinChar))

	gameH := m.width * 9 / 16
	chatH := m.height - 1 - gameH - 1
	minChatH := max(5, (m.height-2)/3)
	if chatH < minChatH {
		chatH = minChatH
		gameH = m.height - 1 - chatH - 1
		if gameH < 0 {
			gameH = 0
		}
	}

	gameView := fitBlock(game.View(m.playerID, m.width, gameH), m.width, gameH)
	chatView := renderChatLines(m.chatLines, m.width, chatH, m.chatScrollOffset, chStyle)

	var cmdBar string
	if m.mode == modeInput {
		cmdBar = truncateStyled(m.input.View(), m.width)
	} else {
		idleText := game.CommandBar(m.playerID)
		if idleText == "" {
			idleText = fmt.Sprintf("[Enter] to chat  | game: %s", gameName)
		}
		cmdBar = ciStyle.Width(m.width).Render(idleText)
	}

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, gameView, chatView, cmdBar)
}

// renderTeamPanel draws the team list panel for the lobby.
func (m chromeModel) renderTeamPanel(width, height int, baseStyle lipgloss.Style) string {
	teams := m.app.state.GetTeams()
	unassigned := m.app.state.UnassignedPlayers()
	myTeamIdx := m.app.state.PlayerTeamIndex(m.playerID)
	focused := m.lobbyFocus == lobbyFocusTeams

	var lines []string

	// Unassigned players at the top (always shown).
	unStyle := baseStyle
	if focused && myTeamIdx < 0 {
		unStyle = unStyle.Bold(true)
	}
	grayBlock := lipgloss.NewStyle().Background(lipgloss.Color("#888888")).Render("  ")
	lines = append(lines, unStyle.Width(width).Render(truncateStyled(fmt.Sprintf(" %s Unassigned", grayBlock), width)))
	for _, pid := range unassigned {
		p := m.app.state.GetPlayer(pid)
		name := pid
		if p != nil {
			name = p.Name
		}
		lines = append(lines, baseStyle.Width(width).Render(truncateStyled("    "+name, width)))
	}

	for i, team := range teams {
		lines = append(lines, baseStyle.Width(width).Render(""))
		teamColor := lipgloss.Color(team.Color)
		colorBlock := lipgloss.NewStyle().Background(teamColor).Render("  ")
		nameText := fmt.Sprintf(" %s %s", colorBlock, team.Name)
		if m.teamEditing && i == myTeamIdx {
			nameText = fmt.Sprintf(" %s %s", colorBlock, m.teamEditInput.View())
		}
		teamStyle := baseStyle
		if focused && i == myTeamIdx {
			teamStyle = teamStyle.Bold(true)
		}
		lines = append(lines, teamStyle.Width(width).Render(truncateStyled(nameText, width)))

		for _, pid := range team.Players {
			p := m.app.state.GetPlayer(pid)
			name := pid
			if p != nil {
				name = p.Name
			}
			lines = append(lines, baseStyle.Width(width).Render(truncateStyled("    "+name, width)))
		}
	}

	// Pad to fill height.
	for len(lines) < height {
		lines = append(lines, baseStyle.Width(width).Render(""))
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

// defaultSplashScreen renders a simple splash screen with the game name centered in a box.
func (m chromeModel) defaultSplashScreen(name string, width, height int) string {
	boxW := len(name) + 6
	if boxW > width-4 {
		boxW = width - 4
	}
	if boxW < 10 {
		boxW = 10
	}

	border := "+" + strings.Repeat("-", boxW-2) + "+"
	pad := boxW - 2 - len(name)
	leftPad := pad / 2
	rightPad := pad - leftPad
	middle := "|" + strings.Repeat(" ", leftPad) + name + strings.Repeat(" ", rightPad) + "|"

	boxLines := []string{border, middle, border}

	// Center vertically.
	topPad := (height - len(boxLines)) / 2
	if topPad < 0 {
		topPad = 0
	}

	var lines []string
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}
	// Center horizontally.
	leftMargin := (width - boxW) / 2
	if leftMargin < 0 {
		leftMargin = 0
	}
	prefix := strings.Repeat(" ", leftMargin)
	for _, bl := range boxLines {
		lines = append(lines, prefix+bl)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// defaultGameOverScreen renders a "GAME OVER" screen with ranked results.
func (m chromeModel) defaultGameOverScreen(results []common.GameResult, width, height int) string {
	var lines []string

	title := "G A M E   O V E R"
	titlePad := (width - len(title)) / 2
	if titlePad < 0 {
		titlePad = 0
	}
	lines = append(lines, "")
	lines = append(lines, strings.Repeat(" ", titlePad)+title)
	lines = append(lines, "")

	if len(results) > 0 {
		lines = append(lines, "")
		maxNameLen := 0
		for _, r := range results {
			if len(r.Name) > maxNameLen {
				maxNameLen = len(r.Name)
			}
		}
		for i, r := range results {
			pos := fmt.Sprintf("%d.", i+1)
			line := fmt.Sprintf("  %-3s %-*s  %s", pos, maxNameLen, r.Name, r.Result)
			lines = append(lines, line)
		}
	}

	// Center vertically.
	totalLines := len(lines)
	topPad := (height - totalLines) / 2
	if topPad < 0 {
		topPad = 0
	}
	var result []string
	for i := 0; i < topPad; i++ {
		result = append(result, "")
	}
	result = append(result, lines...)
	for len(result) < height {
		result = append(result, "")
	}
	return strings.Join(result, "\n")
}

func (m *chromeModel) syncChat() {
	// Rebuild chat from state
	history := m.app.state.GetChatHistory()
	lines := make([]string, 0, len(history))
	addLines := func(text string) {
		for _, l := range strings.Split(text, "\n") {
			lines = append(lines, l)
		}
	}
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
			addLines(fmt.Sprintf("[PM from %s] %s", from, msg.Text))
		} else if msg.IsReply {
			addLines(msg.Text)
		} else if msg.Author == "" {
			addLines(fmt.Sprintf("[system] %s", msg.Text))
		} else {
			addLines(fmt.Sprintf("<%s> %s", msg.Author, msg.Text))
		}
	}
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	m.chatLines = lines
	m.chat.SetContent(strings.Join(lines, "\n"))
	m.chat.GotoBottom()
}

func (m *chromeModel) resizeViewports() {
	phase := m.app.state.GetGamePhase()

	if !m.inActiveGame || phase == common.PhaseNone {
		// Lobby — chat shares space with fixed-width team panel.
		teamW := lobbyTeamPanelW
		if teamW > m.width-10 {
			teamW = m.width - 10
		}
		chatW := m.width - teamW
		chatH := m.height - 2
		if chatH < 1 {
			chatH = 1
		}
		m.chatH = chatH
		m.chat.SetWidth(chatW)
		m.chat.SetHeight(chatH)
		m.input.SetWidth(max(1, chatW-2))
	} else if phase == common.PhasePlaying {
		gameH := m.width * 9 / 16
		chatH := m.height - 1 - gameH - 1
		minChatH := max(5, (m.height-2)/3)
		if chatH < minChatH {
			chatH = minChatH
		}
		m.chatH = chatH
		m.chat.SetWidth(m.width)
		m.chat.SetHeight(chatH)
	} else {
		// Splash or GameOver — no chat viewport needed.
		m.chatH = 0
	}
	m.input.SetWidth(max(1, m.width-2))
}

func (m *chromeModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if text != "" {
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
			m.inputHistory = append(m.inputHistory, text)
			if len(m.inputHistory) > 50 {
				m.inputHistory = m.inputHistory[1:]
			}
		}
	}
	// In-game: return to idle after submit so keys route to the game.
	// Lobby: stay in input mode.
	if m.inActiveGame {
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

// renderChatLines renders `height` lines from `lines` with the given style, offset
// from the bottom by `scrollOffset` lines (0 = show newest). Lines above the
// buffer are rendered as blank rows.
func renderChatLines(lines []string, width, height, scrollOffset int, style lipgloss.Style) string {
	end := len(lines) - scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	visible := lines[start:end]
	result := make([]string, height)
	// visible may be shorter than height (near top of buffer); blank-pad the top
	offset := height - len(visible)
	for i := 0; i < height; i++ {
		var text string
		vi := i - offset
		if vi >= 0 && vi < len(visible) {
			text = truncateStyled(visible[vi], width)
		}
		result[i] = style.Width(width).Render(text)
	}
	return strings.Join(result, "\n")
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
