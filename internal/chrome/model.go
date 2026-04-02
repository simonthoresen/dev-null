package chrome

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/internal/domain"
	"null-space/internal/engine"
	"null-space/internal/state"
	"null-space/internal/theme"
	"null-space/internal/widget"
)

// ServerAPI is the interface that the chrome model uses to interact with the server.
type ServerAPI interface {
	State() *state.CentralState
	Clock() domain.Clock
	DataDir() string
	Uptime() string

	// Communication
	BroadcastChat(msg domain.Message)
	BroadcastMsg(msg tea.Msg)
	SendToPlayer(playerID string, msg tea.Msg)
	ServerLog(text string)

	// Commands
	TabCandidates(input string, playerNames []string) (prefix string, candidates []string)
	DispatchCommand(input string, ctx domain.CommandContext)

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
	shaders     []domain.Shader
	shaderNames []string // parallel to shaders; display names

	// Enhanced client protocol (null-space-client with charmap support).
	IsEnhancedClient bool
	charmapSent      bool // true after charmap+atlas OSC have been sent for the current game

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
	menuCache     []domain.MenuDef
	menuCacheGame domain.Game // game pointer when cache was built (nil = no game)

	// Game NC window — built from WidgetNode tree via reconciler.
	// Preserves interactive control state (focus, cursor, scroll) across frames.
	gameWindow *widget.GameWindow

	// Viewport bounds from the last renderPlaying call (for enhanced client OSC).
	viewportX, viewportY, viewportW, viewportH int
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
		m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
	}
	lobbyTeamPanel.OnCreateTeam = func() {
		m.api.State().MovePlayerToTeam(m.playerID, m.api.State().TeamCount())
		m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
	}
	lobbyTeamPanel.OnCycleColor = func(direction int) {
		idx := m.api.State().PlayerTeamIndex(m.playerID)
		m.api.State().NextTeamColor(idx, direction)
		m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
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

	case domain.TickMsg:
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
		engine.UpdateShaders(m.shaders, 0.1)
		return m, nil

	case domain.ChatMsg:
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
		if chatMsg.FromID != m.playerID && !chatMsg.IsReply && !chatMsg.IsFromPlugin {
			isSystem := chatMsg.Author == ""
			for _, pl := range m.plugins {
				if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
					m.dispatchPluginReply(reply)
				}
			}
		}
		return m, nil

	case domain.PlayerJoinedMsg, domain.PlayerLeftMsg:
		m.syncChat()
		return m, nil

	case domain.TeamUpdatedMsg:
		// ClearScreen forces a full redraw. The ultraviolet renderer's
		// partial-update optimizer uses CR+LF cursor movement that
		// mispositions team panel content over SSH.
		return m, tea.ClearScreen

	case domain.GameLoadedMsg:
		// This player was connected when the game loaded — they're in the game.
		m.inActiveGame = true
		m.charmapSent = false
		m.invalidateMenuCache()
		m.lobbyInput.Model.Blur()
		// Focus the playing gameview.
		m.playingWindow.FocusIdx = 0
		m.resizeViewports()
		return m, nil

	case domain.GameUnloadedMsg:
		m.inActiveGame = false
		m.invalidateMenuCache()
		m.lobbyWindow.FocusIdx = 4 // lobbyInput
		cmd := m.lobbyInput.Model.Focus()
		m.playingInput.Model.Blur()
		m.resizeViewports()
		return m, cmd

	case domain.GamePhaseMsg:
		if msg.Phase == domain.PhaseGameOver {
			m.gameOverStart = time.Now()
		}
		if msg.Phase == domain.PhaseNone {
			m.inActiveGame = false
			m.lobbyWindow.FocusIdx = 4
			cmd := m.lobbyInput.Model.Focus()
			m.resizeViewports()
			return m, cmd
		}
		if msg.Phase == domain.PhaseSuspended {
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

	if !m.inActiveGame || phase == domain.PhaseNone || phase == domain.PhaseSuspended {
		// Lobby — chatH for scroll math.
		windowH := m.height - 2 // minus menu bar and status bar
		chatH := max(1, windowH-4) // approx: borders + divider + cmd bar
		m.chatH = chatH
	} else if phase == domain.PhasePlaying {
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
