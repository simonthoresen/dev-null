# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **For Claude:** This file is the portable memory for this project. Whenever you make a change, discover a gotcha, or establish a pattern or decision, **update this file before committing**. It is the single source of truth that survives new clones, new machines, and new sessions. Keep it accurate and concise — do not let it drift from the actual code.
>
> **For Claude:** When you have completed a task or a logical unit of work, **commit and push to git**. Don't wait to be asked.

## Project Goal

A framework for hosting terminal-based multiplayer **games** over SSH. **Only the server operator needs to install anything.** Players connect with a plain `ssh` command — no client install required.

Games are written in JavaScript (goja) and loaded at runtime from `dist/games/`. The server binary itself is game-agnostic.

## Commands

```bash
make build              # compile to dist/null-space-{server,client}.exe + dist/pinggy-helper.exe
make run-server         # server: SSH server + console TUI
make run-server-lan     # server: LAN-only (no UPnP, no public IP, no Pinggy)
make run-server-local   # server: headless SSH server + terminal client
make run-client         # client: connect to a running server
make run-client-local   # client: headless SSH server + graphical client
make clean              # remove compiled binaries from dist/

go run ./cmd/null-space-server --data-dir dist   # equivalent to make run, add --password etc.
go test ./...

ssh -p 23234 localhost   # connect via plain SSH (host plays this way too)

# Graphical client — SSH + sprite rendering for charmap games.
go run ./cmd/null-space-client
go run ./cmd/null-space-client --host example.com --port 23234 --player alice

# Local mode (server) — headless SSH server + terminal client in one process.
# Exercises the full SSH pipeline; you see what `ssh -p 23234 localhost` would show.
go run ./cmd/null-space-server --local --data-dir dist
go run ./cmd/null-space-server --local --data-dir dist --player alice
go run ./cmd/null-space-server --local --data-dir dist --game orbits
go run ./cmd/null-space-server --local --data-dir dist --resume orbits/autosave

# Local mode (client) — headless SSH server + graphical client in one process.
# Exercises the full SSH pipeline with the Ebitengine renderer.
go run ./cmd/null-space-client --local --data-dir dist
go run ./cmd/null-space-client --local --data-dir dist --player alice --game orbits
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
- **Central ticker** (configurable interval, default 100ms via `--tick-interval`) sends `TickMsg` to all programs simultaneously → synchronized real-time rendering
- **The server terminal is management-only.** The host joins as a player via SSH like everyone else.

### Key Packages

| Package | Role |
|---------|------|
| `internal/server/server.go` | SSH server setup, session lifecycle, tick broadcast |
| `internal/server/lifecycle.go` | Game load/unload, phase transitions, teams cache |
| `internal/server/commands.go` | Slash command registry and dispatch |
| `internal/server/local.go` | `--local` mode: headless SSH server + terminal SSH pipe to stdin/stdout |
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
| `dist/start-server.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts pinggy-helper, then null-space-server.exe |
| `dist/start-client.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts null-space-client.exe |
| `install.ps1` | One-liner installer: downloads latest release zip, extracts to a folder, creates desktop shortcuts |
| `.github/workflows/release.yml` | CI: builds binaries and publishes rolling `latest` release on every push to main |

## UI Rule — No Bespoke Rendering

**All UI must build on the NC widget system** (`internal/widget/`). Never hand-draw ANSI strings for dialogs, overlays, or modals. Use `Window` + child Controls (`Label`, `Button`, `ListBox`, `TextInput`, `TextView`, etc.) and render via `RenderToBuf`. If a control doesn't exist, add it to `internal/widget/` — that extends the system rather than creating a parallel one. Dialogs are NC Windows rendered into a sub-buffer and blitted as overlays.

**Theme layer depth:** Layer 0 = main window (lobby/playing). Menus can only open from layer 0, so they always render at layer 1. Dialogs render at `1 + stackIndex` — the first dialog is layer 1 (same as menus, which close when a dialog opens), a dialog opened from a dialog is layer 2, etc. Use `OverlayState.DialogLayer()` to get the current layer.

**Theme junction characters:** `BorderSet` has a complete set of junction characters for every combination of inner divider (`─`/`│`) meeting outer border (`═`/`║`) or another inner divider. The outer-to-inner junctions (`CrossL/R/T/B`) use mixed double/single characters (e.g. `╟`, `╧`). The inner-to-inner T-junctions use all-thin characters: `InnerCrossT` (`┬`), `InnerCrossB` (`┴`), `InnerCrossL` (`├`), `InnerCrossR` (`┤`). The inner cross is `CrossX` (`┼`). The post-processing in `Window.RenderToBuf` and `Panel.Render` automatically selects the right character based on whether each divider edge touches the outer border or an interior divider.

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

Other mutexes (`programsMu`, `sessionsMu`, `consoleProgramMu`, `commandRegistry.mu`, `lastUpdateMu`) are leaf locks — they don't call into JS or acquire `state.mu`.

**`lastUpdateMu`** protects the `Server.lastUpdate` field, which is written from `splashTimer()`, `resumeGame()`, and read/written in `runTicker()`. All access must go through this mutex.

## Render Tests — Golden Files

`internal/rendertest/` contains full-frame render tests for both the server console and the player chrome view. All golden files live flat in `internal/rendertest/testdata/golden/` as `<scenario>_console.txt` and `<scenario>_chrome.txt`.

- **Curated eval set**: edit `scenarios_test.go` to add/change test states.
- **Regenerate golden files** after a layout or content change:
  ```bash
  go test ./internal/rendertest/ -update
  ```
- Each scenario produces two golden files (`<name>_console.txt`, `<name>_chrome.txt`).
- `<name>_chrome.txt` is verified against **8 unit sub-tests** (4 execution contexts × 2 color modes) and **4 integration sub-tests** (real SSH connections) — all compared against the same golden file.
- Chrome output is normalized before comparison: ANSI stripped, trailing spaces trimmed per line, trailing blank lines dropped, and the lobby status bar line (timestamp + uptime) replaced with fixed placeholders so golden files are stable across runs.
- `time.Now()` in View() methods uses `m.api.Clock().Now()` so tests inject a fixed time via `domain.MockClock`.
- Scenarios marked `noIntegration: true` are only tested by unit tests. Use this for: playing/splash (late joiners stay in lobby) and menu/dialog scenarios (integration harness can't send keystrokes).
- `setup()` must **never** add the scenario's `playerID` to state. `renderChrome` adds them automatically (simulating the server's player-join path), keeping unit and integration outputs identical.

## Slog Feedback Loop Guard

**Never add `slog` calls to `View()` or `Render()` methods.** The console routes slog → channel → Update → View, so any slog call in the render path creates an infinite feedback loop (high CPU, starved keyboard events).

The `consoleSlogHandler` has a runtime guard (`isRenderPath()`) that inspects the call stack and suppresses console-channel sends from inside `.View` or `.Render` methods. This is a safety net — the primary rule is still: don't log from render paths. `TestNoSlogInRenderPath` scans render-path source files for slog calls; `TestSlogBlockedInRenderPath` verifies the runtime guard.

## Dependencies (charm.land v2 stack)
- `charm.land/bubbletea/v2` — TUI framework
- `charm.land/wish/v2` — SSH server (use `bubbletea.Middleware`, not deprecated wish middleware)
- `charm.land/lipgloss/v2` — terminal styling/layout
- `charm.land/bubbles/v2` — `textinput`, `viewport` components
- `github.com/charmbracelet/x/term` — terminal size detection
- `github.com/huin/goupnp` — UPnP IGD
- `github.com/dop251/goja` — JavaScript runtime for games

## Detailed References

Read these as needed when working on specific areas:

- [`docs/claude/game-lifecycle.md`](docs/claude/game-lifecycle.md) — lifecycle phases, suspend/resume, teams, Game.state, game over
- [`docs/claude/game-api.md`](docs/claude/game-api.md) — Game interface (Go), JS game hooks & globals, Command struct, Message type
- [`docs/claude/ui-layout.md`](docs/claude/ui-layout.md) — Screen layout (lobby/playing/console), themes, widget reconciler
- [`docs/claude/extensions.md`](docs/claude/extensions.md) — plugins, shaders, charmaps, canvas rendering, OSC protocol
- [`docs/claude/server-console.md`](docs/claude/server-console.md) — boot sequence, console UI, admin auth
- [`docs/claude/networking.md`](docs/claude/networking.md) — release/distribution, connection strategy, invite token format, SSH PTY gotcha, init files
- [`API-REFERENCE.md`](API-REFERENCE.md) — full JS game developer documentation
