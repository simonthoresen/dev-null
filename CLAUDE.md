# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Goal

A framework for hosting terminal-based multiplayer games over SSH. **Only the server operator needs to install anything.** Players (including the host) connect with a plain `ssh` command — no client install required.

Games are written in a scripting language and loaded at runtime from the `games/` folder. The server binary itself is game-agnostic.

## Commands

```bash
# Run the server
go run ./cmd/null-space --password changeme --address :23234

# Build binaries
go build ./cmd/null-space
go build ./cmd/pinggy-helper

# Run tests (none exist yet)
go test ./...

# Connect as a client (host also plays this way — not from the server terminal)
ssh -p 23234 localhost
```

**Environment variables:**
- `NULL_SPACE_LOG_FILE` — path to log file (default: discard)
- `NULL_SPACE_LOG_LEVEL` — log level: debug/info/warn/error (default: info)
- `NULL_SPACE_PINGGY_STATUS_FILE` — path to Pinggy status file (enables tunnel bridge UI)

## Architecture

**null-space** is a "Multitenant Singleton" multiplayer game server over SSH.

### Core Pattern
- **One game singleton** runs on the server (`CentralState.ActiveGame`)
- **One Bubble Tea `Program` per SSH session**, all sharing the same game state
- **Central 100ms ticker** sends `TickMsg` to all programs simultaneously → synchronized real-time rendering
- **The server terminal is management-only.** The host joins as a player via SSH like everyone else.

### Lobby vs. In-Game
Players start in the **server lobby** — chat only, no game running. An admin uses a `/load <name>` command to load a game from `games/`. This transitions all players into the game view.

### UI Layout

**Lobby:**
```
┌─────────────────────────────────────┐
│ Title bar (1 row)                   │
├─────────────────────────────────────┤
│                                     │
│ Chat (fills remaining height)       │
│                                     │
├─────────────────────────────────────┤
│ Input (1 row)                       │
└─────────────────────────────────────┘
```

**In-game:**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — game-owned     │  Game.statusBar(playerID) → "HP: 100  Score: 4200"
├─────────────────────────────────────┤
│                                     │
│ Game viewport (W × W*9/16 rows)     │  Game.view(playerID, W, H)
│                                     │
├─────────────────────────────────────┤
│                                     │
│ Chat (remaining rows, min 5)        │  shared chat history
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row) — dual-purpose  │  idle: Game.commandBar(playerID) → "[↑↓] Move  [B] Build"
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```

**Lobby (no game loaded):**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — framework      │  e.g. "null-space | 3 players online | 00:42"
├─────────────────────────────────────┤
│                                     │
│ Chat (fills remaining rows)         │
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row) — dual-purpose  │  idle: "[Enter] to chat  /help for commands"
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```

