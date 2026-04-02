# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **For Claude:** This file is the portable memory for this project. Whenever you make a change, discover a gotcha, or establish a pattern or decision, **update this file before committing**. It is the single source of truth that survives new clones, new machines, and new sessions. Keep it accurate and concise — do not let it drift from the actual code.
>
> **For Claude:** When you have completed a task or a logical unit of work, **commit and push to git**. Don't wait to be asked.

## Project Goal

A framework for hosting terminal-based multiplayer **games** over SSH. **Only the server operator needs to install anything.** Players connect with a plain `ssh` command — no client install required.

Games are written in JavaScript (goja) and loaded at runtime from `dist/games/`. The server binary itself is game-agnostic.

## Release & Distribution

**Binaries are NOT checked into git.** They are built and published automatically by GitHub Actions on every push to `main`.

- **GitHub Actions** (`.github/workflows/release.yml`): builds `null-space-server.exe` + `null-space-client.exe` + `pinggy-helper.exe`, packages the full `dist/` folder into `null-space.zip`, and publishes a rolling `latest` release.
- **`install.ps1`** (repo root): one-liner installer for operators — downloads and extracts the latest release zip, creates desktop shortcuts. Usage: `irm https://github.com/simonthoresen/null-space/raw/main/install.ps1 | iex`
- **`start.ps1`** (in `dist/`): auto-updates on each launch — checks the GitHub release for a newer version and downloads the full zip (binaries, games, fonts) before starting.
- **Version tracking**: `dist/.version` stores the commit SHA of the installed release. Not tracked in git.

## Commands

```bash
make build          # compile to dist/null-space-{server,client}.exe + dist/pinggy-helper.exe
make run            # go run with --data-dir dist (dev shortcut)
make clean          # remove compiled binaries from dist/

go run ./cmd/null-space-server --data-dir dist   # equivalent to make run, add --password etc.
go test ./...

ssh -p 23234 localhost   # connect via plain SSH (host plays this way too)

# Graphical client — SSH + sprite rendering for charmap games.
go run ./cmd/null-space-client
go run ./cmd/null-space-client --host example.com --port 23234 --player alice

# Local mode — no SSH, runs full client TUI directly in the terminal.
# Useful as a render test-bed and as a local single-player mode.
go run ./cmd/null-space-server --local --data-dir dist
go run ./cmd/null-space-server --local --data-dir dist --game example
go run ./cmd/null-space-server --local --data-dir dist --game example --player alice
```

**Environment variables:**
- `NULL_SPACE_LOG_FILE` — path to log file (default: discard)
- `NULL_SPACE_LOG_LEVEL` — log level: debug/info/warn/error (default: info)
- `NULL_SPACE_PINGGY_STATUS_FILE` — path to Pinggy status file (enables tunnel bridge UI)

## Architecture

**null-space** is a "Multitenant Singleton" server over SSH.

### Core Pattern
- **One game singleton** runs on the server (`CentralState.ActiveGame`)
- **One Bubble Tea `Program` per SSH session**, all sharing the same game state
- **Central 100ms ticker** sends `TickMsg` to all programs simultaneously → synchronized real-time rendering
- **The server terminal is management-only.** The host joins as a player via SSH like everyone else.

### Game Lifecycle
```
LOBBY (teams + chat) → SPLASH → PLAYING → GAME OVER → LOBBY
                                   ↓
                               SUSPENDED → LOBBY (game still in memory)
                                   ↑
                                RESUME (warm or cold)
```
1. **Lobby**: Players configure teams, chat. Admin loads game with `/game load <name>`.
2. **Load**: Framework snapshots teams for the game (lobby teams stay independent), loads saved state, calls `init(savedState)`. `teams()` returns game teams.
3. **Splash**: Shows game splash screen (custom or default with game name). Admin presses Enter to start, or auto-starts after 10s.
4. **Splash→Playing**: Framework calls `start()`. Game sets up its playing state.
5. **Reconnect**: If a player disconnects mid-game and reconnects with the same name, they rejoin the game automatically.
5. **Playing**: Normal game mode. Game calls `gameOver(results, state)` when done.
4. **Game Over**: Framework renders ranked results screen. All players press Enter or 15s auto-transition.
5. Back to **Lobby** — game unloaded, teams preserved for next round.
6. **Suspend** (optional): Admin runs `/game suspend [saveName]`. Framework calls `Game.suspend()` to get session state, persists it to `dist/state/saves/<gameName>/<saveName>.json`, transitions to lobby. Runtime stays alive for warm resume.
7. **Resume**: Admin runs `/game resume <gameName/saveName>` or uses File → Resume Game menu. **Warm resume** (runtime alive): calls `Game.resume(nil)`, goes straight to Playing. **Cold resume** (server restarted): loads game fresh, calls `init(globalState)` + `start()` + `resume(sessionState)`, skips splash.

