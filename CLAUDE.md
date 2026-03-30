# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **For Claude:** This file is the portable memory for this project. Whenever you make a change, discover a gotcha, or establish a pattern or decision, **update this file before committing**. It is the single source of truth that survives new clones, new machines, and new sessions. Keep it accurate and concise — do not let it drift from the actual code.
>
> **For Claude:** When you have completed a task or a logical unit of work, **commit and push to git**. Don't wait to be asked.

## Project Goal

A framework for hosting terminal-based multiplayer **apps** (games, canvases, polls, anything terminal-interactive) over SSH. **Only the server operator needs to install anything.** Players connect with a plain `ssh` command — no client install required.

Apps and plugins are written in JavaScript (goja) and loaded at runtime from `dist/apps/` and `dist/plugins/`. The server binary itself is app/plugin-agnostic.

## Commands

```bash
make build          # compile to dist/null-space.exe + dist/pinggy-helper.exe
make run            # go run with --data-dir dist (dev shortcut)
make clean          # remove compiled binaries from dist/

go run ./cmd/null-space --data-dir dist   # equivalent to make run, add --password etc.
go test ./...

ssh -p 23234 localhost   # connect as a client (host plays this way too)
```

**Environment variables:**
- `NULL_SPACE_LOG_FILE` — path to log file (default: discard)
- `NULL_SPACE_LOG_LEVEL` — log level: debug/info/warn/error (default: info)
- `NULL_SPACE_PINGGY_STATUS_FILE` — path to Pinggy status file (enables tunnel bridge UI)

## Architecture

**null-space** is a "Multitenant Singleton" server over SSH.

### Core Pattern
- **One app singleton** runs on the server (`CentralState.ActiveApp`)
- **One Bubble Tea `Program` per SSH session**, all sharing the same app state
- **Central 100ms ticker** sends `TickMsg` to all programs simultaneously → synchronized real-time rendering
- **The server terminal is management-only.** The host joins as a player via SSH like everyone else.

### Lobby vs. In-App
Players start in the **server lobby** — chat only, no app running. An admin uses `/app load <name>` to load an app from `dist/apps/`. This transitions all players into the app view.

### UI Layout

