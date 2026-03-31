# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **For Claude:** This file is the portable memory for this project. Whenever you make a change, discover a gotcha, or establish a pattern or decision, **update this file before committing**. It is the single source of truth that survives new clones, new machines, and new sessions. Keep it accurate and concise — do not let it drift from the actual code.
>
> **For Claude:** When you have completed a task or a logical unit of work, **commit and push to git**. Don't wait to be asked.

## Project Goal

A framework for hosting terminal-based multiplayer **games** over SSH. **Only the server operator needs to install anything.** Players connect with a plain `ssh` command — no client install required.

Games and plugins are written in JavaScript (goja) and loaded at runtime from `dist/games/` and `dist/plugins/`. The server binary itself is game/plugin-agnostic.

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
go run ./cmd/null-space --local --data-dir dist --game example --plugins foo,bar
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
2. **Splash**: Shows game splash screen (custom or default with game name). Admin presses Enter to start, or auto-starts after 10s.
3. **Playing**: Normal game mode. Game calls `gameOver(results, state)` when done. Only players who had teams at game start participate.
4. **Game Over**: Framework renders ranked results screen. All players press Enter or 15s auto-transition.
5. Back to **Lobby** — game unloaded, teams preserved for next round.

Late joiners during any game phase see the lobby and can chat but don't join the active game. Teams are locked while a game is running.

### Teams
Players manage teams in the lobby panel (right side, 30% width). New players start unassigned (no team). Tab switches focus between chat and team panel. In team panel: Up/Down moves between teams, first player can Enter to rename and Left/Right to cycle color. Games can declare `teamRange: {min, max}` to enforce valid team counts. Teams are available to games via the `teams()` global function. Teams lock during a game — lobby waiters cannot modify them.

### State Persistence
Games persist state by passing it as the second argument to `gameOver(results, state)`. On the next load, it's received as the argument to `init(savedState)`. State files are stored as JSON in `dist/state/<gamename>.json`.

### UI Layout

**Lobby (no game loaded):**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — framework      │  e.g. "null-space | 3 players online | 00:42 ⠹"
├──────────────────────┬──────────────┤
│ Chat (70% width)     │ Teams (30%)  │
│                      │  Red Team    │
│                      │   > alice    │
│                      │     bob      │
│                      │  Blue Team   │
│                      │     charlie  │
├──────────────────────┴──────────────┤
│ Command bar (1 row) — dual-purpose  │  [Tab] toggles chat/teams focus
└─────────────────────────────────────┘  In teams: [↑↓] move, [←→] color, [Enter] rename
```

**In-game:**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — game-owned     │  Game.StatusBar(playerID) → "HP: 100  Score: 4200 ⠹"
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
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```

**Braille spinner:** the last character of every status bar row is reserved for a Braille spinner — a live indicator that the server is running. Sequence: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, advances once per second (every 10 ticks at 100ms). Status bar content must never overwrite it.

**Viewport sizing:** Ideal `gameH = W * 9 / 16`. Chat gets the remaining rows. `minChatH = max(5, (H-2)/3)` — chat always gets at least ⅓ of content rows, so both game and chat grow proportionally on short terminals. Once `H` is large enough for ideal `gameH`, chat takes all extra rows. Command bar is always 1 row.

**Chat scroll buffer:** 200 lines per player. `PgUp`/`PgDn` scroll the chat panel in both idle and input modes. Multi-line command replies (e.g. `/help`) are split into individual lines before storage.

**Command history:** 50 entries per player. In input mode, `↑`/`↓` browse history. `↓` past the newest entry restores the draft that was in the input box when browsing started. History does not rotate.

### Key Packages

| Package | Role |
|---------|------|
| `server/server.go` | Wish SSH server setup, session lifecycle, tick broadcast, `Server` orchestrator, game lifecycle (splash/gameOver) |
| `server/chrome.go` | Per-user `chromeModel`: renders lobby (with teams panel), splash, game, game-over screens |
| `server/state.go` | `CentralState`: players, chat, active game, plugins, teams, game phase, game-over readiness |
| `server/state_persist.go` | Load/save game state JSON files in `dist/state/` |
| `server/commands.go` | `/` command registry, tab completion, permission checks |
| `server/console.go` | Local server management terminal (not for playing) |
| `server/runtime.go` | JS game runtime (goja): loads `dist/games/*.js`, implements `common.Game` |
| `server/plugin.go` | JS plugin runtime (goja): loads `dist/plugins/*.js`, implements `common.Plugin` |
| `server/local.go` | Local (non-SSH) mode: single-player / render test-bed |
| `server/upnp.go` | Auto UPnP port mapping on start, cleanup on shutdown |
| `server/pinggy.go` | Polls Pinggy status file, updates `state.Net.PinggyURL` |
| `common/interfaces.go` | `Game` and `Plugin` interface contracts, `Command` struct |
| `common/types.go` | Shared types: `Message`, `Player`, `TickMsg`, `ChatMsg`, etc. |
| `cmd/null-space/` | Entry point: boot sequence, console setup, signal handling |
| `cmd/pinggy-helper/` | Standalone helper that runs the Pinggy SSH tunnel |
| `dist/start.ps1` | PowerShell launcher: starts pinggy-helper, then null-space.exe |

