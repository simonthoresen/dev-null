package common

// Command is a registered slash command.
type Command struct {
	Name             string
	Description      string
	AdminOnly        bool
	FirstArgIsPlayer bool // enables tab-completion against player list for first arg
	Handler          func(ctx CommandContext, args []string)
}

// CommandContext is passed to command handlers.
type CommandContext struct {
	PlayerID  string // empty = server console
	IsAdmin   bool
	Reply     func(string) // send message to caller only (private)
	Broadcast func(string) // send system message to all chat
	ServerLog func(string) // append to server log panel only (never sent to players)
}

// App is the interface every loaded app must satisfy.
// One app is active at a time and owns the viewport, status bar, and command bar.
type App interface {
	OnPlayerJoin(playerID, playerName string)
	OnPlayerLeave(playerID string)
	OnInput(playerID, key string)
	View(playerID string, width, height int) string
	StatusBar(playerID string) string  // content for top status bar row
	CommandBar(playerID string) string // idle hint shown in command bar
	Commands() []Command
	Unload()
}

// Plugin is a passive extension that runs alongside any app (or in the lobby).
// Multiple plugins can be active simultaneously and persist across app switches.
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
	// Unload is called when the plugin is removed.
	Unload()
}
