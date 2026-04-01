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
	Disabled bool
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

// Game is the interface every loaded game must satisfy.
// One game is active at a time and owns the viewport, status bar, and command bar.
// All methods are implemented by jsRuntime; optional JS hooks return zero values
// when not defined by the game script.
type Game interface {
	GameName() string                      // display name (fallback: filename stem)
	TeamRange() TeamRange                  // supported team count range (zero = no constraint)
	SplashScreen() string                  // splash screen content (empty = use default)
	Init(savedState any)                   // called before splash with persisted state (or nil)
	Start()                                // called at splash→playing transition
	OnPlayerLeave(playerID string)
	OnInput(playerID, key string)
	View(playerID string, width, height int) string
	StatusBar(playerID string) string  // game-controlled status bar (second row, below menu bar)
	CommandBar(playerID string) string // game-controlled command bar (above framework status bar)
	Commands() []Command
	Menus() []MenuDef
	Unload()
}

// SkinColors defines optional color overrides for the framework chrome
// (menu bar, chat area, command bar, input box).
// Any field left empty ("") means "use the framework default for that slot".
// Colors are CSS hex strings (e.g. "#ff79c6") or standard terminal color names.
type SkinColors struct {
	MenuBg  string // menu bar background (top row, always present)
	MenuFg  string // menu bar foreground
	ChatBg  string // chat area background
	ChatFg  string // chat area foreground
	CmdBg   string // command bar background (idle hint mode)
	CmdFg   string // command bar foreground (idle hint mode)
	InputBg string // input box background (while typing)
	InputFg string // input box foreground (while typing)
}

// Plugin is a passive extension that runs alongside any game (or in the lobby).
// Multiple plugins can be active simultaneously and persist across game switches.
type Plugin interface {
	// OnChatMessage is called for every outgoing message before it's committed.
	// Return nil to drop the message, or return a (possibly modified) copy to allow it.
	OnChatMessage(msg *Message) *Message
	// OnPlayerJoin is called when a player connects.
	OnPlayerJoin(playerID, playerName string)
	// OnPlayerLeave is called when a player disconnects.
	OnPlayerLeave(playerID string)
	// Commands returns plugin-registered slash commands.
	Commands() []Command
	// Skin returns optional color overrides for the framework chrome.
	// Return nil if this plugin does not provide a skin.
	Skin() *SkinColors
	// Unload is called when the plugin is removed.
	Unload()
}
