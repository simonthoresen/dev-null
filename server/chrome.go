package server

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
)

// Lobby chrome colors — hardcoded for now; should migrate to theme palettes.
// These define the player lobby layout colors (chat panel + team panel).
var (
	defaultMenuBg = lipgloss.Color("#D8C7A0")
	defaultMenuFg = lipgloss.Color("#4A2D18")
	defaultCmdBg  = lipgloss.Color("#B8AA88")
	defaultChatBg = lipgloss.Color("#EADFC7")
	defaultChatFg = lipgloss.Color("#2C1810")

	menuBg = defaultMenuBg
	menuFg = defaultMenuFg
	cmdBg  = defaultCmdBg

	lobbyChatBarActiveBg   = lipgloss.Color("#D8C7A0")
	lobbyChatBarActiveFg   = lipgloss.Color("#4A2D18")
	lobbyChatBarInactiveBg = lipgloss.Color("#C4B898")
	lobbyChatBarInactiveFg = lipgloss.Color("#8A7A68")
	lobbyChatActiveBg      = lipgloss.Color("#EADFC7")
	lobbyChatInactiveBg    = lipgloss.Color("#E0D6BE")
	lobbyChatFg            = lipgloss.Color("#2C1810")

	lobbyTeamBarActiveBg   = lipgloss.Color("#5B7BA5")
	lobbyTeamBarActiveFg   = lipgloss.Color("#FFFFFF")
	lobbyTeamBarInactiveBg = lipgloss.Color("#8898B0")
	lobbyTeamBarInactiveFg = lipgloss.Color("#C0C8D8")
	lobbyTeamActiveBg      = lipgloss.Color("#CEDAEA")
	lobbyTeamInactiveBg    = lipgloss.Color("#C4D0E0")
	lobbyTeamFg            = lipgloss.Color("#1A2A40")
)

const lobbyTeamPanelW = 32

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


const (
	lobbyFocusChat  = 0
	lobbyFocusTeams = 1
)