Late joiners see the lobby and can chat but don't join the active game. Lobby teams are independent from game teams — players can freely organize for the next round while a game is running.

### Suspend/Resume
Games opt in to suspend/resume by setting `canSuspend: true` on the `Game` object. Suspend saves are independent of global game state (high scores via `gameOver(results, state)`) — you can have multiple suspended sessions of the same game.

**JS hooks** (all optional, require `canSuspend: true`):
- `suspend()` — called on `/game suspend`. Returns session state to persist. Game should pause internal logic.
- `resume(sessionState)` — called on resume. `sessionState` is `null` for warm resume (runtime still alive), or the saved state for cold resume.

**Save files**: `dist/state/saves/<gameName>/<saveName>.json` — contains team snapshot, disconnected player map, and game session state. Deleted after successful resume.

**Commands**:
- `/game suspend [saveName]` — admin only. Auto-generates timestamp name if omitted.
- `/game resume <gameName/saveName>` — admin only. Tab-completes against saved sessions. No args lists available saves.
- File → Resume Game menu — shows saves in a dialog with team count validation.

### Teams
Players manage teams in the lobby panel (right side, fixed 32 chars). New players start **unassigned** (shown under "Unassigned" at the top of the team list). Tab switches focus between chat and team panel. Navigation in team panel:
- **Down** from unassigned → join first team (or create one if none exist)
- **Down** from a team → move to team below
- **Down** from last team → create new "Team \<your name\>" (blocked if you're the sole member, to avoid drop/recreate churn)
- **Up** from a team → move to team above
- **Up** from first team → become unassigned
- **Enter** (first player in team) → rename team
- **Left/Right** (first player in team) → cycle team color

New teams default to "Team \<creator name\>" and the first unused palette color. Games can declare `teamRange: {min, max}` to enforce valid team counts. Games access teams via the `teams()` global, which returns `[{name, color, players: [{id, name}, ...]}, ...]`. Game teams are a snapshot taken at load time — lobby teams remain editable during a game. Unassigned players are excluded from the game snapshot.

### Game State (`Game.state`)

All mutable game data must live on `Game.state`. The framework reads this property for:
- **Suspend/resume:** `Game.state` is serialized to JSON on suspend and restored via `SetState()` on cold resume. No special suspend/resume hooks needed — the framework handles it.
- **Client-side state replication:** (future) enhanced clients receive state deltas and render locally.

Games still persist cross-session data (high scores) via `gameOver(results, persistState)`. The `persistState` argument saves to `dist/state/<gamename>.json` and is received in `init(savedState)` on the next load. `Game.state` is session-scoped — it lives only during gameplay.

### UI Layout

All three views (console, lobby, playing) share a unified `Screen` layout:

```
Row 0: MenuBar      (fixed 1)   ← File Edit View Help — navigation only
Row 1: Window        (fill)      ← bordered NCWindow, content varies per view
Row 2: StatusBar    (fixed 1)   ← left text + right-aligned time
```

`Screen` (`internal/widget/screen.go`) renders the MenuBar at the secondary theme layer (depth 1), the Window at the primary layer (depth 0), and the StatusBar at the secondary layer. Focus management and cursor position are delegated to the content Window.

**Lobby:**
```
│ File  Edit  View  Help              │  MenuBar (row 0)
╔═══════════════════╤════════════════╗
║                   │ ██ Unassigned  ║  NCWindow (NoTopBorder) with grid:
║  [chat messages]  │   alice        ║    Row 0: NCTextView(chat) │ NCVDivider │ NCTeamPanel
║                   │ ██ Red Team    ║    Row 1: NCHDivider (connected)
║                   │    bob         ║    Row 2: NCCommandInput
║                   │ ██ Blue Team   ║
║                   │    charlie     ║  Chat: weight=1, Teams: fixed 32 cols
╟───────────────────┴────────────────╢  [Tab] cycles: input → chat → teams
║ [·····]                            ║  NCCommandInput: Enter=submit, Tab=cycle
╚════════════════════════════════════╝
│ null-space (local) | 3 players | ..│  StatusBar (row 2)
```

**Playing:**
```
│ File  Edit  View  Help              │  MenuBar (row 0)
╔════════════════════════════════════╗
║                                    ║  GameView (aspect-ratio: W×W*9/16)
║  Game viewport                     ║    Enter → focus command input
║                                    ║    all other keys → game.OnInput
╟────────────────────────────────────╢
║  [chat messages]                   ║  NCTextView (chat, fills remaining)
╟────────────────────────────────────╢
║ [·····]                            ║  NCCommandInput: submit/Esc → refocus GameView
╚════════════════════════════════════╝
│ HP: 100  Score: 42    15:04:05     │  StatusBar: game.StatusBar() left, time right
```

**Viewport sizing:** Ideal `gameH = W * 9 / 16`. Chat gets the remaining rows. `minChatH = max(5, interiorH/3)` — chat always gets at least ⅓ of interior rows. Interior = window height minus borders, dividers, and command input.

**Focus model:** NCWindow owns all focus management. Tab cycles between focusable controls. In the playing view, GameView has focus by default — Enter moves focus to the command input, submit/Esc returns it to GameView. For NC-tree games (layout), game controls participate in the Tab cycle alongside chat and command input.

**Chat scroll buffer:** 200 lines per player. `PgUp`/`PgDn` scroll the chat panel. Multi-line command replies (e.g. `/help`) are split into individual lines before storage.

**Command history:** 50 entries per NCCommandInput. `↑`/`↓` browse history. `↓` past the newest entry restores the draft. History is managed by the NCCommandInput widget.

### Key Packages

| Package | Role |
|---------|------|
| `internal/server/server.go` | SSH server setup, session lifecycle, tick broadcast |
| `internal/server/lifecycle.go` | Game load/unload, phase transitions, teams cache |
| `internal/server/commands.go` | Slash command registry and dispatch |
| `internal/server/local.go` | Local (non-SSH) single-player mode |
| `internal/server/pinggy.go` | Pinggy tunnel status polling bridge |
| `internal/chrome/model.go` | Per-player TUI model: struct, constructor, Init, Update |
| `internal/chrome/view.go` | Per-player rendering: lobby, playing, splash, game-over |
| `internal/chrome/input.go` | Key/mouse handling: lobby, game, team editing |
| `internal/chrome/commands.go` | Per-player command dispatch: /plugin, /theme, /shader |
| `internal/chrome/menus.go` | Menu tree construction and dialog helpers |
| `internal/console/console.go` | Server console TUI: model, view, log filtering |
| `internal/console/commands.go` | Console command dispatch: /plugin, /theme, /shader |
| `internal/console/sloghandler.go` | Console slog handler with render-path guard |
| `internal/domain/types.go` | Player, Message, GamePhase, Team, WidgetNode, tea messages |
| `internal/domain/interfaces.go` | Game, Command, Shader, MenuDef, DialogRequest interfaces |
| `internal/domain/clock.go` | Clock abstraction (RealClock, MockClock) |
| `internal/render/buffer.go` | ImageBuffer: 2D cell grid, ANSI parsing/serialization |
| `internal/render/charmap.go` | Charmap format: PUA codepoints, CharMapDef/Entry, loader |
| `internal/state/state.go` | `CentralState`: players, chat, game phase |
| `internal/state/teams.go` | Team management helpers |
| `internal/state/persist.go` | Game state JSON save/load |
| `internal/widget/` | NC widget toolkit: Window, Label, TextInput, Button, etc. |
| `internal/widget/screen.go` | Screen: unified 3-row chrome (MenuBar + Window + StatusBar) |
| `internal/widget/menubar.go` | MenuBar control: renders menu titles with shortcut highlighting |
| `internal/widget/statusbar.go` | StatusBar control: left/right text on a single row |
| `internal/widget/menu.go` | Overlay state, dropdown/dialog rendering, key handling |
| `internal/widget/reconcile.go` | Widget tree reconciler for game viewports |
| `internal/theme/theme.go` | Theme system: palettes, borders, depth layers |
| `internal/engine/runtime.go` | JS game runtime (goja): lifecycle, Game interface impl |
| `internal/engine/bindings.go` | JS global functions: log, chat, teams, gameOver, etc. |
| `internal/engine/shader.go` | Per-player JS shader post-processing |
| `internal/engine/plugin.go` | Per-player JS plugin system |
| `internal/engine/figlet.go` | Figlet ASCII art rendering |
| `internal/engine/gamelist.go` | Game discovery, path resolution, team range probing |
| `internal/network/` | UPnP, Pinggy status, public IP detection, downloads |
| `cmd/null-space-server/` | Server entry point: boot sequence, console setup, signal handling |
| `cmd/null-space-client/` | Graphical client: SSH + Ebitengine sprite rendering for charmap games |
| `cmd/pinggy-helper/` | Standalone helper that runs the Pinggy SSH tunnel |
| `internal/client/` | Client internals: SSH transport, ANSI parser, charmap atlas, Ebitengine renderer |
| `dist/charmaps/` | Charmap assets: per-game subdirectories with charmap.json + atlas PNG |
| `dist/start.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts pinggy-helper, then null-space-server.exe |
| `install.ps1` | One-liner installer: downloads latest release zip, extracts to a folder, creates desktop shortcuts |
| `.github/workflows/release.yml` | CI: builds binaries and publishes rolling `latest` release on every push to main |

### The `Game` Interface (`internal/domain/interfaces.go`)
```go
type Game interface {
    GameName() string                      // display name (fallback: filename stem)
    TeamRange() TeamRange                  // {Min, Max} — zero = no constraint
    Init(savedState any)                   // called before splash with persisted state
    Start()                                // called at splash→playing transition
    Update(dt float64)                     // called once per tick with seconds since last update
    OnPlayerLeave(playerID string)
    OnInput(playerID, key string)
    Render(buf *ImageBuffer, playerID string, x, y, width, height int) // write game viewport into buffer
    RenderSplash(buf *ImageBuffer, playerID string, x, y, w, h int) bool   // custom splash (false = use default figlet)
    RenderGameOver(buf *ImageBuffer, playerID string, x, y, w, h int, results []GameResult) bool // custom game-over
    Layout(playerID string, width, height int) *WidgetNode             // declarative widget tree for game window (nil = use Render)
    StatusBar(playerID string) string      // feeds framework status bar (left-aligned)
    CommandBar(playerID string) string     // command bar (above framework status bar)
    Commands() []Command
    Menus() []MenuDef
    CharMap() *CharMapDef              // nil if game doesn't use a charmap
    RenderCanvas(playerID string, width, height int) []byte // PNG bytes, nil if no canvas hook
    HasCanvasMode() bool               // true if game defines renderCanvas
    Unload()

    // Game.state — the framework reads/writes this for suspend/resume
    // and client-side state replication.
    State() any              // returns current Game.state object
    SetState(state any)      // replaces Game.state (cold resume)
}
```
`jsRuntime` implements `Game`. `init()` is mandatory; all other JS hooks are optional. `teams()` global returns game team snapshot during init/start/playing.

### Central Clock (`internal/domain/clock.go`)
The framework provides a central `Clock` interface (`Now() time.Time`) used for all time-related operations. Games access it via the `now()` JS global (epoch milliseconds). In tests, inject a `MockClock` to control time. `Update(dt)` receives the real elapsed seconds between ticks.

### Game Over

Games call `gameOver(results, state)` where `results` is an array of `{ name, result }` in ranked order and `state` is an optional object to persist for the next run. The framework renders the game-over screen — games don't need to provide their own. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). Both arguments are optional. State is received via `config.savedState` in `init()` on the next load.

### Commands (`internal/domain/interfaces.go`)
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

`ctx.Reply(text)` sends a private response to the caller only. For SSH players it sends a `ChatMsg` with `IsPrivate: true`. For the console (playerID `""`) it appends directly to the console's chat panel — **not** to the server log. Tab completion cycles through candidates alphabetically; repeated Tab advances through the list.

`GameName` in `CentralState` stores the bare name (e.g. `example`), not the full file path. `loadGame` strips the directory and `.js` extension. Commands that broadcast game load/unload events should use the bare name too — `loadGame` already broadcasts `"Game loaded: <name>"` to chat, so command handlers must not send a redundant reply.

### `Message` Type (`internal/domain/types.go`)
```go
type Message struct {
    Author       string
    Text         string
    IsPrivate    bool
    ToID         string
    FromID       string
    IsReply      bool  // command response — rendered as plain text, no "[system]" or "[PM]" prefix
    IsFromPlugin bool  // originated from a plugin — plugins skip these to prevent loops
}
```

`IsReply: true` is set by `ctx.Reply()` so command output (e.g. `/help` listing) appears as plain text in the caller's chat window with no prefix. Without it, private messages show `[PM from X]`.

### Games (JS)

Games live in `dist/games/` as either single `.js` files or folders containing `main.js` (for multi-file games using `include()`). Loaded at runtime via `/game load <name>`. A HTTPS URL can be given instead of a name — `.js` files are cached in `dist/games/.cache/`, `.zip` files are extracted to `dist/games/<name>/`. GitHub blob URLs are converted to raw automatically.

**Game** — exports a global `Game` object with hooks `update`, `onPlayerLeave`, `onInput`, `render`, `renderCanvas`, `renderSplash`, `renderGameOver`, `layout`, `statusBar`, `commandBar`. Optional properties: `gameName`, `teamRange`, `charmap`. Mandatory `init(savedState)` called on load. Loaded one at a time; owns the viewport. `update(dt)` is called once per tick with elapsed seconds — all game logic belongs here. `render(buf, playerID, ox, oy, w, h)` receives an `ImageBuffer` and writes pixels directly via `buf.setChar(x, y, ch, fg, bg)`, `buf.writeString(x, y, text, fg, bg)`, `buf.fill(x, y, w, h, ch, fg, bg)`. Colors are `"#RRGGBB"` hex strings or `null`. Attribute constants: `ATTR_BOLD`, `ATTR_FAINT`, `ATTR_ITALIC`, `ATTR_UNDERLINE`, `ATTR_REVERSE`. `renderSplash(buf, playerID, ox, oy, w, h)` renders a custom splash screen (return true); if omitted, framework renders figlet game name. `renderGameOver(buf, playerID, ox, oy, w, h, results)` renders a custom game-over screen (return true); if omitted, framework renders figlet "GAME OVER" + results table. `layout` returns a declarative widget tree describing the game window using NC controls; if defined, `render()` is only called for `{type: "gameview"}` nodes within the tree. Interactive node types (`button`, `textinput`, `checkbox`) route actions back via `onInput(playerID, action)`. Tab cycles focus between interactive controls; Esc returns to raw `onInput` mode.

**Global functions available to JS:** `log()`, `chat()`, `chatPlayer()`, `teams()`, `now()`, `registerCommand()`, `gameOver(results, state)`, `figlet(text, font?)` (ASCII art via figlet4go; built-in fonts: `"standard"`, `"larry3d"`; extra fonts loaded from `dist/fonts/*.flf` at startup), `include(name)` (evaluate another `.js` file from the same directory — for multi-file games).

**Full developer documentation:** see `API-REFERENCE.md` at the repo root.

### Plugins (JS)

Per-player (or per-console) JavaScript extensions in `dist/plugins/`. Loaded with `/plugin load <name|url>`. Each player/console maintains their own plugin list — plugins are not shared.

A plugin exports a `Plugin` object with an `onMessage(author, text, isSystem)` hook. The hook is called for every chat message (or log line, for console plugins). If it returns a non-empty string, that string is dispatched as if the player typed it — starting with `/` means a command, otherwise it's sent as chat. Return `null` to do nothing.

**Loop prevention:** Messages originating from plugins are tagged with `IsFromPlugin: true` and are never fed back to plugin hooks. This prevents cross-plugin infinite loops (Plugin A → chat → Plugin B → chat → Plugin A …). Same-player messages are also skipped (SSH only: `FromID` check). Command replies (`IsReply`) are always skipped too.

**Use cases:** auto-greeting bots, chat responders, server management scripts, auto-moderation.

**Global JS:** `log()` only (for debug output).

**Bundled plugins:** `greeter` (welcomes new players), `echo` (echoes `!echo` messages).

### Shaders (JS / Go)

Per-player (or per-console) post-processing scripts in `dist/shaders/`. Loaded with `/shader load <name|url>`. Each player/console maintains their own ordered shader list. Shaders run in sequence on the fully-rendered `ImageBuffer` **after** the screen is composed but **before** overlays (menus, dialogs) and `ToString()`.

A JS shader exports a `Shader` object with a required `process(buf, time)` method. `time` is total elapsed seconds since server start (deterministic, same value on server and client for local rendering). `buf` exposes:
- `width`, `height` — buffer dimensions
- `getPixel(x, y)` → `{char, fg, bg, attr}` or `null` — read a cell
- `setChar(x, y, ch, fg, bg, attr)` — write a cell
- `writeString(x, y, text, fg, bg, attr)` — write text
- `fill(x, y, w, h, ch, fg, bg, attr)` — fill rectangle
- `recolor(x, y, w, h, fg, bg, attr)` — change colors without changing characters

Optional hooks: `init()` (called once on load), `unload()` (called on removal). **Shaders must be stateless** — all time-based effects must derive from the `time` parameter passed to `process()`. This ensures shaders are pure functions of (buffer × time), replicable on the client for local rendering.

**Go shaders** implement `domain.Shader` interface: `Name() string`, `Process(buf *ImageBuffer, elapsed float64)`, `Unload()`. Compiled into the binary.

**Commands:** `/shader` (list), `/shader load <name>`, `/shader unload <name>`, `/shader list`, `/shader up <name>`, `/shader down <name>`.

**Menu:** File → Shaders... shows active shaders with order and available shaders.

**Bundled shaders:** `invert` (swap fg/bg), `scanlines` (animated scrolling scanlines), `crt` (green-on-black retro terminal), `rainbow` (flowing rainbow on box-drawing borders).

| Package | Role |
|---------|------|
| `internal/engine/shader.go` | JS shader runtime: `jsShader`, `LoadShader()`, `applyShaders()`, JS buffer wrapper with `getPixel`/`setChar`/`recolor` |

### Charmaps (Sprite-Based Rendering)

Games can use **charmap-based sprite rendering** by mapping Unicode Private Use Area codepoints (U+E000–U+F8FF) to sprites in a sprite sheet. Regular SSH clients show tofu/blank for PUA codepoints; the custom `null-space-client` renders them as sprites.

**Charmap format:** Each charmap lives in `dist/charmaps/<name>/` with a `charmap.json` and an atlas PNG:
```json
{
  "name": "pacman",
  "version": 1,
  "cellWidth": 16,
  "cellHeight": 16,
  "atlas": "atlas.png",
  "entries": [
    {"codepoint": 57344, "x": 0, "y": 0, "w": 16, "h": 16, "name": "player"},
    {"codepoint": 57345, "x": 16, "y": 0, "w": 16, "h": 16, "name": "ghost"}
  ]
}
```

**JS game usage:** Set `Game.charmap = "pacman"`, then use PUA codepoints in render:
```js
var Game = {
    charmap: "pacman",
    // ...
};
buf.setChar(x, y, "\uE000", "#ffff00", "#000000"); // renders as sprite in custom client
```

**PUA constants:** `PUA_START` (0xE000) and `PUA_END` (0xF8FF) are available in JS.

**Enhanced client protocol:** The server detects the custom client via `NULL_SPACE_CLIENT=enhanced` SSH env var, then sends charmap data and viewport bounds using in-band OSC escape sequences that regular terminals silently ignore:
- `\x1b]ns;charmap;<base64 JSON>\x07` — charmap definition (sent once on game load)
- `\x1b]ns;atlas;<base64 gzipped PNG>\x07` — sprite sheet (sent once on game load)
- `\x1b]ns;viewport;<x>,<y>,<w>,<h>\x07` — game viewport bounds (sent every frame)
- `\x1b]ns;frame;<base64 gzipped PNG>\x07` — canvas frame (sent every frame when canvas mode active)

**Rendering rules:** Charmaps apply only to the game viewport. NC chrome (menus, dialogs, chat, status bars) always renders as text. Drop shadows on PUA cells clear the sprite and fill with shadow color.

### Canvas Rendering (Server-Side 2D Graphics)

Games can define an optional `renderCanvas(ctx, playerID, w, h)` hook for server-side 2D canvas rendering. The server rasterizes using `fogleman/gg` and sends PNG frames to enhanced clients. Regular SSH players still see the cell-based `render()` output.

**Canvas API (subset of HTML5 Canvas2D):**
- **State:** `save()`, `restore()`
- **Transforms:** `translate(x,y)`, `rotate(angle)`, `scale(sx,sy)`
- **Style:** `setFillStyle(color)`, `setStrokeStyle(color)`, `setLineWidth(w)`
- **Rectangles:** `fillRect(x,y,w,h)`, `strokeRect(x,y,w,h)`, `clearRect(x,y,w,h)`
- **Paths:** `beginPath()`, `closePath()`, `moveTo(x,y)`, `lineTo(x,y)`, `arc(x,y,r,start,end)`, `fill()`, `stroke()`
- **Curves:** `quadraticCurveTo(cpx,cpy,x,y)`, `bezierCurveTo(cp1x,cp1y,cp2x,cp2y,x,y)`
- **Circles:** `fillCircle(x,y,r)`, `strokeCircle(x,y,r)`, `fillEllipse(x,y,rx,ry)`, `strokeEllipse(x,y,rx,ry)`
- **Text:** `fillText(text,x,y)`
- **Pixels:** `setPixel(x,y,color)`
- **Constants:** `PI`, `TAU`

**JS game usage:**
```js
var Game = {
    renderCanvas: function(ctx, playerID, w, h) {
        ctx.setFillStyle("#000000");
        ctx.fillRect(0, 0, w, h);
        ctx.setFillStyle("#ffff00");
        ctx.fillCircle(w/2, h/2, 20);
    },
    render: function(buf, playerID, ox, oy, w, h) {
        // Optional: cell-based overlay (score, HUD) on top of canvas
        buf.writeString(ox, oy, "Score: 42", "#fff", null);
    }
};
```

**Canvas scale:** Admin sets the scaling factor with `/canvas scale <n>` (pixels per cell). `/canvas info` shows current scale, pixel dimensions, and estimated bandwidth per user. `/canvas off` disables canvas rendering. Scale is stored in `CentralState.CanvasScale`. Canvas dimensions = viewport cells × scale. The `/canvas` command shows bandwidth estimates at the console's viewport size.

| Package | Role |
|---------|------|
| `common/charmap.go` | CharMapDef/CharMapEntry types, PUA constants, JSON loader |
| `common/osc.go` | OSC escape sequence encoding for charmap/atlas/viewport/frame, bandwidth estimator |
| `internal/engine/canvas.go` | Headless Canvas2D context (fogleman/gg) exposed to goja |
| `internal/client/` | SSH connection, ANSI parser, Ebitengine sprite + canvas renderer |

### Init Files (`~/.null-space/`)

Both files: one command per line; lines starting with `#` are comments. Dispatched on the first tick after the UI is running. Lives in the home directory so they survive reinstalls.

**`~/.null-space/server.txt`** — commands run automatically when the server console starts. Useful for loading a default game, setting a theme, or loading server-side plugins.

**`~/.null-space/client.txt`** — commands run automatically when a player joins a server (or starts in `--local` mode). The join script reads this file, base64-encodes it, and sends it via the `NULL_SPACE_INIT` SSH environment variable.

Example `~/.null-space/server.txt`:
```
# Server auto-setup
/theme dark
/game load invaders
```

Example `~/.null-space/client.txt`:
```
# Client auto-setup
/theme dark
/plugin load greeter
```

### Themes

JSON files in `dist/themes/` that control the NC-style chrome colors. Switch at runtime with `/theme <name>` (per-player, not global). Bundled themes: `norton` (default), `dark`, `light`.

Themes use a 4-layer depth model matching the original Norton Commander. Each layer (`ThemeLayer`) carries **both** a color palette (`Palette`) **and** a border character set (`BorderSet`):

| Layer | Depth | NC role |
|-------|-------|---------|
| Primary | 0 | Desktop: main windows, panels |
| Secondary | 1, 3, 5… | Menus, dropdowns, status bar |
| Tertiary | 2, 4, 6… | Dialogs, nested popups |
| Warning | (explicit) | Error/warning dialogs |

`Theme.LayerAt(depth)` returns the layer, cycling Secondary/Tertiary for depth > 0. `Theme.WarningLayer()` returns the Warning layer regardless of depth. `Theme.ShadowStyle()` is global (not per-layer).

**Color fields** (per layer): `bg/fg`, `accent`, `highlightBg/Fg`, `activeBg/Fg`, `inputBg/Fg`, `disabledFg`. **Border fields** (per layer): outer frame (`outerTL/TR/BL/BR/H/V`), inner dividers (`innerH/V`), intersections (`crossL/R/T/B/X`), bar separator (`barSep`). Defaults: double-line outer (`╔═╗║╚╝`), single-line inner (`─│`), intersections (`╟╢╤╧`). Any omitted field falls back to hardcoded defaults. Different layers can use different border styles (e.g., double-line for desktop, single-line for menus).

**Render signatures:** `Control.Render(buf, x, y, w, h, focused, layer)` writes directly into a `*ImageBuffer`. `Window.RenderToBuf(buf, x, y, w, h, layer)` writes into a caller-provided buffer. `Screen.RenderToBuf(buf, x, y, w, h, theme)` renders the full chrome (MenuBar at secondary layer, Window at primary, StatusBar at secondary). `MenuBar` renders directly into the buffer using `SetChar`/`WriteString` (no lipgloss). Dropdown/dialog renderers still return strings painted via `PaintANSI` + `Blit`.

**Widget tree reconciler** (`internal/widget/reconcile.go`): `ReconcileGameWindow()` builds real `Control` instances from a `WidgetNode` tree, reusing controls by tree path to preserve state (focus, cursor, scroll) across frames. Supports interactive nodes: `button` (action via OnInput), `textinput` (submit via OnInput), `checkbox` (toggle via OnInput), `textview` (scrollable), `gameview` (optionally focusable). NC framework owns focus — Tab cycles controls, Esc blurs all, unfocused keys fall through to `game.OnInput()`.

**JSON backwards compat**: Global border fields at the theme root are copied into any layer that has empty borders via `resolveDefaults()`. New themes should define borders per-layer.

---

## Server Console

`internal/console/console.go` is its own Bubble Tea program on the local terminal. Two phases:

### Phase 1 — Boot sequence

Each step is printed in two passes:
1. **Before** the operation: `label ...................` (dots to fill line, no status, no newline)
2. **After** the operation: `\r` overwrites the line with `label ........ [ STATUS ]` right-aligned

Status tokens are always **8 chars wide** with the text centered:
```
[ DONE ]   (DONE = 4 chars, no padding)
[ FAIL ]   (FAIL = 4 chars, no padding)
[ SKIP ]   (SKIP = 4 chars, no padding)
```

Implementation: `startBootStep(label)` / `finishBootStep(status)` in `cmd/null-space-server/main.go`. Terminal width via `github.com/charmbracelet/x/term`. The PS1 script has matching `Write-BootStepStart` / `Write-BootStepEnd` helpers.

Startup sequence (PS1 steps first, then Go binary):
```
Setting up network ......................................... [ DONE ]  ← PS1 header
Pinggy helper .............................................. [ DONE ]  ← PS1
SSH server ................................................. [ DONE ]  ← Go
UPnP port mapping .......................................... [ SKIP ]
Public IP detection ........................................ [ SKIP ]
Pinggy tunnel .............................................. [ DONE ]
Generating invite command .................................. [ DONE ]

  <invite command>

  (console UI runs)

Initiating shutdown ........................................ [ DONE ]  ← Go
Shutting down network ...................................... [ DONE ]  ← Go header
Stopping SSH server ........................................ [ DONE ]  ← Go
Stopping Pinggy helper ..................................... [ DONE ]  ← PS1
```

In `--local` mode, group headers show `[ SKIP ]` (yellow) and substeps are omitted:
```
Setting up network ......................................... [ SKIP ]  ← PS1
Generating invite command .................................. [ SKIP ]  ← Go
  (local TUI runs)
Initiating shutdown ........................................ [ DONE ]  ← Go
Shutting down network ...................................... [ SKIP ]  ← Go
```

### Phase 2 — Console UI

Uses the same Screen layout as all views (MenuBar + Window + StatusBar):

```
│ File  View  Help                    │  MenuBar (row 0)
╔════════════════════════════════════╗
║                                    ║
║ Log (scrollable, fills height)     ║  NCTextView: slog lines + all chat
║                                    ║  PgUp/PgDn to scroll
║                                    ║
╟────────────────────────────────────╢
║ [·····]                            ║  NCCommandInput: '/' = command; plain text = chat
╚════════════════════════════════════╝
│ game: none | players: 0 | 15:04:05│  StatusBar (row 2)
```

The server console is always admin. SSH clients elevate via `/admin <password>`. Password set via `--password`; changeable at runtime via `/password <new>` (admin only).

---

## Connection Strategy

Startup order: UPnP → Pinggy → generate invite script.

The invite command is a raw PowerShell one-liner (paste into a PowerShell window): `$env:NS='<token>';irm <join.ps1 URL>|iex`. The `NS` environment variable is a base64url-encoded binary token:

| Bytes | Field | Notes |
|-------|-------|-------|
| 0–1 | SSH port (uint16 BE) | Shared by localhost, LAN, UPnP |
| 2–5 | LAN IP (4 bytes) | `0.0.0.0` = absent |
| 6–9 | Public/UPnP IP (4 bytes) | `0.0.0.0` = absent |
| 10–11 | Pinggy port (uint16 BE) | `0` = no Pinggy |
| 12+ | Pinggy hostname (UTF-8) | Remaining bytes |

Variable-length: trailing absent fields are omitted. `join.ps1` always tries `localhost` first (not encoded). Field presence is determined by token length: ≥6 → LAN, ≥10 → public IP, ≥12 → Pinggy.

Each attempt uses a short `ConnectTimeout`; falls through on failure.

`pinggy-helper.exe` stdout/stderr are redirected to `dist/logs/pinggy-stdout.log` / `pinggy-stderr.log` by `start.ps1` — they must not pollute the boot sequence output.

---

## Concurrency — Lock Ordering

Two primary mutexes protect shared state:

| Mutex | Type | Location | Protects |
|-------|------|----------|----------|
| `CentralState.mu` | RWMutex | `internal/state/state.go` | Players, teams, game phase, chat history, network info |
| `jsRuntime.mu` | Mutex | `internal/engine/runtime.go` | Goja JS VM and all JS callback execution |

**Invariant:** `jsRuntime` must **never** acquire `CentralState.mu`. This is enforced structurally — `jsRuntime` has no reference to `CentralState`. Data flows through:
- **Teams:** Server builds a cache (`buildTeamsCache`) and pushes it via `SetTeamsCache()`. JS `teams()` reads the local cache.
- **Chat:** JS `chat()`/`chatPlayer()` send on a buffered channel; a server goroutine drains it and calls `broadcastChat()`.

**Callers** (`internal/server/server.go`, `internal/chrome/model.go`) must release `state.mu` **before** calling any `jsRuntime` Game method (`Init`, `Start`, `Update`, `Render`, `OnInput`, etc.). All existing call sites follow this pattern — verify any new ones do too.

Other mutexes (`programsMu`, `sessionsMu`, `consoleProgramMu`, `commandRegistry.mu`) are leaf locks — they don't call into JS or acquire `state.mu`.

---

## Slog Feedback Loop Guard

**Never add `slog` calls to `View()` or `Render()` methods.** The console routes slog → channel → Update → View, so any slog call in the render path creates an infinite feedback loop (high CPU, starved keyboard events).

The `consoleSlogHandler` has a runtime guard (`isRenderPath()`) that inspects the call stack and suppresses console-channel sends from inside `.View` or `.Render` methods. This is a safety net — the primary rule is still: don't log from render paths. `TestNoSlogInRenderPath` scans render-path source files for slog calls; `TestSlogBlockedInRenderPath` verifies the runtime guard.

---

## Dependencies (charm.land v2 stack)
- `charm.land/bubbletea/v2` — TUI framework
- `charm.land/wish/v2` — SSH server (use `bubbletea.Middleware`, not deprecated wish middleware)
- `charm.land/lipgloss/v2` — terminal styling/layout
- `charm.land/bubbles/v2` — `textinput`, `viewport` components
- `github.com/charmbracelet/x/term` — terminal size detection
- `github.com/huin/goupnp` — UPnP IGD
- `github.com/dop251/goja` — JavaScript runtime for games

---

## SSH Input Handling (Windows gotcha)

Use `ssh.EmulatePty()` — **not** `ssh.AllocatePty()` — in all three call sites in `internal/server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `internal/server/server.go` to find all three call sites.