**Lobby (no app loaded):**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — framework      │  e.g. "null-space | 3 players online | 00:42 ⠹"
├─────────────────────────────────────┤
│                                     │
│ Chat (fills remaining rows)         │
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row) — dual-purpose  │  idle: "[Enter] to chat  /help for commands"
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```

**In-app:**
```
┌─────────────────────────────────────┐
│ Status bar (1 row) — app-owned      │  App.StatusBar(playerID) → "HP: 100  Score: 4200 ⠹"
├─────────────────────────────────────┤
│                                     │
│ App viewport (W × W*9/16 rows)      │  App.View(playerID, W, H)
│                                     │
├─────────────────────────────────────┤
│                                     │
│ Chat (remaining rows, min 5)        │  shared chat history
│                                     │
├─────────────────────────────────────┤
│ Command bar (1 row) — dual-purpose  │  idle: App.CommandBar(playerID) → "[↑↓] Move"
└─────────────────────────────────────┘  on Enter: text input; submit/Esc: reverts
```

**Braille spinner:** the last character of every status bar row is reserved for a Braille spinner — a live indicator that the server is running. Sequence: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, advances once per second (every 10 ticks at 100ms). Status bar content must never overwrite it.

**Viewport sizing:** `gameH = W * 9 / 16`. Chat gets the remaining rows (min 5). Command bar is always 1 row.

### Key Packages

| Package | Role |
|---------|------|
| `server/server.go` | Wish SSH server setup, session lifecycle, tick broadcast, `App` orchestrator |
| `server/chrome.go` | Per-user `chromeModel`: renders lobby or app chrome depending on state |
| `server/state.go` | `CentralState`: players map, chat history (max 50), active app, plugins |
| `server/commands.go` | `/` command registry, tab completion, permission checks |
| `server/console.go` | Local server management terminal (not for playing) |
| `server/runtime.go` | JS app runtime (goja): loads `dist/apps/*.js`, implements `common.App` |
| `server/plugin.go` | JS plugin runtime (goja): loads `dist/plugins/*.js`, implements `common.Plugin` |
| `server/upnp.go` | Auto UPnP port mapping on start, cleanup on shutdown |
| `server/pinggy.go` | Polls Pinggy status file, updates `state.Net.PinggyURL` |
| `common/interfaces.go` | `App` and `Plugin` interface contracts, `Command` struct |
| `common/types.go` | Shared types: `Message`, `Player`, `TickMsg`, `ChatMsg`, etc. |
| `cmd/null-space/` | Entry point: boot sequence, console setup, signal handling |
| `cmd/pinggy-helper/` | Standalone helper that runs the Pinggy SSH tunnel |
| `dist/start.ps1` | PowerShell launcher: starts pinggy-helper, then null-space.exe |

### The `App` Interface (`common/interfaces.go`)
```go
type App interface {
    OnPlayerJoin(playerID, playerName string)
    OnPlayerLeave(playerID string)
    OnInput(playerID, key string)
    View(playerID string, width, height int) string
    StatusBar(playerID string) string   // content for top status bar (spinner appended by framework)
    CommandBar(playerID string) string  // idle hint in command bar
    Commands() []Command
    Unload()
}
```

### The `Plugin` Interface
```go
type Plugin interface {
    OnChatMessage(msg *Message) *Message  // return nil to drop; runs in load order before chat history
    OnPlayerJoin(playerID, playerName string)
    OnPlayerLeave(playerID string)
    Commands() []Command
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

`AppName` in `CentralState` stores the bare name (e.g. `example`), not the full file path. `loadApp` strips the directory and `.js` extension. Commands that broadcast app load/unload events should use the bare name too — `loadApp` already broadcasts `"App loaded: <name>"` to chat, so command handlers must not send a redundant reply.

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

### Apps and Plugins (JS)

Both are single `.js` files in `dist/apps/` or `dist/plugins/`. Loaded at runtime via `/app load <name>` / `/plugin load <name>`.

**App** — exports a global `Game` object with hooks `onPlayerJoin`, `onPlayerLeave`, `onInput`, `view`, `statusBar`, `commandBar`. Loaded one at a time; owns the viewport.

**Plugin** — exports a global `Plugin` object with hooks `onChatMessage`, `onPlayerJoin`, `onPlayerLeave`, `commands`. Multiple active simultaneously; persistent across app switches.

The chat pipeline runs all active plugin `onChatMessage` hooks (in load order) before committing a message to history. Return `null` to drop.

---

## Server Console

`server/console.go` is its own Bubble Tea program on the local terminal. Two phases:

### Phase 1 — Boot sequence

Each step is printed in two passes:
1. **Before** the operation: `label ...................` (dots to fill line, no status, no newline)
2. **After** the operation: `\r` overwrites the line with `label ........ [ STATUS ]` right-aligned

Status tokens are always **11 chars wide** with the text centered:
```
[  DONE   ]   (DONE = 4 chars, pad 3: 1 left, 2 right)
[ FAILED  ]   (FAILED = 6 chars, pad 1: 0 left, 1 right)
[ IGNORED ]   (IGNORED = 7 chars, no padding)
[ SKIPPED ]   (SKIPPED = 7 chars, no padding)
```

Implementation: `startBootStep(label)` / `finishBootStep(status)` in `cmd/null-space/main.go`. Terminal width via `github.com/charmbracelet/x/term`. The PS1 script has matching `Write-BootStepStart` / `Write-BootStepEnd` helpers.

Startup sequence (PS1 steps first, then Go binary):
```
Pinggy helper .............................................. [  DONE   ]  ← start.ps1
SSH server ................................................. [  DONE   ]  ← Go
UPnP port mapping .......................................... [ IGNORED ]
Public IP detection ........................................ [ IGNORED ]
Pinggy tunnel .............................................. [  DONE   ]
Generating invite script ................................... [  DONE   ]

  <invite command>

  (console UI runs)

Stopping SSH server ........................................ [  DONE   ]  ← Go shutdown
Stopping Pinggy helper ..................................... [  DONE   ]  ← PS1 finally block
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
- `github.com/dop251/goja` — JavaScript runtime for apps/plugins

---

## SSH Input Handling (Windows gotcha)

Use `ssh.EmulatePty()` — **not** `ssh.AllocatePty()` — in all three call sites in `server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `server/server.go` to find all three call sites.