### The `Game` Interface (`common/interfaces.go`)
```go
type Game interface {
    GameName() string                      // display name (fallback: filename stem)
    TeamRange() TeamRange                  // {Min, Max} — zero = no constraint
    SplashScreen() string                  // splash screen content (empty = use default)
    OnPlayerJoin(playerID, playerName string)
    OnPlayerLeave(playerID string)
    OnInput(playerID, key string)
    View(playerID string, width, height int) string
    StatusBar(playerID string) string      // content for top status bar (spinner appended by framework)
    CommandBar(playerID string) string     // idle hint in command bar
    Commands() []Command
    Unload()
}
```
`jsRuntime` implements `Game`. All JS hooks are optional — zero values returned when not defined. `init()` is called internally by `LoadGame`, not part of the interface.

### Game Over

Games call `gameOver(results, state)` where `results` is an array of `{ name, result }` in ranked order and `state` is an optional object to persist for the next run. The framework renders the game-over screen — games don't need to provide their own. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). Both arguments are optional. State is received via `config.savedState` in `init()` on the next load.

### The `Plugin` Interface
```go
type Plugin interface {
    OnChatMessage(msg *Message) *Message  // return nil to drop; runs in load order before chat history
    OnPlayerJoin(playerID, playerName string)
    OnPlayerLeave(playerID string)
    Commands() []Command
    Skin() *SkinColors  // returns nil if plugin doesn't provide a skin
    Unload()
}
```

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

### Games and Plugins (JS)

Both are single `.js` files in `dist/games/` or `dist/plugins/`. Loaded at runtime via `/game load <name>` / `/plugin load <name>`. A HTTPS URL can be given instead of a name — the file is downloaded and cached in `dist/games/.cache/` or `dist/plugins/.cache/`. GitHub blob URLs are converted to raw automatically.

**Game** — exports a global `Game` object with hooks `onPlayerJoin`, `onPlayerLeave`, `onInput`, `view`, `statusBar`, `commandBar`. Optional properties: `gameName`, `teamRange`, `splashScreen`. Mandatory `init(savedState)` called on load. Loaded one at a time; owns the viewport.

**Plugin** — exports a global `Plugin` object with hooks `onChatMessage`, `onPlayerJoin`, `onPlayerLeave`. Multiple active simultaneously; persistent across game switches.

**Global functions available to JS:** `log()`, `chat()`, `chatPlayer()`, `players()`, `teams()`, `registerCommand()`, `gameOver(results, state)`.

The chat pipeline runs all active plugin `onChatMessage` hooks (in load order) before committing a message to history. Return `null` to drop.

**Skin plugins:** A JS plugin can set `Plugin.skin = { statusBg, statusFg, chatBg, chatFg, cmdBg, cmdFg, inputBg, inputFg }` to override framework chrome colors. The first loaded plugin with a non-null skin wins. Colors are CSS hex strings. Any omitted field uses the framework default. Bundled skins: `skin-dracula`, `skin-matrix`, `skin-nord`.

**Full developer documentation:** see `API-REFERENCE.md` at the repo root.

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

### Phase 2 — Console UI (2-panel)

```
┌─────────────────────────────────────┐
│ Status bar (1 row)                  │  "null-space | game: none | uptime 00:42 ⠹"
├─────────────────────────────────────┤
│ Server log (top half, scrollable)   │  internal log lines; never sent to players
├─────────────────────────────────────┤
│ Chat label (1 row)                  │  "Chat (N players online)"
├─────────────────────────────────────┤
│ Chat view (bottom half, scrollable) │  all messages; private prefixed "[PM a→b]"
├─────────────────────────────────────┤
│ Admin input (1 row)                 │  '/' = command; plain text = chat as [admin]
└─────────────────────────────────────┘
```

The server console is always admin. SSH clients elevate via `/admin <password>`. Password set via `--password`; changeable at runtime via `/password <new>` (admin only).

---

## Connection Strategy

Startup order: UPnP → Pinggy → generate invite script.

The invite script is a PowerShell one-liner that tries in order:
1. `localhost:<port>` — same machine
2. `<UPnP public IP>:<port>` — direct internet
3. Pinggy relay — always works, highest latency

Each attempt uses a short `ConnectTimeout`; falls through on failure.

`pinggy-helper.exe` stdout/stderr are redirected to `dist/logs/pinggy-stdout.log` / `pinggy-stderr.log` by `start.ps1` — they must not pollute the boot sequence output.

---

## Dependencies (charm.land v2 stack)
- `charm.land/bubbletea/v2` — TUI framework
- `charm.land/wish/v2` — SSH server (use `bubbletea.Middleware`, not deprecated wish middleware)
- `charm.land/lipgloss/v2` — terminal styling/layout
- `charm.land/bubbles/v2` — `textinput`, `viewport` components
- `github.com/charmbracelet/x/term` — terminal size detection
- `github.com/huin/goupnp` — UPnP IGD
- `github.com/dop251/goja` — JavaScript runtime for games/plugins

---

## SSH Input Handling (Windows gotcha)

Use `ssh.EmulatePty()` — **not** `ssh.AllocatePty()` — in all three call sites in `server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `server/server.go` to find all three call sites.
