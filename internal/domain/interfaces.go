package domain

import (
	"image"

	"dev-null/internal/render"
)

// Command is a registered slash command.
type Command struct {
	Name             string
	Description      string
	AdminOnly        bool
	FirstArgIsPlayer bool // shorthand: complete first arg against player names
	// Complete returns all valid candidates for the next arg given what was
	// already typed. TabComplete calls this, filters by partial, and cycles.
	// If nil and FirstArgIsPlayer is false, no tab completion is offered.
	Complete func(before []string) []string
	Handler  func(ctx CommandContext, args []string)
}

// CommandContext is passed to command handlers.
type CommandContext struct {
	PlayerID  string // empty string when invoked from the server console
	IsConsole bool   // true when invoked from the server console (not a player)
	IsAdmin   bool
	Reply     func(string) // send message to caller only (private)
	Broadcast func(string) // send system message to all chat
	ServerLog func(string) // append to server log panel only (never sent to players)
	Clipboard func(string) // copy text to system clipboard (nil = no clipboard support)
}

// MenuItemDef describes one item in a game-registered drop-down menu.
// A Label consisting entirely of "-" characters renders as a separator line.
type MenuItemDef struct {
	Label    string
	Hotkey   string                // e.g. "ctrl+c" — displayed right-aligned, globally bound
	Disabled bool
	Toggle   bool                  // if true, this is a toggle item with a checkmark column
	Checked  func() bool           // returns current toggle state (nil = not a toggle)
	Handler  func(playerID string) // nil for separators
}

// MenuDef describes a top-level menu registered by a game in the NC action bar.
type MenuDef struct {
	Label string
	Items []MenuItemDef
}

// DialogCopyItem is a labeled text entry with a [Copy] button in a dialog.
type DialogCopyItem struct {
	Label string // short label shown on the left, e.g. "Windows"
	Value string // full text to copy (may be display-truncated in the dialog)
}

// DialogRequest asks the framework to show a modal dialog to a specific player.
type DialogRequest struct {
	Title   string
	Body    string   // may be multi-line (\n-separated)
	Buttons []string // button labels; if empty, defaults to ["OK"]
	Warning bool     // render with the warning theme layer (red) instead of the normal dialog layer
	// OnClose is called with the pressed button label, or "" if dismissed with Esc.
	OnClose func(button string)

	// CopyItems shows labeled text entries each with a [Copy] button.
	// When set, replaces the Body text content.
	CopyItems []DialogCopyItem
	// OnCopy is called when a [Copy] button is pressed, with the item's full Value.
	OnCopy func(value string)

	// List support — when ListItems is non-nil, the dialog renders a selectable,
	// scrollable list instead of the Body text. The list cursor is tracked by
	// OverlayState and passed to OnListAction when a button is pressed.
	ListItems    []string
	ListTags     []string                     // optional right-aligned tags per item (e.g. "[active]")
	OnListAction func(button string, idx int) // called instead of OnClose when ListItems is set
	// OnListEnter is called when Enter is pressed on the focused list item,
	// without closing the dialog. Use PopDialog+PushDialog inside to refresh.
	OnListEnter func(idx int)

	// RequireListNavigation is a list of button labels that are disabled until
	// the user has navigated the list (pressed an arrow key or clicked an item).
	// This prevents accidental actions on the default cursor position.
	RequireListNavigation []string

	// Input support — when InputPrompt is non-empty, the dialog shows a single-line
	// text input field above the buttons. OnInputClose receives the button label
	// and the current input value.
	InputPrompt  string
	OnInputClose func(button string, value string) // called instead of OnClose when InputPrompt is set
}

// Shader is a post-processing pass that runs on the rendered ImageBuffer
// before it is serialized to the final ANSI string. Shaders can be implemented
// in Go (compiled into the binary) or in JavaScript (loaded at runtime from
// dist/shaders/). Multiple shaders run in sequence per player.
type Shader interface {
	Name() string
	// Process applies the shader effect to the buffer. elapsed is the total
	// seconds since the server started — shaders derive all time-based effects
	// from this value (no mutable state). This makes shaders pure functions of
	// (buffer × time), replicable on the client for local rendering.
	Process(buf *render.ImageBuffer, elapsed float64)
	Unload()
}

