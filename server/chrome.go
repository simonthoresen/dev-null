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
	"null-space/internal/theme"
)

// lobbyTeamPanelW is the fixed width of the team panel in the lobby.

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
	theme *theme.Theme

	// Per-player plugins
	plugins     []*jsPlugin
	pluginNames []string // parallel to plugins; display names

	// Per-player shaders (post-processing, run in order)
	shaders     []common.Shader
	shaderNames []string // parallel to shaders; display names

	overlay overlayState

	// Lobby NC window and child controls.
	lobbyWindow    *NCWindow
	lobbyChatView  *NCTextView
	lobbyTeamPanel *NCTeamPanel
	lobbyCmdLabel  *NCLabel

	// Cached menu tree — rebuilt only on invalidation.
	menuCache     []common.MenuDef
	menuCacheGame common.Game // game pointer when cache was built (nil = no game)

	// Game NC window — built from WidgetNode tree via reconciler.
	// Preserves interactive control state (focus, cursor, scroll) across frames.
	gameWindow *GameNCWindow
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

	// Lobby NC controls.
	lobbyChatView := &NCTextView{
		BottomAlign: true,
		Scrollable:  true,
	}
	lobbyTeamPanel := &NCTeamPanel{}
	lobbyCmdLabel := &NCLabel{}
	lobbyWindow := &NCWindow{
		NoTopBorder: true,
		FocusIdx:    0, // chat focused by default
		Children: []GridChild{
			{Control: lobbyChatView, TabIndex: 0, Constraint: GridConstraint{
				Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth,
			}},
			{Control: &NCVDivider{Connected: true}, Constraint: GridConstraint{
				Col: 1, Row: 0, MinW: 1, WeightY: 1, Fill: FillVertical,
			}},
			{Control: lobbyTeamPanel, TabIndex: 1, Constraint: GridConstraint{
				Col: 2, Row: 0, MinW: lobbyTeamPanelW, WeightY: 1, Fill: FillVertical,
			}},
			{Control: &NCHDivider{Connected: true}, Constraint: GridConstraint{
				Col: 0, Row: 1, ColSpan: 3, MinH: 1, Fill: FillHorizontal,
			}},
			{Control: lobbyCmdLabel, Constraint: GridConstraint{
				Col: 0, Row: 2, ColSpan: 3, MinH: 1, Fill: FillHorizontal,
			}},
		},
	}

	m := chromeModel{
		app:            app,
		playerID:       playerID,
		chat:           chat,
		input:          input,
		teamEditInput:  teamInput,
		historyIdx:     -1,
		theme:          theme.Default(),
		overlay:        overlayState{OpenMenu: -1},
		lobbyWindow:    lobbyWindow,
		lobbyChatView:  lobbyChatView,
		lobbyTeamPanel: lobbyTeamPanel,
		lobbyCmdLabel:  lobbyCmdLabel,
	}
	m.syncChat()
	// Always start in lobby/input mode. GameLoadedMsg will transition
	// participating players into game mode. Late joiners stay in lobby.
	setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
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
		m.invalidateMenuCache()
		setInputStyle(&m.input, m.theme.LayerAt(1).BgC(), m.theme.LayerAt(1).FgC())
		m.mode = modeIdle
		m.lobbyFocus = lobbyFocusChat
		m.input.Blur()
		m.resizeViewports()
		return m, nil

	case common.GameUnloadedMsg:
		m.inActiveGame = false
		m.invalidateMenuCache()
		setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
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
			setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
			m.mode = modeInput
			cmd := m.input.Focus()
			m.resizeViewports()
			return m, cmd
		}
		m.resizeViewports()
		return m, nil

	case showDialogMsg:
		m.overlay.PushDialog(msg.Dialog)
		return m, nil

	case tea.MouseClickMsg:
		// NC overlay gets first crack at mouse clicks (menus, dialogs).
		if msg.Button == tea.MouseLeft {
			ncBarRow := 0 // NC bar is at row 0 in lobby, row 1 in-game
			if m.inActiveGame {
				ncBarRow = 1
			}
			if m.overlay.HandleClick(msg.X, msg.Y, ncBarRow, m.width, m.height, m.cachedMenus(), m.playerID) {
				return m, nil
			}
		}
		if !m.inActiveGame && msg.Button == tea.MouseLeft {
			// Route click through NCWindow — it sets FocusIdx and identifies the target child.
			var clickedPlayer string
			if m.lobbyWindow.HandleClick(msg.X, msg.Y) {
				if m.lobbyWindow.FocusIdx == 2 {
					// Clicked in team panel.
					cx, cy, _, _ := m.lobbyWindow.ChildRect(2)
					clickedPlayer = m.handleTeamPanelClick(msg.X-cx, msg.Y-cy)
					if clickedPlayer != "" {
						// Player name clicked — insert into chat input.
						m.lobbyFocus = lobbyFocusChat
						m.mode = modeInput
						m.input.Focus()
						if m.input.Value() == "" {
							m.input.SetValue("/msg " + clickedPlayer + " ")
							m.input.CursorEnd()
						} else {
							val := m.input.Value()
							pos := m.input.Position()
							m.input.SetValue(val[:pos] + clickedPlayer + val[pos:])
							m.input.SetCursor(pos + len(clickedPlayer))
						}
						return m, nil
					}
					// Non-player row — switch focus to teams.
					if m.lobbyFocus == lobbyFocusChat {
						m.lobbyFocus = lobbyFocusTeams
						m.input.Blur()
					}
					return m, nil
				}
				// Clicked chat panel or elsewhere — switch to chat.
				if m.lobbyFocus == lobbyFocusTeams {
					m.lobbyFocus = lobbyFocusChat
					m.mode = modeInput
					cmd := m.input.Focus()
					return m, cmd
				}
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
	if m.overlay.HandleKey(msg.String(), m.cachedMenus(), m.playerID) {
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
		key := msg.String()

		// If a game NC control has focus, route keys to it.
		if m.gameWindow != nil && m.gameWindow.Window.FocusIdx >= 0 {
			if key == "esc" {
				// Esc blurs all game controls, returning to raw OnInput mode.
				m.gameWindow.Window.FocusIdx = -1
				return m, nil
			}
			cmd := m.gameWindow.Window.HandleUpdate(msg)
			return m, cmd
		}

		// Tab/Shift-Tab cycle focus among game controls (if any).
		if key == "tab" && m.gameWindow != nil && m.gameWindow.HasFocusable() {
			cmd := m.gameWindow.Window.CycleFocus()
			return m, cmd
		}
		if key == "shift+tab" && m.gameWindow != nil && m.gameWindow.HasFocusable() {
			cmd := m.gameWindow.Window.CycleFocusBack()
			return m, cmd
		}

		switch key {
		case "enter":
			setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
			m.mode = modeInput
			cmd := m.input.Focus()
			return m, cmd
		default:
			// route to game
			m.app.state.RLock()
			game := m.app.state.ActiveGame
			m.app.state.RUnlock()
			if game != nil {
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
			setInputStyle(&m.input, m.theme.LayerAt(1).BgC(), m.theme.LayerAt(1).FgC())
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

	m.app.state.RLock()
	game := m.app.state.ActiveGame
	gameName := m.app.state.GameName
	phase := m.app.state.GamePhase
	m.app.state.RUnlock()

	barLayer := m.theme.LayerAt(1) // secondary: menu bar, status bar, command bar
	bodyLayer := m.theme.LayerAt(0) // primary: content areas
	mbStyle := barLayer.HighlightStyle()
	chStyle := bodyLayer.BaseStyle()
	ciStyle := barLayer.BaseStyle()

	if !m.inActiveGame || phase == common.PhaseNone {
		if m.mode == modeInput {
			setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
		}
		if m.teamEditing {
			setInputStyle(&m.teamEditInput, m.theme.LayerAt(0).InputBgC(), m.theme.LayerAt(0).InputFgC())
		}
	} else if m.mode == modeInput {
		setInputStyle(&m.input, m.theme.LayerAt(1).HighlightBgC(), m.theme.LayerAt(1).HighlightFgC())
	} else {
		setInputStyle(&m.input, m.theme.LayerAt(1).BgC(), m.theme.LayerAt(1).FgC())
	}

	// Build menus once per frame — passed to sub-views and overlay rendering.
	menus := m.cachedMenus()

	buf := common.NewImageBuffer(m.width, m.height)

	if !m.inActiveGame || phase == common.PhaseNone {
		m.renderLobby(buf, menus)
	} else if phase == common.PhaseSplash {
		content := m.viewSplash(menus, game, gameName, mbStyle, ciStyle)
		buf.PaintANSI(0, 0, m.width, m.height, content, nil, nil)
	} else if phase == common.PhaseGameOver {
		content := m.viewGameOver(menus, game, gameName, mbStyle, ciStyle)
		buf.PaintANSI(0, 0, m.width, m.height, content, nil, nil)
	} else {
		m.renderPlaying(buf, menus, game, gameName, mbStyle, chStyle, ciStyle)
	}

	// Post-processing shaders: run in sequence on the fully-rendered buffer.
	applyShaders(m.shaders, buf)

	// Overlay layers: render to sub-buffers, blit, then shadow via RecolorRect.
	shadowFg := m.theme.ShadowFgC()
	shadowBg := m.theme.ShadowBgC()
	if m.overlay.OpenMenu >= 0 {
		menuLayer := m.theme.LayerAt(1)
		if dd := m.overlay.RenderDropdown(menus, 1, menuLayer); dd.Content != "" {
			sub := common.NewImageBuffer(dd.Width, dd.Height)
			sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, menuLayer.FgC(), menuLayer.BgC())
			buf.Blit(dd.Col, dd.Row, sub)
			common.BlitShadow(buf, dd.Col, dd.Row, dd.Width, dd.Height, shadowFg, shadowBg)
		}
	}
	if m.overlay.HasDialog() {
		dlgLayer := m.theme.LayerAt(2)
		if dlg := m.overlay.RenderDialog(m.width, m.height, dlgLayer); dlg.Content != "" {
			sub := common.NewImageBuffer(dlg.Width, dlg.Height)
			sub.PaintANSI(0, 0, dlg.Width, dlg.Height, dlg.Content, dlgLayer.FgC(), dlgLayer.BgC())
			buf.Blit(dlg.Col, dlg.Row, sub)
			common.BlitShadow(buf, dlg.Col, dlg.Row, dlg.Width, dlg.Height, shadowFg, shadowBg)
		}
	}

	view.SetContent(buf.ToString())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	isLobby := !m.inActiveGame || phase == common.PhaseNone

	if m.mode == modeInput {
		if cursor := m.input.Cursor(); cursor != nil {
			if isLobby {
				// Command bar is 1 row above NCWindow bottom border, which is 1 row above status bar.
				// NCWindow occupies rows 1..H-2, so bottom border = H-2, cmd bar = H-3.
				cursor.Position.Y = m.height - 3
				cursor.Position.X += 1 // +1 for left window border
			} else {
				cursor.Position.Y = m.height - 2 // row above framework status bar
			}
			view.Cursor = cursor
		}
	}
	if isLobby && m.teamEditing {
		if cursor := m.teamEditInput.Cursor(); cursor != nil {
			// Position cursor on the team name row in the right panel.
			// The team panel is at col 2 in the NCWindow grid. After grid layout,
			// its X position is computed. We calculate the Y row within the panel.
			teams := m.app.state.GetTeams()
			unassigned := m.app.state.UnassignedPlayers()
			idx := m.app.state.PlayerTeamIndex(m.playerID)
			row := 1 + len(unassigned) // "Unassigned" header + player rows
			for i := 0; i < idx && i < len(teams); i++ {
				row += 1 + 1 + len(teams[i].Players) // blank + team header + members
			}
			row += 1 // blank before current team
			// NCWindow starts at y=1 (after menu bar), no top border, so content starts at y=1.
			cursor.Position.Y = 1 + row
			// Team panel X: window left border (1) + chat width + divider (1) + swatch (3) + space (1)
			// Use the grid's computed position if available.
			if len(m.lobbyWindow.Children) > 2 {
				cx, _, _, _ := m.lobbyWindow.ChildRect(2) // team panel is child index 2
				cursor.Position.X += cx + 4 // +4 for " XX " (space + swatch + space before name)
			}
			view.Cursor = cursor
		}
	}
	return view
}

// renderLobby renders the lobby view using NC controls directly into the buffer.
// Layout: row 0 = NC menu bar, rows 1..H-2 = NCWindow (chat + teams + cmd bar), row H-1 = status bar.
func (m chromeModel) renderLobby(buf *common.ImageBuffer, menus []common.MenuDef) {
	layer := m.theme.LayerAt(0)
	menuLayer := m.theme.LayerAt(1)

	// Row 0: NC action bar.
	ncBar := m.overlay.RenderMenuBar(m.width, menus, menuLayer)
	buf.PaintANSI(0, 0, m.width, 1, ncBar, menuLayer.FgC(), menuLayer.BgC())

	// Update chat view.
	m.lobbyChatView.Lines = m.chatLines
	m.lobbyChatView.ScrollOffset = m.chatScrollOffset

	// Update team panel.
	teams := m.app.state.GetTeams()
	m.lobbyTeamPanel.Teams = teams
	m.lobbyTeamPanel.Unassigned = m.app.state.UnassignedPlayers()
	m.lobbyTeamPanel.MyTeamIdx = m.app.state.PlayerTeamIndex(m.playerID)
	m.lobbyTeamPanel.PlayerID = m.playerID
	m.lobbyTeamPanel.GetPlayer = m.app.state.GetPlayer
	m.lobbyTeamPanel.Editing = m.teamEditing
	if m.teamEditing {
		m.lobbyTeamPanel.EditValue = m.teamEditInput.Value()
	}
	m.lobbyTeamPanel.ShowCreate = !m.app.state.IsSoleMemberOfTeam(m.playerID)

	// Update command bar text.
	if m.teamEditing {
		m.lobbyCmdLabel.Text = "[Enter] Save  [Esc] Cancel"
	} else if m.lobbyFocus == lobbyFocusTeams {
		m.lobbyCmdLabel.Text = "[Tab] Chat                    [\u2191\u2193] Move [\u2190\u2192] Color [\u23ce] Rename"
	} else if m.mode == modeInput {
		m.lobbyCmdLabel.Text = "[Enter] Send  [Esc] Cancel                          [Tab] Teams"
	} else {
		m.lobbyCmdLabel.Text = "[Enter] Chat  /help for commands                    [Tab] Teams"
	}

	// Set focus to match lobbyFocus.
	if m.lobbyFocus == lobbyFocusTeams {
		m.lobbyWindow.FocusIdx = 2 // team panel child index
	} else {
		m.lobbyWindow.FocusIdx = 0 // chat view child index
	}

	// Render NCWindow (rows 1 to H-2).
	windowH := m.height - 2 // minus menu bar row and status bar row
	if windowH < 3 {
		windowH = 3
	}
	m.lobbyWindow.RenderToBuf(buf, 0, 1, m.width, windowH, layer)

	// If in input mode, overlay the text input on the command bar row.
	if m.mode == modeInput && m.lobbyFocus == lobbyFocusChat {
		cmdRow := 1 + windowH - 2 // window starts at y=1, cmd bar is 1 row above bottom border
		m.input.SetWidth(max(1, m.width-4)) // -2 borders -2 padding
		inputView := m.input.View()
		inputW := m.width - 2 // inside window borders
		buf.PaintANSI(1, cmdRow, inputW, 1, truncateStyled(inputView, inputW), nil, layer.BgC())
	}

	// Status bar (last row): server info + time.
	statusLayer := m.theme.LayerAt(1)
	statusFg := statusLayer.FgC()
	statusBg := statusLayer.BgC()
	buf.Fill(0, m.height-1, m.width, 1, ' ', statusFg, statusBg, common.AttrNone)

	modeLabel := "remote"
	if m.isLocal {
		modeLabel = "local"
	}
	statusLeft := fmt.Sprintf(" null-space (%s) | %d players | uptime %s", modeLabel, m.app.state.PlayerCount(), m.app.uptime())
	buf.WriteString(0, m.height-1, statusLeft, statusFg, statusBg, common.AttrNone)

	statusRight := time.Now().Format("2006-01-02 15:04:05") + " "
	rightX := m.width - len(statusRight)
	if rightX > len(statusLeft) {
		buf.WriteString(rightX, m.height-1, statusRight, statusFg, statusBg, common.AttrNone)
	}

	// Sync chatScrollOffset back from NCTextView (it may have been changed by scroll input).
	m.chatScrollOffset = m.lobbyChatView.ScrollOffset
}

func (m chromeModel) viewSplash(menus []common.MenuDef, game common.Game, gameName string, mbStyle, ciStyle lipgloss.Style) string {
	ncBar := m.overlay.RenderMenuBar(m.width, menus, m.theme.LayerAt(1))
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

func (m chromeModel) viewGameOver(menus []common.MenuDef, game common.Game, gameName string, mbStyle, ciStyle lipgloss.Style) string {
	ncBar := m.overlay.RenderMenuBar(m.width, menus, m.theme.LayerAt(1))
	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	menuBar := mbStyle.Width(m.width).Render(truncateStyled(displayName+" - Game Over", m.width))

	viewportH := m.height - 4
	if viewportH < 1 {
		viewportH = 1
	}

	m.app.state.RLock()
	results := m.app.state.GameOverResults
	m.app.state.RUnlock()

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

func (m chromeModel) renderPlaying(buf *common.ImageBuffer, menus []common.MenuDef, game common.Game, gameName string, mbStyle, chStyle, ciStyle lipgloss.Style) {
	// Row layout: menuBar(1) + ncBar(1) + gameStatusBar(1) + game(gameH) + chat(chatH) + cmdBar(1) + statusBar(1)
	// = 5 + gameH + chatH = m.height

	// Available rows: total - menu bar - NC bar - game status bar - command bar - status bar
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

	row := 0

	// Row 0: menu bar.
	menuBar := mbStyle.Width(m.width).Render(truncateStyled(gameName, m.width))
	buf.PaintANSI(0, row, m.width, 1, menuBar, nil, nil)
	row++

	// Row 1: NC action bar.
	ncBar := m.overlay.RenderMenuBar(m.width, menus, m.theme.LayerAt(1))
	buf.PaintANSI(0, row, m.width, 1, ncBar, nil, nil)
	row++

	// Row 2: game status bar.
	gameStatusBar := mbStyle.Bold(false).Width(m.width).Render(game.StatusBar(m.playerID))
	buf.PaintANSI(0, row, m.width, 1, gameStatusBar, nil, nil)
	row++

	// Rows 3..3+gameH: game viewport — render directly into the buffer.
	if ncTree := game.RenderNC(m.playerID, m.width, gameH); ncTree != nil {
		m.gameWindow = ReconcileGameWindow(m.gameWindow, ncTree,
			func(gbuf *common.ImageBuffer, bx, by, bw, bh int) { game.Render(gbuf, m.playerID, bx, by, bw, bh) },
			func(action string) { game.OnInput(m.playerID, action) })
		m.gameWindow.Window.RenderToBuf(buf, 0, row, m.width, gameH, m.theme.LayerAt(0))
	} else {
		m.gameWindow = nil
		game.Render(buf, m.playerID, 0, row, m.width, gameH)
	}
	row += gameH

	// Chat rows.
	chatView := renderChatLines(m.chatLines, m.width, chatH, m.chatScrollOffset, chStyle)
	buf.PaintANSI(0, row, m.width, chatH, chatView, nil, nil)
	row += chatH

	// Command bar.
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
	buf.PaintANSI(0, row, m.width, 1, cmdBar, nil, nil)
	row++

	// Status bar.
	statusBar := mbStyle.Width(m.width).Align(lipgloss.Right).Render(time.Now().Format("2006-01-02 15:04:05"))
	buf.PaintANSI(0, row, m.width, 1, statusBar, nil, nil)
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
		// Lobby — NC bar (1) + window bottom border (1) + hdivider (1) + cmd bar (1) + status bar (1) = 5 overhead rows.
		// But the NCWindow handles its own internal sizing. We just need chatH for scroll math.
		windowH := m.height - 2 // minus menu bar and status bar
		chatH := max(1, windowH-4) // -1 bottom border, -1 hdivider, -1 cmd bar, -1 vdivider overhead = approx
		m.chatH = chatH
		m.input.SetWidth(max(1, m.width-4))
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
// invalidateMenuCache forces the next cachedMenus() call to rebuild.
func (m *chromeModel) invalidateMenuCache() {
	m.menuCache = nil
}

// cachedMenus returns the menu tree, rebuilding only when the active game has changed.
func (m *chromeModel) cachedMenus() []common.MenuDef {
	m.app.state.RLock()
	game := m.app.state.ActiveGame
	m.app.state.RUnlock()

	if m.menuCache != nil && m.menuCacheGame == game {
		return m.menuCache
	}

	fileItems := []common.MenuItemDef{
		{Label: "&Themes...", Handler: func(_ string) { m.showPlayerListDialog("Themes", "themes", ".json") }},
		{Label: "&Plugins...", Handler: func(_ string) { m.showPlayerListDialog("Plugins", "plugins", ".js") }},
		{Label: "&Shaders...", Handler: func(_ string) { m.showShaderDialog() }},
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
	if game != nil {
		menus = append(menus, game.Menus()...)
	}
	menus = append(menus, common.MenuDef{
		Label: "&Help",
		Items: []common.MenuItemDef{
			{Label: "&About...", Handler: func(_ string) {
				m.overlay.PushDialog(common.DialogRequest{
					Title:   "About",
					Body:    aboutLogo(),
					Buttons: []string{"OK"},
				})
			}},
		},
	})
	m.menuCache = menus
	m.menuCacheGame = game
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
	m.overlay.PushDialog(common.DialogRequest{
		Title:   title,
		Body:    body,
		Buttons: []string{"Close"},
	})
}

func (m *chromeModel) showShaderDialog() {
	available := listDir(filepath.Join(m.app.dataDir, "shaders"), ".js")
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	var lines []string
	if len(m.shaderNames) > 0 {
		lines = append(lines, "Active (in order):")
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Available:")
	if len(available) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, name := range available {
			tag := ""
			if loadedSet[name] {
				tag = "  [active]"
			}
			lines = append(lines, "  "+name+tag)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Use /shader load|unload|up|down <name>")

	m.overlay.PushDialog(common.DialogRequest{
		Title:   "Shaders",
		Body:    strings.Join(lines, "\n"),
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
		setInputStyle(&m.input, m.theme.LayerAt(1).BgC(), m.theme.LayerAt(1).FgC())
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
	if strings.HasPrefix(text, "/shader") {
		m.handleShaderCommand(text)
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
		available := theme.ListThemes(m.app.dataDir)
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
	t, err := theme.Load(path)
	if err != nil {
		m.pluginReply(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.gameWindow = nil // force rebuild with new theme
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
		pl, err := LoadPlugin(path, m.app.clock)
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

func (m *chromeModel) handleShaderCommand(input string) {
	parts := strings.Fields(input)
	// /shader with no args → list
	if len(parts) <= 1 {
		available := listDir(filepath.Join(m.app.dataDir, "shaders"), ".js")
		loadedSet := make(map[string]bool)
		for _, n := range m.shaderNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.shaderNames) == 0 {
			m.pluginReply("No shaders found in shaders/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available shaders:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader load <name|url>")
			return
		}
		nameOrURL := parts[2]
		name, path, err := resolveShaderPath(nameOrURL, m.app.dataDir)
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.shaderNames {
			if strings.EqualFold(n, name) {
				m.pluginReply(fmt.Sprintf("Shader '%s' is already loaded.", name))
				return
			}
		}
		sh, err := LoadShader(path, m.app.clock)
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed to load shader: %v", err))
			return
		}
		m.shaders = append(m.shaders, sh)
		m.shaderNames = append(m.shaderNames, name)
		m.pluginReply(fmt.Sprintf("Shader loaded: %s", name))
	case "unload":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.shaderNames {
			if strings.EqualFold(n, target) {
				m.shaders[i].Unload()
				m.shaders = append(m.shaders[:i], m.shaders[i+1:]...)
				m.shaderNames = append(m.shaderNames[:i], m.shaderNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.pluginReply(fmt.Sprintf("Shader '%s' is not loaded.", target))
			return
		}
		m.pluginReply(fmt.Sprintf("Shader unloaded: %s", target))
	case "list":
		if len(m.shaderNames) == 0 {
			m.pluginReply("No shaders currently loaded.")
			return
		}
		var lines []string
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		m.pluginReply("Active shaders (in order):\n" + strings.Join(lines, "\n"))
	case "up":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader up <name>")
			return
		}
		m.moveShader(parts[2], -1)
	case "down":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader down <name>")
			return
		}
		m.moveShader(parts[2], +1)
	default:
		m.pluginReply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list, up, down", parts[1]))
	}
}

func (m *chromeModel) moveShader(name string, delta int) {
	idx := -1
	for i, n := range m.shaderNames {
		if strings.EqualFold(n, name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.pluginReply(fmt.Sprintf("Shader '%s' is not loaded.", name))
		return
	}
	newIdx := idx + delta
	if newIdx < 0 || newIdx >= len(m.shaderNames) {
		m.pluginReply(fmt.Sprintf("Shader '%s' is already at position %d.", name, idx+1))
		return
	}
	m.shaders[idx], m.shaders[newIdx] = m.shaders[newIdx], m.shaders[idx]
	m.shaderNames[idx], m.shaderNames[newIdx] = m.shaderNames[newIdx], m.shaderNames[idx]
	m.pluginReply(fmt.Sprintf("Shader '%s' moved to position %d.", name, newIdx+1))
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
