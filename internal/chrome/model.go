package chrome

import (
	"fmt"
	"image/color"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
	"dev-null/internal/state"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
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
	ReadyUp(playerID string)
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

	// Init commands from ~/.dev-null/client.txt (dispatched on first tick)
	InitCommands []string

	// Per-player theme
	theme     *theme.Theme
	themeName string // file stem used to load theme (empty = default)

	// Per-player plugins
	plugins     []engine.Plugin
	pluginNames []string // parallel to plugins; display names

	// Per-player shaders (post-processing, run in order)
	shaders     []domain.Shader
	shaderNames []string // parallel to shaders; display names

	// Per-player graphics preference (Ascii/Blocks/Pixels) — the user's intent.
	// The effective renderMode is derived from this preference + capabilities.
	graphicsPref domain.RenderMode

	// Per-player effective render mode (Ascii, Blocks, Pixels).
	// Derived from graphicsPref + current capabilities; updated on game load.
	renderMode domain.RenderMode

	// Enhanced client protocol (dev-null-client with canvas/local-render support).
	IsEnhancedClient bool
	SessionWriter    io.Writer // direct session writer for OSC passthrough (bypasses renderer)
	oscModeSent      bool     // true after the initial mode OSC has been sent
	gameSrcSent      bool     // true after game source files have been sent
	assetsSent       bool     // true after game assets (audio/images) have been sent
	lastStateHash    uint64   // FNV-64a hash of last sent Game.state JSON (for delta detection)
	pendingSoundOSC  []string // sound/stop-sound OSC strings to inject into next View()
	pendingMidiOSC   []string // MIDI event OSC strings to inject into next View()
	synthName        string   // active SoundFont name (e.g. "chiptune", "gm"); empty = default
	synthSent        bool     // true after synth selection OSC has been sent

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

	// Starting dialog NC controls (rendered as overlay in game viewport).
	startingWindow *widget.Window
	startingSplash *widget.GameView
	startingStatus *widget.Label

	// Cached menu tree — rebuilt only on invalidation.
	menuCache      []domain.MenuDef
	menuCacheGame  domain.Game // game pointer when cache was built (nil = no game)

	// Game NC window — built from WidgetNode tree via reconciler.
	// Preserves interactive control state (focus, cursor, scroll) across frames.
	gameWindow *widget.GameWindow

	// Viewport bounds from the last renderPlaying call (for enhanced client OSC).
	viewportX, viewportY, viewportW, viewportH int

	// Reusable render buffer — cleared and resized each frame instead of allocated.
	renderBuf *render.ImageBuffer

	// pendingClipboard is set by commands that want to copy text to the clipboard.
	// Consumed by View() (OSC 52) or PopClipboard() (GUI backend).
	pendingClipboard string

	// ColorProfile is the terminal's color depth, used when serialising the
	// render buffer. Defaults to TrueColor; set by the server from the SSH env.
	ColorProfile colorprofile.Profile
}

func NewModel(api ServerAPI, playerID string) *Model {
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
		FocusIdx: 4, // lobbyInput focused by default (index 4 in Children)
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

	// Starting dialog — rendered as a centered overlay in the game viewport.
	startingSplash := &widget.GameView{}
	startingStatus := &widget.Label{Align: "center"}
	startingWindow := &widget.Window{
		Children: []widget.GridChild{
			{Control: startingSplash, Constraint: widget.GridConstraint{
				Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 1, MinH: 1, Fill: widget.FillHorizontal,
			}},
			{Control: startingStatus, Constraint: widget.GridConstraint{
				Col: 0, Row: 2, WeightX: 1, Fill: widget.FillHorizontal,
			}},
		},
	}

	m := Model{
		api:           api,
		playerID:      playerID,
		teamEditInput: teamInput,
		theme:         theme.Default(),
		ColorProfile:  colorprofile.TrueColor,
		graphicsPref:  domain.RenderModeBlocks, // default: prefer Blocks (canvas as Unicode blocks)
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
		startingWindow:   startingWindow,
		startingSplash:   startingSplash,
		startingStatus:   startingStatus,
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
	return &m
}

func (m *Model) Init() tea.Cmd {
	return m.lobbyInput.Model.Focus() // starts cursor blink in lobby
}

// ViewBuffer returns the raw render buffer from the last View() call.
// Used by the GUI backend to skip ANSI serialization.
func (m *Model) ViewBuffer() *render.ImageBuffer {
	return m.renderBuf
}

// PopClipboard returns and clears any pending clipboard text.
func (m *Model) PopClipboard() string {
	s := m.pendingClipboard
	m.pendingClipboard = ""
	return s
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
	if len(lines) > domain.MaxChatDisplayLines {
		lines = lines[len(lines)-domain.MaxChatDisplayLines:]
	}
	m.chatLines = lines
}

func (m *Model) resizeViewports() {
	phase := m.api.State().GetGamePhase()

	if !m.inActiveGame || phase == domain.PhaseNone {
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

// canUseRenderMode returns true if the given mode is usable with the current
// client capabilities and active game.
func (m *Model) canUseRenderMode(mode domain.RenderMode) bool {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	m.api.State().RUnlock()

	switch mode {
	case domain.RenderModeAscii:
		return true
	case domain.RenderModeBlocks:
		return game != nil && game.HasCanvasMode()
	case domain.RenderModePixels:
		return game != nil && game.HasCanvasMode() && m.IsEnhancedClient
	}
	return false
}

// effectiveMode returns the best achievable render mode for the given preference
// and current client/game capabilities. Degrades gracefully:
// Pixels → Blocks → Ascii as capabilities require.
func (m *Model) effectiveMode(pref domain.RenderMode) domain.RenderMode {
	switch pref {
	case domain.RenderModePixels:
		if m.canUseRenderMode(domain.RenderModePixels) {
			return domain.RenderModePixels
		}
		fallthrough
	case domain.RenderModeBlocks:
		if m.canUseRenderMode(domain.RenderModeBlocks) {
			return domain.RenderModeBlocks
		}
		fallthrough
	default:
		return domain.RenderModeAscii
	}
}

// setGraphicsPref stores the user's graphics preference and applies the effective
// render mode derived from that preference + current capabilities.
// Handles OSC state resets when transitioning between Pixels and non-Pixels modes.
func (m *Model) setGraphicsPref(pref domain.RenderMode) {
	m.graphicsPref = pref
	wasLocal := m.renderMode == domain.RenderModePixels
	m.renderMode = m.effectiveMode(pref)
	isLocal := m.renderMode == domain.RenderModePixels
	// Reset OSC state when switching between local (Pixels) and non-local modes.
	if wasLocal != isLocal {
		m.oscModeSent = false
		m.gameSrcSent = false
		m.lastStateHash = 0
	}
}
