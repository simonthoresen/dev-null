package chrome

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
	"null-space/internal/console"
	"null-space/internal/engine"
	"null-space/internal/state"
	"null-space/internal/theme"
	"null-space/internal/widget"
)

// ServerAPI is the interface that the chrome model uses to interact with the server.
type ServerAPI interface {
	State() *state.CentralState
	Clock() common.Clock
	DataDir() string
	Uptime() string

	// Communication
	BroadcastChat(msg common.Message)
	BroadcastMsg(msg tea.Msg)
	SendToPlayer(playerID string, msg tea.Msg)
	ServerLog(text string)

	// Commands
	TabCandidates(input string, playerNames []string) (prefix string, candidates []string)
	DispatchCommand(input string, ctx common.CommandContext)

	// Game lifecycle
	StartGame()
	AcknowledgeGameOver(playerID string)
	SuspendGame(saveName string) error
	ResumeGame(gameName, saveName string) error
	ListSuspends() []state.SuspendInfo

	// Session management
	KickPlayer(playerID string) error
}

// lobbyTeamPanelW is the fixed width of the team panel in the lobby.
const lobbyTeamPanelW = 32

// SetInputStyle applies matching background/foreground to all textinput sub-styles
// and switches to the real terminal cursor (not the virtual cursor).
//
// The virtual cursor's TextStyle (used during blink-hide) has no background by
// default, causing the character under the cursor to flash to terminal default
// (black) on every blink. Using the real cursor avoids this entirely: all text
// renders with a solid background, and the terminal handles cursor blinking.
func SetInputStyle(input *textinput.Model, bg, fg color.Color) {
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



// Model is the Bubble Tea model for a player's chrome (lobby, game viewport, etc.).
type Model struct {
	api      ServerAPI
	playerID string
	IsLocal  bool // true for local mode, false for SSH
	width  int
	height int

	// inActiveGame is true when this player is participating in the current game.
	// Late joiners (connected after GameLoadedMsg) stay in lobby mode.
	inActiveGame bool

	chatLines        []string // buffered chat lines visible to this player (max 200)
	chatScrollOffset int      // lines scrolled up from bottom (0 = bottom)
	chatH            int      // current chat panel height (updated in resizeViewports)

	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Lobby team panel state
	teamEditing   bool // true when renaming a team
	teamEditInput textinput.Model

	// Game-over countdown tracking
	gameOverStart time.Time

	// Init commands from ~/.null-space/client.txt (dispatched on first tick)
	InitCommands []string

	// Per-player theme
	theme *theme.Theme

	// Per-player plugins
	plugins     []*engine.JSPlugin
	pluginNames []string // parallel to plugins; display names

	// Per-player shaders (post-processing, run in order)
	shaders     []common.Shader
	shaderNames []string // parallel to shaders; display names

	overlay widget.OverlayState

	// Lobby NC window and child controls.
	lobbyWindow    *widget.Window
	lobbyChatView  *widget.TextView
	lobbyTeamPanel *widget.TeamPanel
	lobbyInput     *widget.CommandInput
	lobbyScreen    *widget.Screen
	lobbyMenuBar   *widget.MenuBar
	lobbyStatusBar *widget.StatusBar

	// Playing view NC controls.
	playingScreen    *widget.Screen
	playingMenuBar   *widget.MenuBar
	playingStatusBar *widget.StatusBar
	playingWindow    *widget.Window
	playingGameView  *widget.GameView
	playingChatView  *widget.TextView
	playingInput     *widget.CommandInput

	// Cached menu tree — rebuilt only on invalidation.
	menuCache     []common.MenuDef
	menuCacheGame common.Game // game pointer when cache was built (nil = no game)

	// Game NC window — built from WidgetNode tree via reconciler.
	// Preserves interactive control state (focus, cursor, scroll) across frames.
	gameWindow *widget.GameWindow
}

func NewModel(api ServerAPI, playerID string) Model {
	teamInput := textinput.New()
	teamInput.Prompt = ""
	teamInput.CharLimit = 20
	teamInput.SetWidth(20)
	teamInput.SetVirtualCursor(false)

	// Lobby NC controls.
	lobbyChatView := &widget.TextView{
		BottomAlign: true,
		Scrollable:  true,
	}
	lobbyTeamPanel := &widget.TeamPanel{}
	lobbyInputModel := new(textinput.Model)
	*lobbyInputModel = textinput.New()
	lobbyInputModel.Prompt = ""
	lobbyInputModel.Placeholder = ""
	lobbyInputModel.CharLimit = 256
	lobbyInputModel.SetWidth(78)
	lobbyInputCtrl := &widget.CommandInput{TextInput: widget.TextInput{Model: lobbyInputModel}}
	lobbyWindow := &widget.Window{
		NoTopBorder: true,
		FocusIdx:    2, // lobbyInput focused by default
		Children: []widget.GridChild{
			{Control: lobbyChatView, TabIndex: 1, Constraint: widget.GridConstraint{
				Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.VDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 1, Row: 0, MinW: 1, WeightY: 1, Fill: widget.FillVertical,
			}},
			{Control: lobbyTeamPanel, TabIndex: 2, Constraint: widget.GridConstraint{
				Col: 2, Row: 0, MinW: lobbyTeamPanelW, WeightY: 1, Fill: widget.FillVertical,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 1, ColSpan: 3, MinH: 1, Fill: widget.FillHorizontal,
			}},
			{Control: lobbyInputCtrl, TabIndex: 0, Constraint: widget.GridConstraint{
				Col: 0, Row: 2, ColSpan: 3, WeightX: 1, Fill: widget.FillHorizontal,
			}},
		},
	}

	lobbyMenuBar := &widget.MenuBar{}
	lobbyStatusBar := &widget.StatusBar{}
	lobbyScreen := &widget.Screen{
		MenuBar:   lobbyMenuBar,
		Window:    lobbyWindow,
		StatusBar: lobbyStatusBar,
	}

	// Playing view NC controls.
	playingInputModel := new(textinput.Model)
	*playingInputModel = textinput.New()
	playingInputModel.Prompt = ""
	playingInputModel.Placeholder = ""
	playingInputModel.CharLimit = 256
	playingInputModel.SetWidth(78)
	playingInputCtrl := &widget.CommandInput{TextInput: widget.TextInput{Model: playingInputModel}}

	playingGameView := &widget.GameView{}
	playingGameView.SetFocusable(true)
	playingChatView := &widget.TextView{BottomAlign: true, Scrollable: true}
	playingWindow := &widget.Window{
		FocusIdx: 0, // gameview focused by default
		Children: []widget.GridChild{
			{Control: playingGameView, TabIndex: 0, Constraint: widget.GridConstraint{
				Col: 0, Row: 0, WeightX: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 1, MinH: 1, Fill: widget.FillHorizontal,
			}},
			{Control: playingChatView, TabIndex: 1, Constraint: widget.GridConstraint{
				Col: 0, Row: 2, WeightX: 1, WeightY: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 3, MinH: 1, Fill: widget.FillHorizontal,
			}},
			{Control: playingInputCtrl, TabIndex: 2, Constraint: widget.GridConstraint{
				Col: 0, Row: 4, WeightX: 1, Fill: widget.FillHorizontal,
			}},
		},
	}
	playingMenuBar := &widget.MenuBar{}
	playingStatusBar := &widget.StatusBar{}
	playingScreen := &widget.Screen{
		MenuBar:   playingMenuBar,
		Window:    playingWindow,
		StatusBar: playingStatusBar,
	}

	m := Model{
		api:           api,
		playerID:      playerID,
		teamEditInput: teamInput,
		theme:         theme.Default(),
		overlay:        widget.OverlayState{OpenMenu: -1},
		lobbyWindow:    lobbyWindow,
		lobbyChatView:  lobbyChatView,
		lobbyTeamPanel: lobbyTeamPanel,
		lobbyInput:     lobbyInputCtrl,
		lobbyScreen:      lobbyScreen,
		lobbyMenuBar:     lobbyMenuBar,
		lobbyStatusBar:   lobbyStatusBar,
		playingScreen:    playingScreen,
		playingMenuBar:   playingMenuBar,
		playingStatusBar: playingStatusBar,
		playingWindow:    playingWindow,
		playingGameView:  playingGameView,
		playingChatView:  playingChatView,
		playingInput:     playingInputCtrl,
	}
	lobbyMenuBar.Overlay = &m.overlay
	playingMenuBar.Overlay = &m.overlay

	// Wire lobby command input callbacks.
	lobbyInputCtrl.OnSubmit = m.dispatchInput
	lobbyInputCtrl.OnTab = m.lobbyTabComplete

	// Wire playing command input callbacks.
	playingInputCtrl.OnSubmit = func(text string) {
		m.dispatchInput(text)
		// Return focus to gameview after submitting.
		m.playingWindow.FocusIdx = 0
		m.playingInput.Model.Blur()
	}
	playingInputCtrl.OnEsc = func() {
		// Return focus to gameview on Esc.
		m.playingWindow.FocusIdx = 0
		m.playingInput.Model.Blur()
	}
	playingInputCtrl.OnTab = m.lobbyTabComplete // same tab completion logic

	// Wire team panel callbacks.
	lobbyTeamPanel.OnMoveToTeam = func(teamIdx int) {
		m.api.State().MovePlayerToTeam(m.playerID, teamIdx)
		m.api.BroadcastMsg(common.TeamUpdatedMsg{})
	}
	lobbyTeamPanel.OnCreateTeam = func() {
		m.api.State().MovePlayerToTeam(m.playerID, m.api.State().TeamCount())
		m.api.BroadcastMsg(common.TeamUpdatedMsg{})
	}
	lobbyTeamPanel.OnCycleColor = func(direction int) {
		idx := m.api.State().PlayerTeamIndex(m.playerID)
		m.api.State().NextTeamColor(idx, direction)
		m.api.BroadcastMsg(common.TeamUpdatedMsg{})
	}
	lobbyTeamPanel.OnStartRename = func() {
		idx := m.api.State().PlayerTeamIndex(m.playerID)
		teams := m.api.State().GetTeams()
		if idx >= 0 && idx < len(teams) {
			m.teamEditing = true
			m.teamEditInput.SetValue(teams[idx].Name)
			m.teamEditInput.Focus()
			m.teamEditInput.CursorEnd()
		}
	}
	lobbyTeamPanel.IsSoleMember = func() bool {
		return m.api.State().IsSoleMemberOfTeam(m.playerID)
	}
	lobbyTeamPanel.IsFirstInTeam = func() bool {
		return m.api.State().IsFirstInTeam(m.playerID)
	}

	m.syncChat()
	m.lobbyInput.Model.Focus()
	return m
}

func (m Model) Init() tea.Cmd {
	return m.lobbyInput.Model.Focus() // starts cursor blink in lobby
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if len(m.InitCommands) > 0 {
			for _, cmd := range m.InitCommands {
				if strings.HasPrefix(cmd, "/plugin") {
					m.handlePluginCommand(cmd)
				} else if strings.HasPrefix(cmd, "/theme") {
					m.handleThemeCommand(cmd)
				} else if strings.HasPrefix(cmd, "/") {
					m.dispatchPluginReply(cmd)
				}
			}
			m.InitCommands = nil
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
			if p := m.api.State().GetPlayer(from); p != nil {
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
		m.lobbyInput.Model.Blur()
		// Focus the playing gameview.
		m.playingWindow.FocusIdx = 0
		m.resizeViewports()
		return m, nil

	case common.GameUnloadedMsg:
		m.inActiveGame = false
		m.invalidateMenuCache()
		m.lobbyWindow.FocusIdx = 4 // lobbyInput
		cmd := m.lobbyInput.Model.Focus()
		m.playingInput.Model.Blur()
		m.resizeViewports()
		return m, cmd

	case common.GamePhaseMsg:
		if msg.Phase == common.PhaseGameOver {
			m.gameOverStart = time.Now()
		}
		if msg.Phase == common.PhaseNone {
			m.inActiveGame = false
			m.lobbyWindow.FocusIdx = 4
			cmd := m.lobbyInput.Model.Focus()
			m.resizeViewports()
			return m, cmd
		}
		if msg.Phase == common.PhaseSuspended {
			// Return to lobby view but keep inActiveGame true so resume works.
			m.lobbyWindow.FocusIdx = 4
			cmd := m.lobbyInput.Model.Focus()
			m.playingInput.Model.Blur()
			m.resizeViewports()
			return m, cmd
		}
		m.resizeViewports()
		return m, nil

	case widget.ShowDialogMsg:
		m.overlay.PushDialog(msg.Dialog)
		return m, nil

	case tea.MouseClickMsg:
		// NC overlay gets first crack at mouse clicks (menus, dialogs).
		if msg.Button == tea.MouseLeft {
			if m.overlay.HandleClick(msg.X, msg.Y, 0, m.width, m.height, m.cachedMenus(), m.playerID) {
				return m, nil
			}
		}
		if !m.inActiveGame && msg.Button == tea.MouseLeft {
			// Route click through NCWindow — it sets FocusIdx and identifies the target child.
			var clickedPlayer string
			if m.lobbyWindow.HandleClick(msg.X, msg.Y) {
				if m.lobbyWindow.FocusIdx == 2 {
					// Clicked in team panel — check if a player name was clicked.
					cx, cy, _, _ := m.lobbyWindow.ChildRect(2)
					clickedPlayer = m.handleTeamPanelClick(msg.X-cx, msg.Y-cy)
					if clickedPlayer != "" {
						// Player name clicked — insert into chat input.
						m.lobbyWindow.FocusIdx = 4
						m.lobbyInput.Model.Focus()
						if m.lobbyInput.Model.Value() == "" {
							m.lobbyInput.Model.SetValue("/msg " + clickedPlayer + " ")
							m.lobbyInput.Model.CursorEnd()
						} else {
							val := m.lobbyInput.Model.Value()
							pos := m.lobbyInput.Model.Position()
							m.lobbyInput.Model.SetValue(val[:pos] + clickedPlayer + val[pos:])
							m.lobbyInput.Model.SetCursor(pos + len(clickedPlayer))
						}
						return m, nil
					}
				}
			}
		}
		return m, nil

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Forward other messages to textinput for cursor blink etc.
	if !m.inActiveGame {
		updated, cmd := m.lobbyInput.Model.Update(msg)
		*m.lobbyInput.Model = updated
		return m, cmd
	}
	// Playing: forward to playing command input (for cursor blink).
	updated, cmd := m.playingInput.Model.Update(msg)
	*m.playingInput.Model = updated
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	phase := m.api.State().GetGamePhase()

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

	// Lobby: delegate to the NCWindow which routes to the focused child
	// (NCCommandInput or NCTeamPanel). Tab cycling is handled by the window.
	if !m.inActiveGame {
		cmd := m.lobbyWindow.HandleUpdate(msg)
		// Reset tab candidates on non-tab keys.
		if msg.String() != "tab" {
			m.tabCandidates = nil
		}
		return m, cmd
	}

	// Splash phase — admin can press Enter to start, others wait.
	if m.inActiveGame && phase == common.PhaseSplash {
		switch msg.String() {
		case "enter":
			player := m.api.State().GetPlayer(m.playerID)
			if player != nil && player.IsAdmin {
				m.api.StartGame()
			}
		}
		return m, nil
	}

	// Game-over phase — Enter acknowledges.
	if m.inActiveGame && phase == common.PhaseGameOver {
		switch msg.String() {
		case "enter":
			m.api.AcknowledgeGameOver(m.playerID)
		}
		return m, nil
	}

	// Playing: delegate to the playing NCWindow which routes to the focused child
	// (GameView, NCCommandInput, or NC-tree controls).
	cmd := m.playingWindow.HandleUpdate(msg)
	// Reset tab candidates on non-tab keys.
	if msg.String() != "tab" {
		m.tabCandidates = nil
	}
	return m, cmd
}

func (m Model) handleTeamEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := strings.TrimSpace(m.teamEditInput.Value())
		if name != "" {
			idx := m.api.State().PlayerTeamIndex(m.playerID)
			if m.api.State().RenameTeam(idx, name) {
				m.api.BroadcastMsg(common.TeamUpdatedMsg{})
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
func (m *Model) handleTeamPanelClick(panelX, contentY int) string {
	teams := m.api.State().GetTeams()
	unassigned := m.api.State().UnassignedPlayers()

	// Row 0: "Unassigned" header
	row := 0
	if contentY == row {
		if m.api.State().PlayerTeamIndex(m.playerID) >= 0 {
			m.api.State().MovePlayerToTeam(m.playerID, -1)
			m.api.BroadcastMsg(common.TeamUpdatedMsg{})
		}
		return ""
	}
	row++ // skip header

	// Unassigned player rows.
	for _, pid := range unassigned {
		if contentY == row {
			if p := m.api.State().GetPlayer(pid); p != nil {
				return p.Name
			}
			return pid
		}
		row++
	}

	// Each team: blank line, team header, player rows.
	// Team header layout: " XX TeamName" -> X 0=space, 1-2=color swatch, 3=space, 4+=name
	for i, team := range teams {
		if contentY == row {
			// Clicked on blank separator — ignore.
			return ""
		}
		row++ // advance past blank to team header
		if contentY == row {
			myIdx := m.api.State().PlayerTeamIndex(m.playerID)
			isFirst := m.api.State().IsFirstInTeam(m.playerID)
			if myIdx == i && isFirst {
				// Owner clicked own team header.
				if panelX >= 1 && panelX <= 2 {
					// Clicked on color swatch — cycle color.
					m.api.State().NextTeamColor(i, 1)
					m.api.BroadcastMsg(common.TeamUpdatedMsg{})
				} else {
					// Clicked on team name — enter rename mode.
					m.teamEditing = true
					m.teamEditInput.SetValue(team.Name)
					m.teamEditInput.Focus()
					m.teamEditInput.CursorEnd()
				}
			} else if myIdx != i {
				m.api.State().MovePlayerToTeam(m.playerID, i)
				m.api.BroadcastMsg(common.TeamUpdatedMsg{})
			}
			return ""
		}
		row++ // advance past header
		for _, pid := range team.Players {
			if contentY == row {
				if p := m.api.State().GetPlayer(pid); p != nil {
					return p.Name
				}
				return pid
			}
			row++
		}
	}

	// After all teams: blank + [+ Create Team] button.
	row++ // blank line
	if contentY == row && !m.api.State().IsSoleMemberOfTeam(m.playerID) {
		m.api.State().MovePlayerToTeam(m.playerID, len(teams))
		m.api.BroadcastMsg(common.TeamUpdatedMsg{})
	}
	return ""
}

// handleMouseWheel scrolls the chat panel on wheel events.
func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
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

func (m Model) View() tea.View {
	console.EnterRenderPath()
	defer console.LeaveRenderPath()

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading null-space...")
		view.AltScreen = true
		return view
	}

	m.api.State().RLock()
	game := m.api.State().ActiveGame
	gameName := m.api.State().GameName
	phase := m.api.State().GamePhase
	m.api.State().RUnlock()

	if (!m.inActiveGame || phase == common.PhaseNone || phase == common.PhaseSuspended) && m.teamEditing {
		SetInputStyle(&m.teamEditInput, m.theme.LayerAt(0).InputBgC(), m.theme.LayerAt(0).InputFgC())
	}

	// Build menus once per frame — passed to sub-views and overlay rendering.
	menus := m.cachedMenus()

	buf := common.NewImageBuffer(m.width, m.height)

	if !m.inActiveGame || phase == common.PhaseNone || phase == common.PhaseSuspended {
		m.renderLobby(buf, menus)
	} else {
		m.renderPlaying(buf, menus, game, gameName, phase)
	}

	// Post-processing shaders: run in sequence on the fully-rendered buffer.
	engine.ApplyShaders(m.shaders, buf)

	// Overlay layers: render to sub-buffers, blit, then shadow via RecolorRect.
	shadowFg := m.theme.ShadowFgC()
	shadowBg := m.theme.ShadowBgC()
	if m.overlay.OpenMenu >= 0 {
		menuLayer := m.theme.LayerAt(1)
		if dd := m.overlay.RenderDropdown(menus, 0, menuLayer); dd.Content != "" {
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

	isLobby := !m.inActiveGame || phase == common.PhaseNone || phase == common.PhaseSuspended

	if isLobby && m.lobbyWindow.FocusIdx == 4 {
		if cx, cy, visible := m.lobbyWindow.CursorPosition(); visible {
			if cursor := m.lobbyInput.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	} else if !isLobby && m.playingWindow.FocusIdx == 4 {
		// Playing command input has focus — show cursor.
		if cx, cy, visible := m.playingWindow.CursorPosition(); visible {
			if cursor := m.playingInput.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	}
	if isLobby && m.teamEditing {
		if cursor := m.teamEditInput.Cursor(); cursor != nil {
			// Position cursor on the team name row in the right panel.
			// The team panel is at col 2 in the NCWindow grid. After grid layout,
			// its X position is computed. We calculate the Y row within the panel.
			teams := m.api.State().GetTeams()
			unassigned := m.api.State().UnassignedPlayers()
			idx := m.api.State().PlayerTeamIndex(m.playerID)
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
				cursor.Position.X += cx + 4              // +4 for " XX " (space + swatch + space before name)
			}
			view.Cursor = cursor
		}
	}
	return view
}

// renderLobby renders the lobby view using NC controls directly into the buffer.
// Layout: row 0 = NCMenuBar, rows 1..H-2 = NCWindow (chat + teams + cmd bar), row H-1 = NCStatusBar.
func (m Model) renderLobby(buf *common.ImageBuffer, menus []common.MenuDef) {
	// Update menu bar.
	m.lobbyMenuBar.Menus = menus

	// Update chat view.
	m.lobbyChatView.Lines = m.chatLines
	m.lobbyChatView.ScrollOffset = m.chatScrollOffset

	// Update team panel.
	teams := m.api.State().GetTeams()
	m.lobbyTeamPanel.Teams = teams
	m.lobbyTeamPanel.Unassigned = m.api.State().UnassignedPlayers()
	m.lobbyTeamPanel.MyTeamIdx = m.api.State().PlayerTeamIndex(m.playerID)
	m.lobbyTeamPanel.PlayerID = m.playerID
	m.lobbyTeamPanel.GetPlayer = m.api.State().GetPlayer
	m.lobbyTeamPanel.Editing = m.teamEditing
	if m.teamEditing {
		m.lobbyTeamPanel.EditValue = m.teamEditInput.Value()
	}
	m.lobbyTeamPanel.ShowCreate = !m.api.State().IsSoleMemberOfTeam(m.playerID)

	// Update status bar.
	modeLabel := "remote"
	if m.IsLocal {
		modeLabel = "local"
	}
	statusLeft := fmt.Sprintf(" null-space (%s) | %d players | uptime %s", modeLabel, m.api.State().PlayerCount(), m.api.Uptime())
	m.api.State().RLock()
	suspPhase := m.api.State().GamePhase
	suspName := m.api.State().GameName
	m.api.State().RUnlock()
	if suspPhase == common.PhaseSuspended && suspName != "" {
		statusLeft += fmt.Sprintf(" | suspended: %s", suspName)
	}
	m.lobbyStatusBar.LeftText = statusLeft
	m.lobbyStatusBar.RightText = time.Now().Format("2006-01-02 15:04:05") + " "

	// Render the full screen: menu bar + window + status bar.
	m.lobbyScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back from NCTextView (it may have been changed by scroll input).
	m.chatScrollOffset = m.lobbyChatView.ScrollOffset
}


func (m Model) renderPlaying(buf *common.ImageBuffer, menus []common.MenuDef, game common.Game, gameName string, phase common.GamePhase) {
	// Compute game viewport height (16:9 aspect ratio with min chat height).
	// Window interior = total - menuBar(1) - statusBar(1) - topBorder(1) - bottomBorder(1) = height - 4
	// Interior rows: gameView + divider(1) + chat + divider(1) + cmdInput(1) = gameH + chatH + 3
	interiorH := m.height - 4 // screen chrome (menu bar, status bar) + window borders
	gameH := m.width * 9 / 16
	chatH := interiorH - 3 - gameH // 3 = two dividers + command input
	minChatH := max(5, interiorH/3)
	if chatH < minChatH {
		chatH = minChatH
		gameH = interiorH - 3 - chatH
	}
	if gameH < 1 {
		gameH = 1
	}

	// Update gameview constraint for aspect-ratio sizing.
	m.playingWindow.Children[0].Constraint.MinH = gameH

	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	// Wire gameview rendering based on phase.
	switch phase {
	case common.PhaseSplash:
		m.playingGameView.RenderFn = func(gbuf *common.ImageBuffer, x, y, w, h int) {
			if !game.RenderSplash(gbuf, m.playerID, x, y, w, h) {
				m.defaultRenderSplash(gbuf, displayName, x, y, w, h)
			}
		}
		m.playingGameView.OnKey = nil // splash ignores game keys
	case common.PhaseGameOver:
		m.api.State().RLock()
		results := m.api.State().GameOverResults
		m.api.State().RUnlock()
		m.playingGameView.RenderFn = func(gbuf *common.ImageBuffer, x, y, w, h int) {
			if !game.RenderGameOver(gbuf, m.playerID, x, y, w, h, results) {
				m.defaultRenderGameOver(gbuf, results, x, y, w, h)
			}
		}
		m.playingGameView.OnKey = nil // game-over ignores game keys
	default: // PhasePlaying
		if ncTree := game.RenderNC(m.playerID, m.width, gameH); ncTree != nil {
			// NC-tree game: reconcile into a GameWindow and render/route through it.
			m.gameWindow = widget.ReconcileGameWindow(m.gameWindow, ncTree,
				func(gbuf *common.ImageBuffer, bx, by, bw, bh int) { game.Render(gbuf, m.playerID, bx, by, bw, bh) },
				func(action string) { game.OnInput(m.playerID, action) })
			m.playingGameView.RenderFn = func(gbuf *common.ImageBuffer, x, y, w, h int) {
				m.gameWindow.Window.RenderToBuf(gbuf, x, y, w, h, m.theme.LayerAt(0))
			}
			// Route keys to the reconciled window's focused control.
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		} else {
			m.gameWindow = nil
			m.playingGameView.RenderFn = func(gbuf *common.ImageBuffer, x, y, w, h int) {
				game.Render(gbuf, m.playerID, x, y, w, h)
			}
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		}
	}

	// Update chat view.
	m.playingChatView.Lines = m.chatLines
	m.playingChatView.ScrollOffset = m.chatScrollOffset

	// Update menu bar and status bar.
	m.playingMenuBar.Menus = menus
	switch phase {
	case common.PhaseSplash:
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		if isAdmin {
			m.playingStatusBar.LeftText = " [Enter] Start game"
		} else {
			m.playingStatusBar.LeftText = " Waiting for host to start..."
		}
	case common.PhaseGameOver:
		remaining := 15 - int(time.Since(m.gameOverStart).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		m.playingStatusBar.LeftText = fmt.Sprintf(" [Enter] Continue to lobby (%ds remaining)", remaining)
	default:
		m.playingStatusBar.LeftText = " " + game.StatusBar(m.playerID)
	}
	m.playingStatusBar.RightText = time.Now().Format("2006-01-02 15:04:05") + " "

	// Render the full screen.
	m.playingScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back.
	m.chatScrollOffset = m.playingChatView.ScrollOffset
}

// defaultRenderSplash renders a figlet game name centered in the viewport.
func (m Model) defaultRenderSplash(buf *common.ImageBuffer, name string, x, y, w, h int) {
	figletTitle := strings.TrimRight(engine.Figlet(name, ""), "\n")
	var lines []string
	if figletTitle != "" {
		lines = strings.Split(figletTitle, "\n")
		// Check if figlet fits; fall back to plain text if too wide.
		maxW := 0
		for _, l := range lines {
			if len(l) > maxW {
				maxW = len(l)
			}
		}
		if maxW > w {
			lines = []string{name}
		}
	} else {
		lines = []string{name}
	}

	// Center vertically and horizontally.
	topPad := (h - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i, line := range lines {
		row := y + topPad + i
		if row >= y+h {
			break
		}
		col := x + (w-len(line))/2
		if col < x {
			col = x
		}
		buf.WriteString(col, row, line, nil, nil, common.AttrNone)
	}
}

// defaultRenderGameOver renders a figlet "GAME OVER" title with ranked results.
func (m Model) defaultRenderGameOver(buf *common.ImageBuffer, results []common.GameResult, x, y, w, h int) {
	var lines []string

	// Figlet title.
	figletTitle := strings.TrimRight(engine.Figlet("GAME OVER", "slant"), "\n")
	figletLines := strings.Split(figletTitle, "\n")
	maxW := 0
	for _, l := range figletLines {
		if len(l) > maxW {
			maxW = len(l)
		}
	}
	if figletTitle != "" && maxW <= w {
		lines = append(lines, figletLines...)
	} else {
		lines = append(lines, "G A M E   O V E R")
	}
	lines = append(lines, "")

	// Results table.
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
			lines = append(lines, fmt.Sprintf("  %-3s %-*s  %s", pos, maxNameLen, r.Name, r.Result))
		}
	}

	// Center vertically.
	topPad := (h - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i, line := range lines {
		row := y + topPad + i
		if row >= y+h {
			break
		}
		col := x + (w-len(line))/2
		if col < x {
			col = x
		}
		buf.WriteString(col, row, line, nil, nil, common.AttrNone)
	}
}

func (m *Model) syncChat() {
	// Rebuild chat from state
	history := m.api.State().GetChatHistory()
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
			if p := m.api.State().GetPlayer(from); p != nil {
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
}

func (m *Model) resizeViewports() {
	phase := m.api.State().GetGamePhase()

	if !m.inActiveGame || phase == common.PhaseNone || phase == common.PhaseSuspended {
		// Lobby — chatH for scroll math.
		windowH := m.height - 2 // minus menu bar and status bar
		chatH := max(1, windowH-4) // approx: borders + divider + cmd bar
		m.chatH = chatH
	} else if phase == common.PhasePlaying {
		// Playing — Screen (menu bar + status bar = 2), window borders (2), dividers (2), cmd bar (1) = 7 overhead.
		interiorH := m.height - 4
		gameH := m.width * 9 / 16
		chatH := interiorH - 3 - gameH
		minChatH := max(5, interiorH/3)
		if chatH < minChatH {
			chatH = minChatH
		}
		m.chatH = chatH
	} else {
		m.chatH = 0
	}
}

// allMenus returns the full ordered list of menus for the NC action bar:
// the framework "File" menu followed by any game-registered menus.
// invalidateMenuCache forces the next cachedMenus() call to rebuild.
func (m *Model) invalidateMenuCache() {
	m.menuCache = nil
}

// cachedMenus returns the menu tree, rebuilding only when the active game has changed.
func (m *Model) cachedMenus() []common.MenuDef {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	m.api.State().RUnlock()

	if m.menuCache != nil && m.menuCacheGame == game {
		return m.menuCache
	}

	fileItems := []common.MenuItemDef{
		{Label: "&Resume Game...", Handler: func(_ string) { m.showResumeGameDialog() }},
		{Label: "---"},
		{Label: "&Themes...", Handler: func(_ string) { m.showPlayerListDialog("Themes", "themes", ".json") }},
		{Label: "&Plugins...", Handler: func(_ string) { m.showPlayerListDialog("Plugins", "plugins", ".js") }},
		{Label: "&Shaders...", Handler: func(_ string) { m.showShaderDialog() }},
		{Label: "---"},
	}
	if m.IsLocal {
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
				go m.api.KickPlayer(playerID)
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
					Body:    engine.AboutLogo(),
					Buttons: []string{"OK"},
				})
			}},
		},
	})
	m.menuCache = menus
	m.menuCacheGame = game
	return menus
}

func (m *Model) showResumeGameDialog() {
	saves := m.api.ListSuspends()
	if len(saves) == 0 {
		m.overlay.PushDialog(common.DialogRequest{
			Title:   "Resume Game",
			Body:    "No suspended games found.",
			Buttons: []string{"OK"},
		})
		return
	}

	teamCount := m.api.State().TeamCount()

	var lines []string
	var buttons []string
	for i, s := range saves {
		if i >= 9 {
			break // limit to 9 saves in the dialog
		}
		teamNote := ""
		if s.TeamCount != teamCount {
			teamNote = fmt.Sprintf("  (lobby has %d teams)", teamCount)
		}
		lines = append(lines, fmt.Sprintf("  %d. %s/%s  (%d teams, %s)%s",
			i+1, s.GameName, s.SaveName, s.TeamCount, s.SavedAt.Format("Jan 2 15:04"), teamNote))
		buttons = append(buttons, fmt.Sprintf("%d", i+1))
	}
	buttons = append(buttons, "Cancel")

	body := strings.Join(lines, "\n")

	// Capture saves slice for the OnClose callback.
	capturedSaves := saves
	m.overlay.PushDialog(common.DialogRequest{
		Title:   "Resume Game",
		Body:    body,
		Buttons: buttons,
		OnClose: func(button string) {
			if button == "Cancel" || button == "" {
				return
			}
			idx := 0
			fmt.Sscanf(button, "%d", &idx)
			if idx < 1 || idx > len(capturedSaves) {
				return
			}
			s := capturedSaves[idx-1]
			if err := m.api.ResumeGame(s.GameName, s.SaveName); err != nil {
				m.overlay.PushDialog(common.DialogRequest{
					Title:   "Resume Failed",
					Body:    err.Error(),
					Buttons: []string{"OK"},
				})
			}
		},
	})
}

func (m *Model) showPlayerListDialog(title, subdir, ext string) {
	dir := filepath.Join(m.api.DataDir(), subdir)
	items := engine.ListDir(dir, ext)
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

func (m *Model) showShaderDialog() {
	available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
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

// dispatchInput processes submitted text (commands, chat, plugins).
// Called by both the lobby and playing NCCommandInput controls.
func (m *Model) dispatchInput(text string) {
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
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		ctx := common.CommandContext{
			PlayerID: m.playerID,
			IsAdmin:  isAdmin,
			Reply: func(s string) {
				msg := common.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: s}
				m.api.SendToPlayer(m.playerID, common.ChatMsg{Msg: msg})
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(common.Message{Text: s})
			},
			ServerLog: func(s string) {
				m.api.ServerLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	// Regular chat
	playerName := "unknown"
	if p := m.api.State().GetPlayer(m.playerID); p != nil {
		playerName = p.Name
	}
	m.api.BroadcastChat(common.Message{Author: playerName, Text: text})
}

// lobbyTabComplete provides tab completion for the lobby command input.
func (m *Model) lobbyTabComplete(current string) (string, bool) {
	if !strings.HasPrefix(current, "/") {
		return "", false
	}
	if m.tabCandidates == nil {
		m.tabPrefix, m.tabCandidates = m.api.TabCandidates(current, m.api.State().PlayerNames())
		m.tabIndex = 0
	}
	if len(m.tabCandidates) == 0 {
		return "", false
	}
	result := m.tabPrefix + m.tabCandidates[m.tabIndex]
	m.tabIndex = (m.tabIndex + 1) % len(m.tabCandidates)
	return result, true
}

func (m *Model) handleThemeCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := theme.ListThemes(m.api.DataDir())
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
	path := filepath.Join(m.api.DataDir(), "themes", name+".json")
	t, err := theme.Load(path)
	if err != nil {
		m.pluginReply(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.gameWindow = nil // force rebuild with new theme
	m.pluginReply(fmt.Sprintf("Theme changed to: %s", t.Name))
}

func (m *Model) pluginReply(text string) {
	msg := common.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: text}
	m.api.SendToPlayer(m.playerID, common.ChatMsg{Msg: msg})
}

func (m *Model) handlePluginCommand(input string) {
	parts := strings.Fields(input)
	// /plugin with no args -> list
	if len(parts) <= 1 {
		available := engine.ListDir(filepath.Join(m.api.DataDir(), "plugins"), ".js")
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
		name, path, err := engine.ResolvePluginPath(nameOrURL, m.api.DataDir())
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
		pl, err := engine.LoadPlugin(path, m.api.Clock())
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
func (m *Model) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		ctx := common.CommandContext{
			PlayerID: m.playerID,
			IsAdmin:  isAdmin,
			Reply: func(s string) {
				msg := common.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: s}
				m.api.SendToPlayer(m.playerID, common.ChatMsg{Msg: msg})
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(common.Message{Text: s})
			},
			ServerLog: func(s string) {
				m.api.ServerLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	playerName := "unknown"
	if p := m.api.State().GetPlayer(m.playerID); p != nil {
		playerName = p.Name
	}
	m.api.BroadcastChat(common.Message{Author: playerName, Text: text})
}

func (m *Model) handleShaderCommand(input string) {
	parts := strings.Fields(input)
	// /shader with no args -> list
	if len(parts) <= 1 {
		available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
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
		name, path, err := engine.ResolveShaderPath(nameOrURL, m.api.DataDir())
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
		sh, err := engine.LoadShader(path, m.api.Clock())
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

func (m *Model) moveShader(name string, delta int) {
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

func truncateStyled(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "")
}
