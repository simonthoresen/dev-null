# Game API: Interfaces, JS Globals, Commands, Messages

## The `Game` Interface (`internal/domain/interfaces.go`)
```go
type Game interface {
    // --- Lifecycle ---
    Load(savedState any)    // called on game load with persisted state (nil = first run)
    Begin()                 // called at starting→playing transition
    Update(dt float64)      // called once per tick with seconds since last update
    End()                   // called when game signals game-over, before ending screen
    Unload() any            // tears down runtime; returns session state to persist (nil ok)

    // --- Events ---
    OnPlayerLeave(playerID string)
    OnInput(playerID, key string)

    // --- Rendering ---
    Render(buf *ImageBuffer, playerID string, x, y, width, height int) // write game viewport into buffer
    Layout(playerID string, width, height int) *WidgetNode             // declarative widget tree for game window (nil = use Render)
    RenderCanvas(playerID string, width, height int) []byte // PNG bytes, nil if no canvas hook
    HasCanvasMode() bool               // true if game defines renderCanvas

    // --- Properties ---
    GameName() string                      // display name (fallback: filename stem)
    TeamRange() TeamRange                  // {Min, Max} -- zero = no constraint
    StatusBar(playerID string) string      // feeds framework status bar (left-aligned)
    CommandBar(playerID string) string     // command bar (above framework status bar)
    Commands() []Command
    Menus() []MenuDef
    CharMap() *CharMapDef              // nil if game doesn't use a charmap

    // --- Source delivery ---
    GameSource() []GameSourceFile
    GameAssets() []GameAsset
}
```
`Runtime` implements `Game`. `load()` is mandatory; all other JS hooks are optional. `teams()` global returns game team snapshot during load/begin/playing.

**Lifecycle (fresh load):** `Load(persistentState)` → [PhaseStarting] → `Begin()` → [PhasePlaying] → `Update(dt)` → game calls `gameOver()` → `End()` → [PhaseEnding] → `Unload() any` → persistent state saved

**Lifecycle (suspend):** `Suspend() any` → session state saved to suspend file → `Unload() any` → persistent state saved → PhaseNone

**Lifecycle (resume):** `Load(persistentState)` → `Resume(sessionState)` → [PhasePlaying immediately, no Starting phase]

State separation: `Unload()` returns **persistent state** (high scores, unlocks) written to `dist/state/<game>.json` and loaded on every fresh load and resume. `Suspend()` returns **session state** (board, current score) written only to the suspend save file and passed to `Resume()`. There is no `State()`/`SetState()` on `domain.Game`; `ScriptRuntime.State()` exists only for OSC push to local renderers.

## Games (JS)

Games live in `dist/games/` as either single `.js` files or folders containing `main.js` (for multi-file games using `include()`). Loaded at runtime via `/game load <name>`. A HTTPS URL can be given instead of a name -- `.js` files are cached in `dist/games/.cache/`, `.zip` files are extracted to `dist/games/<name>/`. GitHub blob URLs are converted to raw automatically.

**Game** -- exports a global `Game` object with hooks `update`, `onPlayerLeave`, `onInput`, `renderAscii`, `renderCanvas`, `layout`, `statusBar`, `commandBar`, `end`, `unload`, `suspend`, `resume`. Optional properties: `gameName`, `teamRange`, `charmap`. Mandatory `load(persistentState)` called on every game load (fresh and resume). Loaded one at a time; owns the viewport. `update(dt)` is called once per tick with elapsed seconds -- all game logic belongs here. Games must use `dt` for all timing (accumulate elapsed time, count down timers by subtracting `dt`) -- never count ticks, as the tick interval is configurable. `renderAscii(buf, playerID, ox, oy, w, h)` receives an `ImageBuffer` and writes pixels directly via `buf.setChar(x, y, ch, fg, bg)`, `buf.writeString(x, y, text, fg, bg)`, `buf.fill(x, y, w, h, ch, fg, bg)`. Colors are `"#RRGGBB"` hex strings or `null`. Attribute constants: `ATTR_BOLD`, `ATTR_FAINT`, `ATTR_ITALIC`, `ATTR_UNDERLINE`, `ATTR_REVERSE`. The starting screen (figlet game name) and the ending screen (figlet "GAME OVER" + ranked results) are rendered by the framework -- games cannot override them. `unload()` is called on game-over, `/game unload`, AND after `suspend()` during `/game suspend` -- return **persistent state** (high scores, unlocks) to save across sessions; received via `load(persistentState)` on the next load. `suspend()` is called on `/game suspend` before `unload()` -- return **session state** (current board, score in progress) to store in the suspend save file; received via `resume(sessionState)` when the save is restored. `resume(sessionState)` is called instead of `begin()` when restoring from a suspend save; if not defined, falls back to `begin()`. `end()` is called just before PhaseEnding (optional cleanup hook). `layout` returns a declarative widget tree describing the game window using NC controls; if defined, `renderAscii()` is only called for `{type: "gameview"}` nodes within the tree. Interactive node types (`button`, `textinput`, `checkbox`) route actions back via `onInput(playerID, action)`. Tab cycles focus between interactive controls; Esc returns to raw `onInput` mode.

**Global functions available to JS:** `log()`, `chat()`, `chatPlayer()`, `teams()`, `now()`, `registerCommand()`, `gameOver(results)`, `figlet(text, font?)` (ASCII art via figlet4go; built-in fonts: `"standard"`, `"larry3d"`; extra fonts loaded from `dist/fonts/*.flf` at startup), `include(name)` (evaluate another `.js` file from the same directory -- for multi-file games). State to persist is returned from `unload()`, not passed to `gameOver()`.

**Full developer documentation:** see `API-REFERENCE.md` at the repo root.

## Commands (`internal/domain/interfaces.go`)
```go
type Command struct {
    Name             string
    Description      string
    AdminOnly        bool
    FirstArgIsPlayer bool                     // Tab-completes first arg against player names
    Complete         func(before []string) []string  // context-aware completion; overrides FirstArgIsPlayer
    Handler          func(ctx CommandContext, args []string)
}
```

`ctx.Reply(text)` sends a private response to the caller only. For SSH players it sends a `ChatMsg` with `IsPrivate: true`. For the console (playerID `""`) it appends directly to the console's chat panel -- **not** to the server log. Tab completion cycles through candidates alphabetically; repeated Tab advances through the list.

`GameName` in `CentralState` stores the bare name (e.g. `example`), not the full file path. `loadGame` strips the directory and `.js` extension. Commands that broadcast game load/unload events should use the bare name too -- `loadGame` already broadcasts `"Game loaded: <name>"` to chat, so command handlers must not send a redundant reply.

## `Message` Type (`internal/domain/types.go`)
```go
type Message struct {
    Author       string
    Text         string
    IsPrivate    bool
    ToID         string
    FromID       string
    IsReply      bool  // command response -- rendered as plain text, no "[system]" or "[PM]" prefix
    IsFromPlugin bool  // originated from a plugin -- plugins skip these to prevent loops
}
```

`IsReply: true` is set by `ctx.Reply()` so command output (e.g. `/help` listing) appears as plain text in the caller's chat window with no prefix. Without it, private messages show `[PM from X]`.
