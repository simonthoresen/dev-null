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

- **GitHub Actions** (`.github/workflows/release.yml`): builds `null-space.exe` + `pinggy-helper.exe`, packages the full `dist/` folder into `null-space.zip`, and publishes a rolling `latest` release.
- **`install.ps1`** (repo root): one-liner installer for operators — downloads and extracts the latest release zip, creates desktop shortcuts. Usage: `irm https://github.com/simonthoresen/null-space/raw/main/install.ps1 | iex`
- **`start.ps1`** (in `dist/`): auto-updates on each launch — checks the GitHub release for a newer version and downloads the full zip (binaries, games, fonts) before starting.
- **Version tracking**: `dist/.version` stores the commit SHA of the installed release. Not tracked in git.

## Commands

```bash
make build          # compile to dist/null-space.exe + dist/pinggy-helper.exe
make run            # go run with --data-dir dist (dev shortcut)
make clean          # remove compiled binaries from dist/

go run ./cmd/null-space --data-dir dist   # equivalent to make run, add --password etc.
go test ./...

ssh -p 23234 localhost   # connect as a client (host plays this way too)

# Local mode — no SSH, runs full client TUI directly in the terminal.
# Useful as a render test-bed and as a local single-player mode.
go run ./cmd/null-space --local --data-dir dist
go run ./cmd/null-space --local --data-dir dist --game example
go run ./cmd/null-space --local --data-dir dist --game example --player alice
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
```
1. **Lobby**: Players configure teams, chat. Admin loads game with `/game load <name>`.
2. **Load**: Framework snapshots teams for the game (lobby teams stay independent), loads saved state, calls `init(savedState)`. `teams()` returns game teams. Game can set `Game.splashScreen` dynamically.
3. **Splash**: Shows game splash screen (custom or default with game name). Admin presses Enter to start, or auto-starts after 10s.
4. **Splash→Playing**: Framework calls `start()`. Game sets up its playing state.
5. **Reconnect**: If a player disconnects mid-game and reconnects with the same name, they rejoin the game automatically.
5. **Playing**: Normal game mode. Game calls `gameOver(results, state)` when done.
4. **Game Over**: Framework renders ranked results screen. All players press Enter or 15s auto-transition.
5. Back to **Lobby** — game unloaded, teams preserved for next round.

Late joiners see the lobby and can chat but don't join the active game. Lobby teams are independent from game teams — players can freely organize for the next round while a game is running.

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

### State Persistence
Games persist state by passing it as the second argument to `gameOver(results, state)`. On the next load, it's received as the argument to `init(savedState)`. State files are stored as JSON in `dist/state/<gamename>.json`.

### UI Layout

**Lobby (no game loaded):**
```
┌──────────────────────┬──────────────┐
│ Menu bar (warm)      │ Teams (blue) │  Split menu bar; active panel bold+bright
├──────────────────────┼──────────────┤
│ Chat (fills rest)    │ Teams (32ch) │  Two color palettes: warm white + cool blue
│                      │  Unassigned  │  Active panel = brighter bg + bold title bar
│                      │   > alice    │
│                      │  Red Team    │
│                      │     bob      │
│                      │  Blue Team   │
│                      │     charlie  │
├──────────────────────┼──────────────┤
│ Chat input row       │ Team cmd bar │  [Tab] toggles chat/teams focus
├──────────────────────┴──────────────┤
│ Status bar (always)                 │  server time right-aligned
└─────────────────────────────────────┘  In teams: [↑↓] move, [←→] color, [Enter] rename
```

**In-game:**
```
┌─────────────────────────────────────┐
│ Menu bar (1 row) — framework        │  game name
├─────────────────────────────────────┤
│ Status bar (1 row) — game-owned     │  Game.StatusBar(playerID) → "HP: 100  Score: 4200"
├─────────────────────────────────────┤
│                                     │
│ Game viewport (W × W*9/16 rows)     │  Game.View(playerID, W, H)
│                                     │
├─────────────────────────────────────┤
│                                     │
│ Chat (remaining rows, min 5)        │  shared chat history
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row) — dual-purpose  │  idle: Game.CommandBar(playerID) → "[↑↓] Move"
├─────────────────────────────────────┤
│ Status bar (1 row) — framework      │  server time right-aligned              always
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```


**Viewport sizing:** Ideal `gameH = W * 9 / 16`. Chat gets the remaining rows. `minChatH = max(5, (H-4)/3)` — chat always gets at least ⅓ of content rows (4 overhead rows: menu bar + game status bar + command bar + status bar). Command bar is always 1 row.

**Chat scroll buffer:** 200 lines per player. `PgUp`/`PgDn` scroll the chat panel in both idle and input modes. Multi-line command replies (e.g. `/help`) are split into individual lines before storage.

**Command history:** 50 entries per player. In input mode, `↑`/`↓` browse history. `↓` past the newest entry restores the draft that was in the input box when browsing started. History does not rotate.

### Key Packages