// Game is the interface every loaded game must satisfy.
// One game is active at a time and owns the viewport, status bar, and command bar.
// All methods are implemented by the JS runtime; optional JS hooks return zero values
// when not defined by the game script.
type Game interface {
	// --- Lifecycle ---

	// Load is called before PhaseStarting with persisted state (nil for new game).
	Load(savedState any)
	// Begin is called at the PhaseStarting→PhasePlaying transition.
	Begin()
	// Update is called once per tick with seconds since last update.
	Update(dt float64)
	// End is called when the game signals game-over, before PhaseEnding.
	End()
	// Unload tears down the game runtime and returns persistent state to save
	// across sessions (high scores, unlocks, etc.). Return nil for no state.
	// Called on game-over, /game unload, AND after Suspend() during /game suspend.
	Unload() any
	// Suspend returns the mid-session snapshot to store in a suspend save.
	// Called before Unload() during /game suspend; does NOT interrupt the VM.
	// Return nil if the game has no session state worth saving.
	Suspend() any
	// Resume is called instead of Begin() when restoring from a suspend save.
	// sessionState is the value previously returned by Suspend().
	// Falls back to Begin() if the game does not implement this hook.
	Resume(sessionState any)

	// --- Events ---

	OnPlayerJoin(playerID, playerName string)
	OnPlayerLeave(playerID string)
	OnInput(playerID, key string)

	// --- Rendering ---

	RenderAscii(buf *render.ImageBuffer, playerID string, x, y, width, height int)
	// RenderStarting renders a custom starting screen into buf. Returns false to
	// use the framework's default figlet-based starting screen.
	RenderStarting(buf *render.ImageBuffer, playerID string, x, y, width, height int) bool
	// RenderEnding renders a custom ending screen into buf. Returns false
	// to use the framework's default ending screen with figlet title + results.
	RenderEnding(buf *render.ImageBuffer, playerID string, x, y, width, height int, results []GameResult) bool
	// Layout returns a declarative widget tree describing the game window.
	// If it returns nil, the framework falls back to wrapping Render() in a
	// default gameview node. Games can mix NC panels with raw Render() output
	// by including {type: "gameview"} nodes in their tree.
	Layout(playerID string, width, height int) *WidgetNode
	// RenderCanvas calls the game's renderCanvas(ctx, w, h) hook if defined.
	// Returns the rendered image as PNG bytes, or nil if the game has no canvas hook.
	// The canvas dimensions match the client's actual pixel viewport.
	RenderCanvas(playerID string, width, height int) []byte
	// RenderCanvasImage is like RenderCanvas but returns the raw *image.RGBA
	// instead of PNG bytes. Used by quadrant rendering to avoid encode/decode overhead.
	RenderCanvasImage(playerID string, width, height int) *image.RGBA
	HasCanvasMode() bool // true if game defines renderCanvas hook

	// --- Properties ---

	GameName() string     // display name (fallback: filename stem)
	TeamRange() TeamRange // supported team count range (zero = no constraint)
	StatusBar(playerID string) string  // game-controlled status bar (second row, below menu bar)
	CommandBar(playerID string) string // game-controlled command bar (above framework status bar)
	Commands() []Command
	Menus() []MenuDef

	// --- Source delivery ---

	// GameSource returns all JS files (main + includes) for client-side replication.
	GameSource() []GameSourceFile

	// GameAssets returns binary asset files (audio, images) bundled with the game folder.
	// Returns nil for single-file games. Called once per game load to send assets to
	// graphical clients during the starting phase.
	GameAssets() []GameAsset
}

// GameSourceFile is a JS file loaded by the game (main or included).
type GameSourceFile struct {
	Name    string // filename (e.g. "main.js")
	Content string // full JS source
}