type chromeModel struct {
	app      *Server
	playerID string
	isLocal  bool // true for local mode, false for SSH
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

	// Init commands from ~/.null-space/client.txt (dispatched on first tick)
	initCommands []string

	// Per-player theme
	theme *Theme

	// Per-player plugins
	plugins     []*jsPlugin
	pluginNames []string // parallel to plugins; display names

	overlay overlayState
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
	teamInput.SetVirtualCursor(false)

	m := chromeModel{
		app:           app,
		playerID:      playerID,
		chat:          chat,
		input:         input,
		teamEditInput: teamInput,
		historyIdx:    -1,
		theme:         DefaultTheme(),
		overlay:       overlayState{openMenu: -1},
	}
	m.syncChat()
	// Always start in lobby/input mode. GameLoadedMsg will transition
	// participating players into game mode. Late joiners stay in lobby.
	setInputStyle(&m.input, menuBg, menuFg)
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
		// Dispatch init commands from ~/.null-space/client.txt on the first tick
		// (after the TUI is fully running and the player is registered).
		if len(m.initCommands) > 0 {
			for _, cmd := range m.initCommands {
				if strings.HasPrefix(cmd, "/plugin") {
					m.handlePluginCommand(cmd)
				} else if strings.HasPrefix(cmd, "/theme") {
					m.handleThemeCommand(cmd)
				} else if strings.HasPrefix(cmd, "/") {
					m.dispatchPluginReply(cmd)
				}
			}
			m.initCommands = nil
		}
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

		// Run per-player plugins on this message.
		// Skip messages from this player (avoid echo loops) and command replies.
		if chatMsg.FromID != m.playerID && !chatMsg.IsReply {
			isSystem := chatMsg.Author == ""
			for _, pl := range m.plugins {
				if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
					m.dispatchPluginReply(reply)
				}
			}
		}
		return m, nil

	case common.PlayerJoinedMsg, common.PlayerLeftMsg:
		m.syncChat()
		return m, nil

	case common.TeamUpdatedMsg:
		// ClearScreen forces a full redraw. The ultraviolet renderer's
		// partial-update optimizer uses CR+LF cursor movement that
		// mispositions team panel content over SSH.
		return m, tea.ClearScreen

	case common.GameLoadedMsg:
		// This player was connected when the game loaded — they're in the game.
		m.inActiveGame = true
		setInputStyle(&m.input, cmdBg, menuFg)
		m.mode = modeIdle
		m.lobbyFocus = lobbyFocusChat
		m.input.Blur()
		m.resizeViewports()
		return m, nil

	case common.GameUnloadedMsg:
		m.inActiveGame = false
		setInputStyle(&m.input, menuBg, menuFg)
		m.mode = modeInput
		cmd := m.input.Focus()
		m.resizeViewports()
		return m, cmd

	case common.GamePhaseMsg:
		if msg.Phase == common.PhaseGameOver {
			m.gameOverStart = time.Now()
		}
		if msg.Phase == common.PhaseNone {
			m.inActiveGame = false
			setInputStyle(&m.input, menuBg, menuFg)
			m.mode = modeInput
			cmd := m.input.Focus()
			m.resizeViewports()
			return m, cmd
		}
		m.resizeViewports()
		return m, nil

	case showDialogMsg:
		m.overlay.pushDialog(msg.dialog)
		return m, nil

	case tea.MouseClickMsg:
		// NC overlay gets first crack at mouse clicks (menus, dialogs).
		if msg.Button == tea.MouseLeft {
			ncBarRow := 1 // NC bar is at row 1 (after framework menu bar at row 0)
			if m.overlay.handleClick(msg.X, msg.Y, ncBarRow, m.width, m.height, m.allMenus(), m.playerID) {
				return m, nil
			}
		}
		if !m.inActiveGame && msg.Button == tea.MouseLeft {
			teamW := lobbyTeamPanelW
			if teamW > m.width-10 {
				teamW = m.width - 10
			}
			chatW := m.width - teamW
			if msg.X >= chatW {
				// Click in team panel — handle team selection or player name insertion.
				contentY := msg.Y - 1
				panelX := msg.X - chatW
				var clickedPlayer string
				if contentY >= 0 {
					clickedPlayer = m.handleTeamPanelClick(panelX, contentY)
				}
				if clickedPlayer != "" {
					// Player name clicked — insert into chat input.
					m.lobbyFocus = lobbyFocusChat
					m.mode = modeInput
					m.input.Focus()
					if m.input.Value() == "" {
						m.input.SetValue("/msg " + clickedPlayer + " ")
						m.input.CursorEnd()
					} else {
						// Insert at cursor position.
						val := m.input.Value()
						pos := m.input.Position()
						m.input.SetValue(val[:pos] + clickedPlayer + val[pos:])
						m.input.SetCursor(pos + len(clickedPlayer))
					}
					return m, nil
				}
				// Non-player row clicked — switch focus to teams.
				if m.lobbyFocus == lobbyFocusChat {
					m.lobbyFocus = lobbyFocusTeams
					m.input.Blur()
				}
				return m, nil
			} else if m.lobbyFocus == lobbyFocusTeams {
				m.lobbyFocus = lobbyFocusChat
				m.mode = modeInput
				cmd := m.input.Focus()
				return m, cmd
			}
		}
		return m, nil

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

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

	// Ctrl+C / Ctrl+D quit from any mode.
	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		return m, tea.Quit
	}

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

	// Overlay intercepts keys when active (F10, menu navigation, dialog buttons).
	if m.overlay.handleKey(msg.String(), m.allMenus(), m.playerID) {
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
		case "enter":
			m.app.AcknowledgeGameOver(m.playerID)
		}
		return m, nil
	}

	if m.mode == modeIdle {
		switch msg.String() {
		case "enter":
			setInputStyle(&m.input, menuBg, menuFg)
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
			setInputStyle(&m.input, cmdBg, menuFg)
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

// handleTeamPanelClick maps a click position (relative to content area)
// to an action: clicking Unassigned unassigns, clicking a team joins it
// (or renames/recolors if owner), clicking a player returns their name,
// clicking [+] creates a new team.
// Returns the name of a clicked player (empty string if a non-player row was clicked).
func (m *chromeModel) handleTeamPanelClick(panelX, contentY int) string {
	teams := m.app.state.GetTeams()
	unassigned := m.app.state.UnassignedPlayers()

	// Row 0: "Unassigned" header
	row := 0
	if contentY == row {
		if m.app.state.PlayerTeamIndex(m.playerID) >= 0 {
			m.app.state.MovePlayerToTeam(m.playerID, -1)
			m.app.broadcastMsg(common.TeamUpdatedMsg{})
		}
		return ""
	}
	row++ // skip header

	// Unassigned player rows.
	for _, pid := range unassigned {
		if contentY == row {
			if p := m.app.state.GetPlayer(pid); p != nil {
				return p.Name
			}
			return pid
		}
		row++
	}

	// Each team: blank line, team header, player rows.
	// Team header layout: " ██ TeamName" → X 0=space, 1-2=color swatch, 3=space, 4+=name
	for i, team := range teams {
		if contentY == row {
			// Clicked on blank separator — ignore.
			return ""
		}
		row++ // advance past blank to team header
		if contentY == row {
			myIdx := m.app.state.PlayerTeamIndex(m.playerID)
			isFirst := m.app.state.IsFirstInTeam(m.playerID)
			if myIdx == i && isFirst {
				// Owner clicked own team header.
				if panelX >= 1 && panelX <= 2 {
					// Clicked on color swatch — cycle color.
					m.app.state.NextTeamColor(i, 1)
					m.app.broadcastMsg(common.TeamUpdatedMsg{})
				} else {
					// Clicked on team name — enter rename mode.
					m.teamEditing = true
					m.teamEditInput.SetValue(team.Name)
					m.teamEditInput.Focus()
					m.teamEditInput.CursorEnd()
				}
			} else if myIdx != i {
				m.app.state.MovePlayerToTeam(m.playerID, i)
				m.app.broadcastMsg(common.TeamUpdatedMsg{})
			}
			return ""
		}
		row++ // advance past header
		for _, pid := range team.Players {
			if contentY == row {
				if p := m.app.state.GetPlayer(pid); p != nil {
					return p.Name
				}
				return pid
			}
			row++
		}
	}

	// After all teams: blank + [+ Create Team] button.
	row++ // blank line
	if contentY == row && !m.app.state.IsSoleMemberOfTeam(m.playerID) {
		m.app.state.MovePlayerToTeam(m.playerID, len(teams))
		m.app.broadcastMsg(common.TeamUpdatedMsg{})
	}
	return ""
}

// handleMouseWheel scrolls the chat panel on wheel events.
func (m chromeModel) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	scrollAmount := 3
	chatH := max(1, m.chatH)
	switch msg.Button {
	case tea.MouseWheelUp:
		m.chatScrollOffset += scrollAmount
		maxOffset := len(m.chatLines) - chatH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.chatScrollOffset > maxOffset {
			m.chatScrollOffset = maxOffset
		}
	case tea.MouseWheelDown:
		m.chatScrollOffset -= scrollAmount
		if m.chatScrollOffset < 0 {
			m.chatScrollOffset = 0
		}
	}
	return m, nil
}

func (m chromeModel) View() tea.View {
	EnterRenderPath()
	defer LeaveRenderPath()

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
	m.app.state.mu.RUnlock()

	mbStyle := lipgloss.NewStyle().Background(defaultMenuBg).Foreground(defaultMenuFg).Bold(true)
	chStyle := lipgloss.NewStyle().Background(defaultChatBg).Foreground(defaultChatFg)
	ciStyle := lipgloss.NewStyle().Background(defaultCmdBg).Foreground(defaultMenuFg)

	if !m.inActiveGame || phase == common.PhaseNone {
		if m.mode == modeInput {
			setInputStyle(&m.input, menuBg, menuFg)
		}
		if m.teamEditing {
			setInputStyle(&m.teamEditInput, lobbyTeamActiveBg, lobbyTeamFg)
		}
	} else if m.mode == modeInput {
		setInputStyle(&m.input, menuBg, menuFg)
	} else {
		setInputStyle(&m.input, cmdBg, menuFg)
	}

	var content string

	if !m.inActiveGame || phase == common.PhaseNone {
		// === LOBBY LAYOUT (with team panel) ===
		content = m.viewLobby(mbStyle, chStyle, ciStyle, defaultChatBg)
	} else if phase == common.PhaseSplash {
		content = m.viewSplash(game, gameName, mbStyle, chStyle, ciStyle)
	} else if phase == common.PhaseGameOver {
		content = m.viewGameOver(game, gameName, mbStyle, chStyle, ciStyle)
	} else {
		// === PLAYING LAYOUT ===
		content = m.viewPlaying(game, gameName, mbStyle, chStyle, ciStyle, defaultChatBg)
	}

	// Apply overlay layers on top of the base content.
	menus := m.allMenus()
	ss := m.theme.ShadowStyle()
	if m.overlay.openMenu >= 0 {
		if ddStr, ddCol, ddRow := m.overlay.renderDropdown(menus, 1, m.theme.PaletteAt(1), m.theme); ddStr != "" {
			ddLines := strings.Split(ddStr, "\n")
			content = PlaceOverlay(ddCol, ddRow, ddStr, content)
			sh := shadowFor(ddCol, ddRow, lipgloss.Width(ddLines[0]), len(ddLines))
			content = ApplyShadow(sh.col, sh.row, sh.width, sh.height, content, ss)
		}
	}
	if m.overlay.hasDialog() {
		if dlgStr, dlgCol, dlgRow := m.overlay.renderDialog(m.width, m.height, m.theme.PaletteAt(2), m.theme); dlgStr != "" {
			dlgLines := strings.Split(dlgStr, "\n")
			content = PlaceOverlay(dlgCol, dlgRow, dlgStr, content)
			sh := shadowFor(dlgCol, dlgRow, lipgloss.Width(dlgLines[0]), len(dlgLines))
			content = ApplyShadow(sh.col, sh.row, sh.width, sh.height, content, ss)
		}
	}

	view.SetContent(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	if m.mode == modeInput {
		if cursor := m.input.Cursor(); cursor != nil {
			cursor.Position.Y = m.height - 2 // row above framework status bar
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
			row := 1 + len(unassigned) // "Unassigned" header + player rows
			for i := 0; i < idx && i < len(teams); i++ {
				row += 1 + 1 + len(teams[i].Players) // blank + team header + members
			}
			row += 1 // blank before current team
			cursor.Position.Y = 2 + row // +1 server info bar, +1 NC action bar
			cursor.Position.X += (m.width - teamW) + 5 // +1 for border │
			view.Cursor = cursor
		}
	}
	return view
}

func (m chromeModel) viewLobby(mbStyle, chStyle, ciStyle lipgloss.Style, chatBg color.Color) string {
	ncBar := m.overlay.renderNCBar(m.width, m.allMenus(), m.theme.PaletteAt(1), m.theme)
	contentH := m.height - 4 // server info + NC bar + input row + status bar
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
	// Chat panel uses default colors; team panel keeps its own blue scheme.
	var chatBarStyle, teamBarStyle lipgloss.Style
	var chatBodyStyle, teamBodyStyle lipgloss.Style
	var chatCmdStyle, teamCmdStyle lipgloss.Style

	var teamPanelBg color.Color
	if chatActive {
		chatBarStyle = mbStyle
		chatBodyStyle = chStyle
		chatCmdStyle = ciStyle
		teamBarStyle = lipgloss.NewStyle().Background(lobbyTeamBarInactiveBg).Foreground(lobbyTeamBarInactiveFg)
		teamBodyStyle = lipgloss.NewStyle().Background(lobbyTeamInactiveBg).Foreground(lobbyTeamFg)
		teamCmdStyle = lipgloss.NewStyle().Background(lobbyTeamBarInactiveBg).Foreground(lobbyTeamBarInactiveFg)
		teamPanelBg = lobbyTeamInactiveBg
	} else {
		chatBarStyle = mbStyle.Bold(false)
		chatBodyStyle = chStyle
		chatCmdStyle = ciStyle
		teamBarStyle = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg).Bold(true)
		teamBodyStyle = lipgloss.NewStyle().Background(lobbyTeamActiveBg).Foreground(lobbyTeamFg)
		teamCmdStyle = lipgloss.NewStyle().Background(lobbyTeamBarActiveBg).Foreground(lobbyTeamBarActiveFg)
		teamPanelBg = lobbyTeamActiveBg
	}

	// Menu bar (split across panels). Spinner lives in the teams bar (far right).
	modeLabel := "remote"
	if m.isLocal {
		modeLabel = "local"
	}
	menuText := fmt.Sprintf("null-space (%s) | %d players | uptime %s", modeLabel, m.app.state.PlayerCount(), m.app.uptime())
	chatMenu := chatBarStyle.Width(chatW).Render(truncateStyled(menuText, chatW))
	teamMenu := teamBarStyle.Width(teamW).Render(truncateStyled(" Teams", teamW))
	menuBar := chatMenu + teamMenu

	// Content area — each row is: chat content (chatW-1) + border "│" + team content (teamW-1) + border "│"
	// The border characters are visible foreground chars that force the
	// bubbletea/ultraviolet renderer to use absolute cursor positioning
	// (CUP) instead of CR+LF, which mispositions content over SSH.
	innerChatW := chatW - 1 // reserve 1 col for right border
	innerTeamW := teamW - 1 // reserve 1 col for right border
	chatView := renderChatLines(m.chatLines, innerChatW, contentH, m.chatScrollOffset, chatBodyStyle, chatBg)
	teamView := m.renderTeamPanel(innerTeamW, contentH, teamBodyStyle, teamPanelBg)
	chatRows := strings.Split(chatView, "\n")
	teamRows := strings.Split(teamView, "\n")
	chatBorder := chatBodyStyle.Render("│")
	teamBorder := teamBodyStyle.Render("│")
	middleRows := make([]string, contentH)
	for i := 0; i < contentH; i++ {
		var c, t string
		if i < len(chatRows) {
			c = chatRows[i]
		}
		if i < len(teamRows) {
			t = teamRows[i]
		}
		middleRows[i] = c + chatBorder + t + teamBorder
	}
	middle := strings.Join(middleRows, "\n")

	// Input row (split across panels).
	var inputRow string
	if m.teamEditing {
		inputRow = chatCmdStyle.Width(chatW).Render("") +
			teamCmdStyle.Width(teamW).Render(truncateStyled("[Enter] Save  [Esc] Cancel", teamW))
	} else if m.lobbyFocus == lobbyFocusTeams {
		inputRow = chatCmdStyle.Width(chatW).Render("[Tab] Chat") +
			teamCmdStyle.Width(teamW).Render(truncateStyled("[↑↓] Move [←→] Color [⏎] Rename", teamW))
	} else if m.mode == modeInput {
		m.input.SetWidth(max(1, chatW-2))
		inputView := truncateStyled(m.input.View(), chatW)
		inputRow = inputView + teamCmdStyle.Width(teamW).Render("[Tab] Teams")
	} else {
		inputRow = chatCmdStyle.Width(chatW).Render("[Enter] Chat  /help for commands") +
			teamCmdStyle.Width(teamW).Render("[Tab] Teams")
	}

	statusBar := mbStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04:05"))

	return lipgloss.JoinVertical(lipgloss.Left, menuBar, ncBar, middle, inputRow, statusBar)
}

func (m chromeModel) viewSplash(game common.Game, gameName string, mbStyle, chStyle, ciStyle lipgloss.Style) string {
	ncBar := m.overlay.renderNCBar(m.width, m.allMenus(), m.theme.PaletteAt(1), m.theme)
	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	menuBar := mbStyle.Width(m.width).Render(truncateStyled(displayName, m.width))

	viewportH := m.height - 4
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

	statusBar := mbStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04:05"))

	return lipgloss.JoinVertical(lipgloss.Left, menuBar, ncBar, viewport, cmdBar, statusBar)
}

func (m chromeModel) viewGameOver(game common.Game, gameName string, mbStyle, chStyle, ciStyle lipgloss.Style) string {
	ncBar := m.overlay.renderNCBar(m.width, m.allMenus(), m.theme.PaletteAt(1), m.theme)
	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	menuBar := mbStyle.Width(m.width).Render(truncateStyled(displayName+" - Game Over", m.width))

	viewportH := m.height - 4
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
	cmdBar := ciStyle.Width(m.width).Render(fmt.Sprintf("[Enter] Continue to lobby (%ds remaining)", remaining))

	statusBar := mbStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04:05"))

	return lipgloss.JoinVertical(lipgloss.Left, menuBar, ncBar, viewport, cmdBar, statusBar)
}

func (m chromeModel) viewPlaying(game common.Game, gameName string, mbStyle, chStyle, ciStyle lipgloss.Style, chatBg color.Color) string {
	ncBar := m.overlay.renderNCBar(m.width, m.allMenus(), m.theme.PaletteAt(1), m.theme)
	menuBar := mbStyle.Width(m.width).Render(truncateStyled(gameName, m.width))
	gameStatusBar := mbStyle.Bold(false).Width(m.width).Render(game.StatusBar(m.playerID))

	// Available rows: total - NC bar - menu bar - game status bar - command bar - status bar
	gameH := m.width * 9 / 16
	chatH := m.height - 5 - gameH
	minChatH := max(5, (m.height-5)/3)
	if chatH < minChatH {
		chatH = minChatH
		gameH = m.height - 4 - chatH
		if gameH < 0 {
			gameH = 0
		}
	}

	gameView := fitBlock(game.View(m.playerID, m.width, gameH), m.width, gameH)
	chatView := renderChatLines(m.chatLines, m.width, chatH, m.chatScrollOffset, chStyle, chatBg)

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

	statusBar := mbStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04:05"))

	return lipgloss.JoinVertical(lipgloss.Left, menuBar, ncBar, gameStatusBar, gameView, chatView, cmdBar, statusBar)
}

// renderTeamPanel draws the team list panel for the lobby.
func (m chromeModel) renderTeamPanel(width, height int, baseStyle lipgloss.Style, bg color.Color) string {
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
	grayBlock := colorSwatch(lipgloss.Color("#888888"), bg)
	lines = append(lines, unStyle.Width(width).Render(truncateStyled(fmt.Sprintf(" %s Unassigned", grayBlock), width)))
	for _, pid := range unassigned {
		p := m.app.state.GetPlayer(pid)
		name := pid
		if p != nil {
			name = p.Name
		}
		lines = append(lines, baseStyle.Width(width).Render(truncateStyled("    "+name, width)))
	}

	blank := strings.Repeat(" ", width)
	blankLine := baseStyle.Width(width).Render(blank)
	for i, team := range teams {
		lines = append(lines, blankLine)
		block := colorSwatch(lipgloss.Color(team.Color), bg)
		nameText := fmt.Sprintf(" %s %s", block, team.Name)
		if m.teamEditing && i == myTeamIdx {
			nameText = fmt.Sprintf(" %s %s", block, m.teamEditInput.Value())
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

	// [+ Create Team] button — shown unless player is already sole member of a team.
	if !m.app.state.IsSoleMemberOfTeam(m.playerID) {
		lines = append(lines, blankLine)
		btnStyle := baseStyle.Faint(true)
		lines = append(lines, btnStyle.Width(width).Render(truncateStyled(" [+ Create Team]", width)))
	}

	// Pad to fill height. Use lipgloss-styled blanks (not raw ANSI) to
	// ensure the ultraviolet renderer doesn't use CR+LF movement
	// optimization that mispositions content over SSH.
	for len(lines) < height {
		lines = append(lines, blankLine)
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

	lines = append(lines, "")
	figletTitle := strings.TrimRight(Figlet("GAME OVER", "slant"), "\n")
	figletLines := strings.Split(figletTitle, "\n")
	maxW := 0
	for _, l := range figletLines {
		if len(l) > maxW {
			maxW = len(l)
		}
	}
	if figletTitle != "" && maxW <= width {
		pad := strings.Repeat(" ", max(0, (width-maxW)/2))
		for _, l := range figletLines {
			lines = append(lines, pad+l)
		}
	} else {
		title := "G A M E   O V E R"
		pad := strings.Repeat(" ", max(0, (width-len(title))/2))
		lines = append(lines, pad+title)
	}
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
		// Lobby — server info + NC bar + input row + status bar = 4 overhead rows.
		teamW := lobbyTeamPanelW
		if teamW > m.width-10 {
			teamW = m.width - 10
		}
		chatW := m.width - teamW
		chatH := m.height - 4
		if chatH < 1 {
			chatH = 1
		}
		m.chatH = chatH
		m.chat.SetWidth(chatW)
		m.chat.SetHeight(chatH)
		m.input.SetWidth(max(1, chatW-2))
	} else if phase == common.PhasePlaying {
		// NC bar + menu bar + game status bar + command bar + status bar = 5 overhead rows.
		gameH := m.width * 9 / 16
		chatH := m.height - 5 - gameH
		minChatH := max(5, (m.height-5)/3)
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

// allMenus returns the full ordered list of menus for the NC action bar:
// the framework "File" menu followed by any game-registered menus.
func (m *chromeModel) allMenus() []common.MenuDef {
	fileItems := []common.MenuItemDef{
		{Label: "&Themes...", Handler: func(_ string) { m.showPlayerListDialog("Themes", "themes", ".json") }},
		{Label: "&Plugins...", Handler: func(_ string) { m.showPlayerListDialog("Plugins", "plugins", ".js") }},
		{Label: "---"},
	}
	if m.isLocal {
		fileItems = append(fileItems, common.MenuItemDef{
			Label: "&Quit",
			Handler: func(_ string) {
				// Ctrl+C is the reliable quit path in local mode.
			},
		})
	} else {
		fileItems = append(fileItems, common.MenuItemDef{
			Label: "&Disconnect",
			Handler: func(playerID string) {
				go m.app.kickPlayer(playerID)
			},
		})
	}
	menus := []common.MenuDef{{Label: "&File", Items: fileItems}}
	m.app.state.mu.RLock()
	game := m.app.state.ActiveGame
	m.app.state.mu.RUnlock()
	if game != nil {
		menus = append(menus, game.Menus()...)
	}
	menus = append(menus, common.MenuDef{
		Label: "&Help",
		Items: []common.MenuItemDef{
			{Label: "&About...", Handler: func(_ string) {
				m.overlay.pushDialog(common.DialogRequest{
					Title:   "About",
					Body:    aboutLogo(),
					Buttons: []string{"OK"},
				})
			}},
		},
	})
	return menus
}

func (m *chromeModel) showPlayerListDialog(title, subdir, ext string) {
	dir := filepath.Join(m.app.dataDir, subdir)
	items := listDir(dir, ext)
	body := "(empty)"
	if len(items) > 0 {
		var lines []string
		for _, name := range items {
			lines = append(lines, "  "+name)
		}
		body = strings.Join(lines, "\n")
	}
	m.overlay.pushDialog(common.DialogRequest{
		Title:   title,
		Body:    body,
		Buttons: []string{"Close"},
	})
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
		setInputStyle(&m.input, cmdBg, menuFg)
		m.mode = modeIdle
		m.input.Blur()
	}
	if text == "" {
		return
	}
	// Handle /plugin and /theme locally (per-player).
	if strings.HasPrefix(text, "/plugin") {
		m.handlePluginCommand(text)
		return
	}
	if strings.HasPrefix(text, "/theme") {
		m.handleThemeCommand(text)
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

func (m *chromeModel) handleThemeCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := ListThemes(m.app.dataDir)
		if len(available) == 0 {
			m.pluginReply("No themes found in themes/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if strings.EqualFold(name, m.theme.Name) {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available themes:\n" + strings.Join(lines, "\n"))
		return
	}
	name := parts[1]
	path := filepath.Join(m.app.dataDir, "themes", name+".json")
	t, err := LoadTheme(path)
	if err != nil {
		m.pluginReply(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.pluginReply(fmt.Sprintf("Theme changed to: %s", t.Name))
}

func (m *chromeModel) pluginReply(text string) {
	msg := common.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: text}
	m.app.sendToPlayer(m.playerID, common.ChatMsg{Msg: msg})
}

func (m *chromeModel) handlePluginCommand(input string) {
	parts := strings.Fields(input)
	// /plugin with no args → list
	if len(parts) <= 1 {
		available := listDir(filepath.Join(m.app.dataDir, "plugins"), ".js")
		loadedSet := make(map[string]bool)
		for _, n := range m.pluginNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.pluginNames) == 0 {
			m.pluginReply("No plugins found in plugins/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [loaded]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available plugins:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.pluginReply("Usage: /plugin load <name|url>")
			return
		}
		nameOrURL := parts[2]
		name, path, err := resolvePluginPath(nameOrURL, m.app.dataDir)
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.pluginNames {
			if strings.EqualFold(n, name) {
				m.pluginReply(fmt.Sprintf("Plugin '%s' is already loaded.", name))
				return
			}
		}
		pl, err := LoadPlugin(path)
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed to load plugin: %v", err))
			return
		}
		m.plugins = append(m.plugins, pl)
		m.pluginNames = append(m.pluginNames, name)
		m.pluginReply(fmt.Sprintf("Plugin loaded: %s", name))
	case "unload":
		if len(parts) < 3 {
			m.pluginReply("Usage: /plugin unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.pluginNames {
			if strings.EqualFold(n, target) {
				m.plugins[i].Unload()
				m.plugins = append(m.plugins[:i], m.plugins[i+1:]...)
				m.pluginNames = append(m.pluginNames[:i], m.pluginNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.pluginReply(fmt.Sprintf("Plugin '%s' is not loaded.", target))
			return
		}
		m.pluginReply(fmt.Sprintf("Plugin unloaded: %s", target))
	case "list":
		if len(m.pluginNames) == 0 {
			m.pluginReply("No plugins currently loaded.")
			return
		}
		m.pluginReply("Loaded plugins: " + strings.Join(m.pluginNames, ", "))
	default:
		m.pluginReply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", parts[1]))
	}
}

// dispatchPluginReply handles a string returned by a plugin's onMessage hook.
// If it starts with "/" it's treated as a command, otherwise as chat.
func (m *chromeModel) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
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


// renderChatLines renders `height` lines from `lines` with the given style, offset
// from the bottom by `scrollOffset` lines (0 = show newest). Lines above the
// buffer are rendered as blank rows.
// colorToANSIBg returns a raw ANSI truecolor background escape sequence for a
// color.Color value, e.g. "\x1b[48;2;234;223;199m".
func colorToANSIBg(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// colorSwatch returns a 2-char color block ("  ") that temporarily switches
// to swatchColor and then restores parentBg. Uses raw ANSI so it can be
// embedded inside a lipgloss.Render() call without producing a \x1b[m reset
// that would kill the outer style. The visual width is 2 characters.
func colorSwatch(swatchColor, parentBg color.Color) string {
	sr, sg, sb, _ := swatchColor.RGBA()
	return colorToANSIBg(parentBg) + fmt.Sprintf("\x1b[48;2;%d;%d;%dm  ", sr>>8, sg>>8, sb>>8) + colorToANSIBg(parentBg)
}

func renderChatLines(lines []string, width, height, scrollOffset int, style lipgloss.Style, _ color.Color) string {
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
	blank := strings.Repeat(" ", width)
	for i := 0; i < height; i++ {
		vi := i - offset
		if vi >= 0 && vi < len(visible) {
			result[i] = style.Width(width).Render(truncateStyled(visible[vi], width))
		} else {
			result[i] = style.Width(width).Render(blank)
		}
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