| Package | Role |
|---------|------|
| `server/server.go` | Wish SSH server setup, session lifecycle, tick broadcast, `Server` orchestrator, game lifecycle (splash/gameOver) |
| `server/chrome.go` | Per-user `chromeModel`: renders lobby (with teams panel), splash, game, game-over screens |
| `server/state.go` | `CentralState`: players, chat, active game, teams, game phase, game-over readiness |
| `server/state_persist.go` | Load/save game state JSON files in `dist/state/` |
| `server/commands.go` | `/` command registry, tab completion, permission checks |
| `server/console.go` | Local server management terminal (not for playing) |
| `server/runtime.go` | JS game runtime (goja): loads `dist/games/*.js`, implements `common.Game` |
| `server/plugin.go` | Per-player JS plugin runtime: loads `dist/plugins/*.js`, calls `onMessage` hook |
| `server/ncwidget.go` | NC widget core: `NCWindow` (grid bag layout, border/title/shadow), `NCControl` interface, focus/cursor/click management |
| `server/nccontrols.go` | NC controls: `NCLabel`, `NCTextInput`, `NCTextArea`, `NCTextView`, `NCButton`, `NCCheckbox`, `NCHDivider`, `NCVDivider`, `NCPanel` (bordered sub-container) |
| `server/theme.go` | Theme system: loads JSON color palettes from `dist/themes/`, applies to NC chrome |
| `server/local.go` | Local (non-SSH) mode: single-player / render test-bed |
| `server/upnp.go` | Auto UPnP port mapping on start, cleanup on shutdown |
| `server/pinggy.go` | Polls Pinggy status file, updates `state.Net.PinggyURL` |
| `common/interfaces.go` | `Game` interface contract, `Command` struct |
| `common/types.go` | Shared types: `Message`, `Player`, `TickMsg`, `ChatMsg`, etc. |
| `cmd/null-space/` | Entry point: boot sequence, console setup, signal handling |
| `cmd/pinggy-helper/` | Standalone helper that runs the Pinggy SSH tunnel |
| `dist/start.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts pinggy-helper, then null-space.exe |
| `install.ps1` | One-liner installer: downloads latest release zip, extracts to a folder, creates desktop shortcuts |
| `.github/workflows/release.yml` | CI: builds binaries and publishes rolling `latest` release on every push to main |

### The `Game` Interface (`common/interfaces.go`)
```go
type Game interface {
    GameName() string                      // display name (fallback: filename stem)
    TeamRange() TeamRange                  // {Min, Max} — zero = no constraint
    SplashScreen() string                  // splash screen content (empty = use default)
    Init(savedState any)                   // called before splash with persisted state
    Start()                                // called at splash→playing transition
    OnPlayerLeave(playerID string)
    OnInput(playerID, key string)
    View(playerID string, width, height int) string
    StatusBar(playerID string) string      // game status bar (2nd row, below menu bar)
    CommandBar(playerID string) string     // command bar (above framework status bar)
    Commands() []Command
    Unload()
}
```
`jsRuntime` implements `Game`. `init()` is mandatory; all other JS hooks are optional. `teams()` global returns game team snapshot during init/start/playing.

### Game Over

Games call `gameOver(results, state)` where `results` is an array of `{ name, result }` in ranked order and `state` is an optional object to persist for the next run. The framework renders the game-over screen — games don't need to provide their own. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). Both arguments are optional. State is received via `config.savedState` in `init()` on the next load.

### Commands (`common/interfaces.go`)
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

### `Message` Type (`common/types.go`)
```go
type Message struct {
    Author    string
    Text      string
    IsPrivate bool
    ToID      string
    FromID    string
    IsReply   bool  // command response — rendered as plain text, no "[system]" or "[PM]" prefix
}
```

`IsReply: true` is set by `ctx.Reply()` so command output (e.g. `/help` listing) appears as plain text in the caller's chat window with no prefix. Without it, private messages show `[PM from X]`.

### Games (JS)

Games live in `dist/games/` as either single `.js` files or folders containing `main.js` (for multi-file games using `include()`). Loaded at runtime via `/game load <name>`. A HTTPS URL can be given instead of a name — `.js` files are cached in `dist/games/.cache/`, `.zip` files are extracted to `dist/games/<name>/`. GitHub blob URLs are converted to raw automatically.

**Game** — exports a global `Game` object with hooks `onPlayerJoin`, `onPlayerLeave`, `onInput`, `view`, `statusBar`, `commandBar`. Optional properties: `gameName`, `teamRange`, `splashScreen`. Mandatory `init(savedState)` called on load. Loaded one at a time; owns the viewport.

**Global functions available to JS:** `log()`, `chat()`, `chatPlayer()`, `teams()`, `registerCommand()`, `gameOver(results, state)`, `figlet(text, font?)` (ASCII art via figlet4go; built-in fonts: `"standard"`, `"larry3d"`; extra fonts loaded from `dist/fonts/*.flf` at startup), `include(name)` (evaluate another `.js` file from the same directory — for multi-file games).

**Full developer documentation:** see `API-REFERENCE.md` at the repo root.

### Plugins (JS)

Per-player (or per-console) JavaScript extensions in `dist/plugins/`. Loaded with `/plugin load <name|url>`. Each player/console maintains their own plugin list — plugins are not shared.

A plugin exports a `Plugin` object with an `onMessage(author, text, isSystem)` hook. The hook is called for every chat message (or log line, for console plugins). If it returns a non-empty string, that string is dispatched as if the player typed it — starting with `/` means a command, otherwise it's sent as chat. Return `null` to do nothing.

**Use cases:** auto-greeting bots, chat responders, server management scripts, auto-moderation.

**Global JS:** `log()` only (for debug output).

**Bundled plugins:** `greeter` (welcomes new players), `echo` (echoes `!echo` messages).

### Init Files

**Server:** `~/.null-space-server.txt` — commands run automatically when the server console starts. Dispatched on the first tick after the console UI is running. Useful for loading a default game, setting a theme, or loading server-side plugins.

**Client:** `~/.null-space.txt` — commands run automatically when a player joins a server. The join script reads this file, base64-encodes it, and sends it via the `NULL_SPACE_INIT` SSH environment variable. The server dispatches each line on the first tick after connection.

Both files: one command per line; lines starting with `#` are comments.

Example `~/.null-space-server.txt`:
```
# Server auto-setup
/theme dark
/game load invaders
```

Example `~/.null-space.txt`:
```
# Client auto-setup
/theme dark
/plugin load greeter
```

### Themes

JSON files in `dist/themes/` that control the NC-style chrome colors. Switch at runtime with `/theme <name>` (per-player, not global). Bundled themes: `norton` (default), `dark`, `light`.

Themes use a 4-layer depth model matching the original Norton Commander:

| Layer | Field prefix | NC role |
|-------|-------------|---------|
| 0 — Desktop | `desktopBg/Fg` | Action bar, background behind everything |
| 1 — Menu | `menuBg/Fg` | Dropdown menus from the action bar |
| 2 — Dialog | `dialogBg/Fg` | Modal dialog boxes |
| 3 — Popup | `popupBg/Fg` | Nested popups inside dialogs |

Plus: `highlightBg/Fg` (focused items, title bars, active buttons), `disabledFg`, `shadowBg`. Border characters are also themeable with outer/inner/intersection sets matching the NC model: outer frame (`outerTL/TR/BL/BR/H/V`), inner dividers (`innerH/V`), intersections where inner meets outer (`crossL/R/T/B`), and `barSep`. Defaults to double-line outer (`╔═╗║╚╝`) with single-line inner (`─│`) and proper intersection chars (`╟╢╤╧`), matching the original NC window style. A **window** is a top-level container (double outer border); **panels** are subdivisions inside a window (separated by inner single-line dividers). Any omitted field falls back to the norton defaults. Theme authors can deliberately share colors between layers.

---

## Server Console

`server/console.go` is its own Bubble Tea program on the local terminal. Two phases:

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

Implementation: `startBootStep(label)` / `finishBootStep(status)` in `cmd/null-space/main.go`. Terminal width via `github.com/charmbracelet/x/term`. The PS1 script has matching `Write-BootStepStart` / `Write-BootStepEnd` helpers.

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

```
┌─────────────────────────────────────┐
│ Menu bar (1 row, with spinner)      │  "null-space | game: none | teams: 0 | uptime 00:42 ⠹"
├─────────────────────────────────────┤
│                                     │
│ Log (scrollable, fills height)      │  slog lines + all chat (global + private)
│                                     │  PgUp/PgDn to scroll
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row)                 │  '/' = command; plain text = chat as [admin]
├─────────────────────────────────────┤
│ Status bar (1 row)                  │  server time right-aligned
└─────────────────────────────────────┘
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
| `CentralState.mu` | RWMutex | `state.go` | Players, teams, game phase, chat history, network info |
| `jsRuntime.mu` | Mutex | `runtime.go` | Goja JS VM and all JS callback execution |

**Invariant:** `jsRuntime` must **never** acquire `CentralState.mu`. This is enforced structurally — `jsRuntime` has no reference to `CentralState`. Data flows through:
- **Teams:** Server builds a cache (`buildTeamsCache`) and pushes it via `SetTeamsCache()`. JS `teams()` reads the local cache.
- **Chat:** JS `chat()`/`chatPlayer()` send on a buffered channel; a server goroutine drains it and calls `broadcastChat()`.

**Callers** (server.go, chrome.go) must release `state.mu` **before** calling any `jsRuntime` Game method (`Init`, `Start`, `View`, `OnInput`, etc.). All existing call sites follow this pattern — verify any new ones do too.

Other mutexes (`programsMu`, `sessionsMu`, `consoleProgramMu`, `commandRegistry.mu`) are leaf locks — they don't call into JS or acquire `state.mu`.

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

Use `ssh.EmulatePty()` — **not** `ssh.AllocatePty()` — in all three call sites in `server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `server/server.go` to find all three call sites.
