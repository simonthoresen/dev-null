package common

// Command is a registered slash command.
type Command struct {
	Name             string
	Description      string
	AdminOnly        bool
	FirstArgIsPlayer bool // shorthand: complete first arg against player names
	// Complete returns all valid candidates for the next arg given what was
	// already typed. TabComplete calls this, filters by partial, and cycles.
	// If nil and FirstArgIsPlayer is false, no tab completion is offered.
	Complete         func(before []string) []string
	Handler          func(ctx CommandContext, args []string)
}

// CommandContext is passed to command handlers.
type CommandContext struct {
	PlayerID  string // empty string when invoked from the server console
	IsConsole bool   // true when invoked from the server console (not a player)
	IsAdmin   bool
	Reply     func(string) // send message to caller only (private)
	Broadcast func(string) // send system message to all chat
	ServerLog func(string) // append to server log panel only (never sent to players)
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

// DialogRequest asks the framework to show a modal dialog to a specific player.
type DialogRequest struct {
	Title   string
	Body    string   // may be multi-line (\n-separated)
	Buttons []string // button labels; if empty, defaults to ["OK"]
	// OnClose is called with the pressed button label, or "" if dismissed with Esc.
	OnClose func(button string)
}

// Shader is a post-processing pass that runs on the rendered ImageBuffer
// before it is serialized to the final ANSI string. Shaders can be implemented
// in Go (compiled into the binary) or in JavaScript (loaded at runtime from
// dist/shaders/). Multiple shaders run in sequence per player.
type Shader interface {
	Name() string
	Update(dt float64)
	Process(buf *ImageBuffer)
	Unload()
}

// Game is the interface every loaded game must satisfy.
// One game is active at a time and owns the viewport, status bar, and command bar.
// All methods are implemented by jsRuntime; optional JS hooks return zero values
// when not defined by the game script.
type Game interface {
	GameName() string   // display name (fallback: filename stem)
	TeamRange() TeamRange // supported team count range (zero = no constraint)
	Init(savedState any)  // called before splash with persisted state (or nil)
	Start()               // called at splash→playing transition
	Update(dt float64)    // called once per tick with seconds since last update
	OnPlayerLeave(playerID string)
	OnInput(playerID, key string)
	Render(buf *ImageBuffer, playerID string, x, y, width, height int)
	// RenderSplash renders a custom splash screen into buf. Returns false to
	// use the framework's default figlet-based splash screen.
	RenderSplash(buf *ImageBuffer, playerID string, x, y, width, height int) bool
	// RenderGameOver renders a custom game-over screen into buf. Returns false
	// to use the framework's default game-over screen with figlet title + results.
	RenderGameOver(buf *ImageBuffer, playerID string, x, y, width, height int, results []GameResult) bool
	// RenderNC returns a declarative widget tree for the game viewport.
	// If it returns nil, the framework falls back to wrapping Render() in a
	// default gameview node. Games can mix NC panels with raw Render() output
	// by including {type: "gameview"} nodes in their tree.
	RenderNC(playerID string, width, height int) *WidgetNode
	StatusBar(playerID string) string  // game-controlled status bar (second row, below menu bar)
	CommandBar(playerID string) string // game-controlled command bar (above framework status bar)
	Commands() []Command
	Menus() []MenuDef
	CharMap() *CharMapDef // returns nil if the game doesn't use a charmap
	Unload()

	// Suspend/resume support — optional for JS games.
	CanSuspend() bool        // true if the game supports suspend/resume
	Suspend() any            // called on suspend; returns session state to persist
	Resume(sessionState any) // called on resume; nil sessionState = warm resume (runtime still alive)
}