**Braille spinner:** the last character of the status bar (both server console and SSH client) is reserved for a Braille spinner — a live indicator that the server is running. It advances once per second (every 10 game ticks at 100ms). Sequence: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`. The status bar must always reserve 1 character on the right for it and never let game/framework content overwrite that position.

**Viewport height calculation:** always fill terminal width W; `gameH = W * 9 / 16`. Chat gets remaining rows (min 5). Command bar is always 1 row; framework owns the input-field toggle.

**Zone ownership summary:**

| Zone | Owner | Game API hook |
|------|-------|---------------|
| Status bar | Game (lobby: framework) | `statusBar(api, playerID)` |
| Game viewport | Game | `view(api, playerID, w, h)` |
| Chat | Framework | — |
| Command bar (idle) | Game (lobby: framework) | `commandBar(api, playerID)` |
| Command bar (active) | Framework | — (input field, framework routes on submit) |

### Key Packages

| Package | Role |
|---------|------|
| `server/server.go` | Wish SSH server setup, session lifecycle, tick broadcast, `App` orchestrator |
| `server/chrome.go` | Per-user `chromeModel`: renders lobby or game chrome depending on state |
| `server/state.go` | `CentralState`: players map, chat history (max 50), active game |
| `server/commands.go` | `/` command registry, admin elevation (`/admin <password>`), permission checks |
| `server/console.go` | Local server management terminal (not for playing) |
| `server/upnp.go` | Auto UPnP port mapping on start, cleanup on shutdown |
| `server/pinggy.go` | Monitors Pinggy tunnel output, updates UI with connection info |
| `common/interfaces.go` | `Game` interface contract all games must implement |
| `common/types.go` | Shared types: `Point`, `Message`, `Player`, `TickMsg`, `MoveMsg` |
| `apps/` | JS app files — one active at a time, loaded via `/load` |
| `plugins/` | JS plugin files — multiple active simultaneously, loaded via `/plugin load` |

### The `Game` Interface
```go
Init() []tea.Cmd
Update(msg tea.Msg, playerID string) []tea.Cmd
View(playerID string, width, height int) string  // camera centered on player
GetCommands() []Command
```

### Apps and Plugins

**`apps/`** — interactive experiences loaded one at a time. An app owns the game viewport, status bar, and command bar. It can be a game, a shared canvas, a poll, anything terminal-interactive. Loaded with `/load <name>`, unloaded with `/unload`.

**`plugins/`** — passive extensions that run alongside any app (or in the lobby). Multiple plugins active simultaneously, persisting across app switches. Loaded with `/plugin load <name>`, unloaded with `/plugin unload <name>`.

Plugin hooks:
- `onChatMessage(msg)` — transform or filter chat; return `null` to drop the message. Plugins run in load order before the message reaches chat history.
- `onPlayerJoin(playerID, playerName)` / `onPlayerLeave(playerID)` — react to session events
- `commands()` — register additional slash commands
- `onLog(line)` — react to server log output (optional)

Both apps and plugins are single `.js` files in their respective folders. The Go binary is app/plugin-agnostic.

### Server Console

The server console (`server/console.go`) is its own Bubble Tea program running on the local terminal. It has two phases:

**Phase 1 — Boot sequence**

RedHat-style boot output: one line per step, status keyword right-aligned at the terminal edge on the same line when the step completes.

```
Starting SSH server on :23234 ............................... [ DONE ]
UPnP port mapping .............................................. [ DONE ]
Pinggy tunnel .................................................. [ FAILED ]
Detecting public IP ............................................ [ IGNORED ]
Generating invite script ....................................... [ DONE ]
```

Status tokens: `[ DONE ]`, `[ FAILED ]`, `[ IGNORED ]`, `[ SKIPPED ]`. After all steps finish, transition to the 2-panel console UI.

**Phase 2 — Console UI (2-panel)**

```
┌─────────────────────────────────────┐
│ Server log status bar (1 row)       │  e.g. "null-space | uptime 00:42 | game: none"
├─────────────────────────────────────┤
│                                     │
│ Server log (top half, scrollable)   │  log lines + game api.log() output
│                                     │  server-only; never sent to players
│                                     │
├─────────────────────────────────────┤
│ Chat status bar (1 row)             │  e.g. "3 players online"
├─────────────────────────────────────┤
│                                     │
│ Chat view (bottom half, scrollable) │  all player messages; private msgs
│                                     │  prefixed e.g. "[PM alice→bob] hello"
│                                     │
├─────────────────────────────────────┤
│ Admin input (1 row)                 │  '/' prefix = command; plain text = chat as admin
└─────────────────────────────────────┘
```

The chat panel mirrors exactly what players see, including private messages (admin sees all, prefixed with `[PM sender→recipient]`). The admin typing plain text without `/` appears in the player chat as a player named e.g. `[admin]`.

**Commands** are registered from two sources:
- **Built-in** — declared by the server framework (e.g. `/load <game>`, `/kick <player>`, `/who`, `/password`)
- **Game-registered** — each game declares additional commands via its API; these are added when the game loads and removed when it unloads

Each command declares two properties: whether it requires admin rights, and whether its first argument is a player name. When `firstArgIsPlayer` is true, the input field provides tab-completion against the current player list for that argument (e.g. `/msg <Tab>` cycles through connected players). The server console is always admin. SSH clients start as regular users and elevate via `/admin <password>`. The password is set at startup via `--password` and can be changed at runtime with `/password <new>` (admin only).

### Server Startup & Connection Strategy

On startup the server attempts network reachability in order, then generates a single PowerShell invite script that embeds all discovered addresses and tries them in the same order at connect time:

**Server-side startup sequence:**
1. **UPnP** — attempt IGD port mapping; record public IP + mapped port if successful
2. **Pinggy** — establish `ssh -R` reverse tunnel; parse the assigned `tcp://…` URL from its output
3. **Generate invite script** — embed all discovered addresses; display it in the status bar / console

**Client-side connection order (inside the generated script):**
1. `localhost:<port>` — same machine as the server
2. `<LAN IP>:<port>` — same local network
3. `<UPnP public IP>:<port>` — direct internet (requires UPnP to have succeeded)
4. Pinggy punch-through — ask Pinggy relay for `SERVER_IP`/`SERVER_PORT` punch info, attempt direct TCP
5. Pinggy relay — plain `ssh` through Pinggy tunnel (always works, highest latency)

The script tries each in order with a short `ConnectTimeout` and moves on if it fails. The client runs one PowerShell one-liner; no install required.

**Key files:** `server/upnp.go`, `server/pinggy.go`, `server/discovery.go`, `cmd/pinggy-helper/`

### Dependencies (charm.land v2 stack)
- `charm.land/bubbletea/v2` — TUI framework
- `charm.land/wish/v2` — SSH server (use `bubbletea.Middleware`, not deprecated wish middleware)
- `charm.land/lipgloss/v2` — terminal styling/layout
- `charm.land/bubbles/v2` — `textinput`, `viewport` components
- `github.com/huin/goupnp` — UPnP IGD

### SSH Input Handling (Windows gotcha)
Use `ssh.EmulatePty()` — **not** `ssh.AllocatePty()` — in all three call sites in `server/server.go`. On Windows, `AllocatePty` creates a real ConPTY and the `charmbracelet/ssh` library spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and every other keystroke is dropped. `EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader.

### Rendering Notes
- Use `\x1b[H` to reset cursor position and prevent flicker
- Only render the visible camera window — never the full map
- `server/kittystrip.go` strips Kitty protocol escape sequences from game output before writing to SSH clients that don't support them
