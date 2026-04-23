package chrome

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
	SuspendGame(saveName string) error
	ResumeGame(gameName, saveName string) error
	ListSuspends() []state.SuspendInfo

	// Session management
	KickPlayer(playerID string) error

	// Invite
	InviteLinks() (win, mac string)
}

// lobbyTeamPanelW is the fixed width of the team panel in the lobby.
const lobbyTeamPanelW = 32

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

	// Graphics preference (Ascii/Blocks/Pixels) and location preference (local/remote).
	// These are the user's intent; effective values are derived from these + capabilities.
	graphicsPref    domain.GraphicsMode // preferred display mode
	renderLocalPref bool               // preferred location: true = client-side, false = server-side

	// Effective settings derived from preferences + capabilities; updated on game load
	// and when the user changes preferences.
	graphicsMode domain.GraphicsMode // effective display mode
	renderLocal  bool               // effective location

	// Chat size in interior rows when a game is open. Clamped to [5, 10].
	chatSize int

	// Enhanced client protocol (dev-null-client with canvas/local-render support).
	IsEnhancedClient bool
	SessionWriter    io.Writer // direct session writer for OSC passthrough (bypasses renderer)
	oscModeSent      bool     // true after the initial mode OSC has been sent
	gameSrcSent      bool     // true after game source files have been sent
	assetsSent       bool     // true after game assets (audio/images) have been sent
	lastStateHash    uint64   // FNV-64a hash of last sent Game.state JSON (for delta detection)
	pendingSoundOSC  []string // sound/stop-sound OSC strings to inject into next View()
	pendingMidiOSC   []string // MIDI event OSC strings to inject into next View()
	synthName        string   // active SoundFont name (e.g. "chiptune", "gm"); default = "chiptune"
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

	// Ready button for PhaseStarting — standalone focus target, rendered
	// directly by renderStartingScreen at a position in the game viewport.
	// Not part of any Window's focus hierarchy.
	phaseReadyButton *widget.Button

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
	// Lobby NC controls.
	lobbyChatView := &widget.TextView{
		BottomAlign: true,
		Scrollable:  true,
		NoFocus:     true, // chrome catches PgUp/PgDn; focus here would be a dead stop
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
	playingChatView := &widget.TextView{BottomAlign: true, Scrollable: true, NoFocus: true}
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

	// Standalone Ready button for PhaseStarting. Its Render method is
	// called by renderStartingScreen at a computed viewport position; it
	// is not part of any Window's focus hierarchy.
	phaseReadyButton := &widget.Button{Label: "Ready", Align: "left"}

	m := Model{
		api:           api,
		playerID:      playerID,
		theme:         theme.Default(),
		ColorProfile:  colorprofile.TrueColor,
		graphicsPref: domain.ModeBlocks, // default: prefer Blocks (canvas as Unicode blocks)
		chatSize:     5,                        // default; overridden by client.txt
		synthName:     "chiptune",              // default SoundFont; overridden by client.txt
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
		phaseReadyButton: phaseReadyButton,
	}
	lobbyMenuBar.Overlay = &m.overlay
	playingMenuBar.Overlay = &m.overlay

	// Wire lobby command input callbacks.
	lobbyInputCtrl.OnSubmit = m.dispatchInput
	lobbyInputCtrl.OnTab = m.lobbyTabComplete

	// Wire playing command input callbacks.
	playingInputCtrl.OnSubmit = func(text string) {
		m.dispatchInput(text)
		// Return focus to gameview after submitting so the next Enter
		// re-triggers the framework's focus-chat action.
		m.playingWindow.FocusIdx = 0
		m.playingInput.Model.Blur()
	}
	playingInputCtrl.OnTab = m.lobbyTabComplete // same tab completion logic

	// Wire the Ready button for the starting phase. There is no
	// equivalent for ending — the server posts results to chat and
	// unloads directly without a player-ack step.
	phaseReadyButton.OnPress = func() { m.api.ReadyUp(m.playerID) }
	phaseReadyButton.Disabled = func() bool {
		st := m.api.State()
		st.RLock()
		defer st.RUnlock()
		return st.StartingReady != nil && st.StartingReady[m.playerID]
	}

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
	lobbyTeamPanel.OnStartRename = m.showTeamRenameDialog
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

// applyGraphicsPrefs computes the effective graphicsMode and renderLocal from
// the user's preferences and current capabilities. Handles degradation:
//   - Pixels requires enhanced client + canvas; degrades to Blocks
//   - Blocks requires canvas; degrades to Ascii
//   - Local requires enhanced client; forced false for SSH
//   - Pixels is always local
func (m *Model) applyGraphicsPrefs() {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	m.api.State().RUnlock()
	hasCanvas := game != nil && game.HasCanvasMode()

	mode := m.graphicsPref
	local := m.renderLocalPref

	// Pixels requires enhanced client + canvas.
	if mode == domain.ModePixels && !(m.IsEnhancedClient && hasCanvas) {
		mode = domain.ModeBlocks
	}
	// Blocks requires canvas.
	if mode == domain.ModeBlocks && !hasCanvas {
		mode = domain.ModeAscii
	}
	// Pixels is always local.
	if mode == domain.ModePixels {
		local = true
	}
	// Local requires enhanced client.
	if local && !m.IsEnhancedClient {
		local = false
	}

	wasLocal := m.renderLocal
	m.graphicsMode = mode
	m.renderLocal = local

	// Reset OSC state when switching between local and remote.
	if wasLocal != local {
		m.oscModeSent = false
		m.gameSrcSent = false
		m.lastStateHash = 0
	}
}

// SetDefaultRenderLocal sets the default render location preference.
// Called during session setup before InitCommands, so an explicit
// /render-local or /render-remote in client.txt will override this.
func (m *Model) SetDefaultRenderLocal(local bool) {
	m.renderLocalPref = local
}

// setGraphicsPref sets the display mode preference and recomputes effective settings.
func (m *Model) setGraphicsPref(pref domain.GraphicsMode) {
	m.graphicsPref = pref
	m.applyGraphicsPrefs()
}

// setRenderLocal sets the render location preference and recomputes effective settings.
func (m *Model) setRenderLocal(local bool) {
	m.renderLocalPref = local
	m.applyGraphicsPrefs()
}
