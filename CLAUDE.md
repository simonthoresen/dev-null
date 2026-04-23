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
make build              # compile to dist/dev-null-{server,client}.exe + dist/pinggy-helper.exe
make run-server         # server: SSH server + console GUI
make run-server-lan     # server: LAN-only (no UPnP, no public IP, no Pinggy)
make run-client         # client: connect to a running server
make run-client-local   # client: headless SSH server + graphical client
make clean              # remove compiled binaries from dist/

# Server — always runs as TUI (Bubble Tea in terminal).
go run ./cmd/dev-null-server --data-dir dist    # TUI mode (only mode)
go test ./...

ssh -p 23234 localhost   # connect via plain SSH (host plays this way too)

# Client — always GUI (Ebitengine graphical window).
go run ./cmd/dev-null-client --data-dir dist        # --data-dir needed in go-run mode (no bootstrap)
go run ./cmd/dev-null-client --host example.com --port 23234 --player alice
go run ./cmd/dev-null-client --game orbits          # send /game-load on connect
go run ./cmd/dev-null-client --resume orbits/autosave  # send /game-resume on connect

# Terminal client — use plain ssh (no binary needed).
ssh -p 23234 localhost

# Local mode — the script starts a headless server + connects the client.
# (--local is a script flag, not a binary flag)
# --no-gui launches plain ssh instead of the GUI binary.
.\start-client.ps1 --local
.\start-client.ps1 --local --game orbits
.\start-client.ps1 --local --resume orbits/autosave
.\start-client.ps1 --local --no-gui   # launches ssh instead of GUI binary

```

**Environment variables:**
- `DEV_NULL_LOG_FILE` — path to log file (overrides auto-log in data-dir/logs/)
- `DEV_NULL_LOG_LEVEL` — log level: debug/info/warn/error (default: info)
- `DEV_NULL_PINGGY_STATUS_FILE` — path to Pinggy status file (enables tunnel bridge UI)

## Data Directory Layout

Built binaries use two directories:

| Directory | Default | Purpose |
|-----------|---------|---------|
| **Install dir** | Exe directory | Binaries, bundled assets, `.bundle-manifest.json` (read-only, managed by installer) |
| **Data dir** | `%LOCALAPPDATA%/DevNull` | Working copies of assets, user-added content, saves, host keys (read-write) |

On first run or version upgrade, `datadir.Bootstrap()` copies bundled assets from install dir to data dir using a manifest-based merge:
- New bundled files are copied; updated bundled files are overwritten; user-added files are left alone.
- `.bundle-version` (written last) tracks the current build commit; if it matches, bootstrap is a no-op.
- Legacy data in the install dir (`state/`, host keys) is migrated once on first upgrade.

`--data-dir` overrides the data dir (skips bootstrap). `go run` (dev mode, `buildCommit=="dev"`) falls back to `"."` with no bootstrap, preserving the existing `--data-dir dist` development workflow.

## Architecture

**dev-null** is a "Multitenant Singleton" server over SSH.

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
| `internal/server/pinggy.go` | Pinggy tunnel status polling bridge |
| `internal/display/display.go` | Display backend interface: `Backend`, `BufferViewer`, options |
| `internal/display/terminal.go` | `TerminalBackend`: wraps `tea.Program` for TUI mode |
| `internal/display/ebiten.go` | `EbitenBackend`: Ebitengine window for GUI mode (drives Bubble Tea models) |
| `internal/display/cellrender.go` | Shared cell→pixel renderer: `DrawImageBuffer()`, `DefaultFontFace()` |
| `internal/display/input.go` | Ebitengine key/mouse → `tea.Msg` translation |
| `internal/chrome/model.go` | Per-player TUI model: struct, constructor, Init, Update |
| `internal/chrome/view.go` | Per-player rendering: lobby, playing, starting, ending |
| `internal/chrome/input.go` | Key/mouse handling: router glue, phase-action focus target, team panel clicks |
| `internal/input/router.go` | Pure-function input router: Action enum, Mode enum, EnterConsumer/EscConsumer |
| `internal/chrome/commands.go` | Per-player command dispatch: /plugin, /theme, /shader |
| `internal/chrome/menus.go` | Menu tree construction, sub-menu builders, font tag injection |
| `internal/console/console.go` | Server console TUI: model, view, log filtering |
| `internal/console/commands.go` | Console command dispatch: /plugin, /theme, /shader |
| `internal/localcmd/localcmd.go` | Shared /theme, /plugin, /shader command handlers (used by chrome and console) |
| `internal/console/sloghandler.go` | Console slog handler with render-path guard |
| `internal/domain/types.go` | Player, Message, GamePhase, Team, WidgetNode, tea messages |
| `internal/domain/interfaces.go` | Game, Command, Shader, MenuDef, DialogRequest interfaces |
| `internal/domain/clock.go` | Clock abstraction (RealClock, MockClock) |
| `internal/render/buffer.go` | ImageBuffer: 2D cell grid, ANSI parsing/serialization |
| `internal/render/charmap.go` | `CanvasCell` sentinel rune for Canvas HD viewport transparency |
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
| `internal/engine/rasterizer.go` | Software 3D rasterizer: depth buffer, barycentric triangle fill, Lambert helper |
| `internal/engine/canvas.go` | JSCanvas: 2D ops, gradients, 3D triangle primitives (fillTriangle3DLit) |
| `internal/engine/bindings.go` | JS global functions: log, chat, teams, gameOver, etc. |
| `internal/engine/shader.go` | Per-player JS shader post-processing |
| `internal/engine/plugin.go` | Per-player JS plugin system |
| `internal/engine/midi.go` | MIDI event types and helper constructors for JS bindings |
| `internal/engine/figlet.go` | Figlet ASCII art rendering |
| `internal/engine/gamelist.go` | Game discovery, path resolution, team range probing |
| `internal/network/` | UPnP, Pinggy status, public IP detection, downloads |
| `internal/datadir/datadir.go` | Data directory resolution, bootstrap (install dir → %LOCALAPPDATA%/DevNull) |
| `cmd/dev-null-server/` | Server entry point: boot sequence, console setup, signal handling |
| `cmd/dev-null-client/` | Graphical client: SSH + Ebitengine canvas rendering |
| `cmd/gen-manifest/` | Generates `.bundle-manifest.json` listing bundled assets with SHA-256 checksums |
| `cmd/pinggy-helper/` | Standalone helper that runs the Pinggy SSH tunnel |
| `internal/client/` | Client internals: SSH transport, ANSI parser, Ebitengine renderer |
| `internal/client/audio.go` | MIDI synthesizer: go-meltysynth SoundFont rendering, NoteOff scheduling |
| `dist/soundfonts/` | SoundFont (.sf2) files for MIDI synthesis: chiptune.sf2, gm.sf2 |
| `dist/start-server.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts pinggy-helper, then dev-null-server.exe |
| `dist/start-client.ps1` | PowerShell launcher: auto-updates from GitHub Releases, starts dev-null-client.exe |
| `install.ps1` | One-liner installer: downloads latest release zip, extracts to a folder, creates desktop shortcuts |
| `.github/workflows/release.yml` | CI: builds binaries, publishes rolling `latest` on main push, versioned releases on `v*` tags |

## Input Routing

All keyboard input flows through `internal/input/router.go`. The router is a pure function `Route(key, mode, focused) → Action` consumed by both `internal/chrome` (per-player) and `internal/console` (server console) — one code path, one source of truth.

**Framework-reserved keys** (games never see these via `onInput`):

| Key           | Action                                                     | Widget opt-in          |
|---------------|------------------------------------------------------------|------------------------|
| `ctrl+c`      | Quit                                                       | Never                  |
| `esc`         | Focused widget consumes if `WantsEsc()`, else activate menu | `input.EscConsumer`    |
| `enter`       | Focused widget consumes if `WantsEnter()`, else focus chat | `input.EnterConsumer`  |
| `pgup/pgdown` | Scroll chat                                                | Never                  |
| `tab/shift+tab` | Cycle focus                                              | `TabWanter` (existing) |

Ctrl+D is deliberately NOT reserved — games may bind it (some roguelikes use it for descend, drop, etc). Users can still exit via Ctrl+C or File → Exit.

**Two-step Esc/Enter contract.** A focused widget may claim Esc/Enter by implementing the consumer interface. `CommandInput`/`TextInput.WantsEsc()` returns true only when focused *with a non-empty draft*, so: first Esc clears the draft, second Esc activates the menu. Same shape for Enter: focused command-input submits; empty/unfocused → framework takes Enter to focus chat.

**Three modes.** Dialog > Menu > Desktop (top-down priority). Dialog and Menu are modal — the router sends everything there. On Desktop the router asks the focused widget first, then falls back to the framework action.

**No chord shortcuts.** Neither `F10`, `Alt+X`, nor menu-item `Ctrl+Q`-style hotkeys exist. The only way to open the menu from Desktop is Esc. Once the bar is focused, typing the ampersand-letter jumps to that menu (then to that item inside the dropdown) — so navigation is still fast, just without reserving Ctrl/Alt keys that a game might want. The `Hotkey` field on `MenuItemDef` has been removed; menu item activation is only via menu navigation or command-line commands. Ctrl+C still quits the session (router-reserved, not a menu binding).

**Phase button (Starting).** The `phaseReadyButton` is the effective focus target during `PhaseStarting` (it lives as a standalone field, not in a Window hierarchy). Enter → button.OnPress → `ReadyUp`. There is no ending phase or acknowledgement button — when a game signals game-over, the server posts the ranked results to chat as a system message and unloads directly (`server/lifecycle.go:checkGameOver`). The chat history persists in the lobby, so players can read and discuss results with PgUp/PgDn scroll.

**GameView as focus container.** When a game's `Layout` returns a `WidgetNode` tree with focusable children, the reconciled `GameWindow` is attached to `GameView.Inner`. Tab/Shift+Tab cycle inside the inner tree; when they wrap, `GameView` signals `WantTab`/`WantBackTab` and focus pops out to the command input. `currentFocus()` in chrome descends into `GameView.FocusedChild()` so `WantsEnter`/`WantsEsc` are consulted on the actual leaf widget (a game's focused TextInput can claim Enter and keep it out of the chat-focus path).

**Team rename is a modal dialog**, not an inline edit mode — pushed via `overlay.PushDialog` with `InputPrompt` + `InputValue`. Esc closes it via the dialog layer.

## UI Rule — No Bespoke Rendering

**All UI must build on the NC widget system** (`internal/widget/`). Never hand-draw ANSI strings for dialogs, overlays, or modals. Use `Window` + child Controls (`Label`, `Button`, `ListBox`, `TextInput`, `TextView`, etc.) and render via `RenderToBuf`. If a control doesn't exist, add it to `internal/widget/` — that extends the system rather than creating a parallel one. Dialogs are NC Windows rendered into a sub-buffer and blitted as overlays.

**Theme layer depth:** Layer 0 = main window (lobby/playing). Menus can only open from layer 0, so they always render at layer 1. Dialogs render at `1 + stackIndex` — the first dialog is layer 1 (same as menus, which close when a dialog opens), a dialog opened from a dialog is layer 2, etc. Use `OverlayState.DialogLayer()` to get the current layer.

## Sub-Menus

Menus support arbitrary-depth sub-menus via `MenuItemDef.SubItems`. Items with non-empty `SubItems` show "►" right-aligned and open a nested dropdown on right-arrow/Enter. State is tracked as a stack (`OverlayState.SubMenus []subMenuState`). `left` pops one level, `right`/`enter` pushes a new level or activates the leaf item. Toggle items stay open after activation.

Games, Themes, Plugins, Shaders, Synths, Fonts, and Invite are all sub-menus of the File menu. Admin-only "Add..." items appear at the top (with a separator) when `isAdmin()` is true. Console items have `OnDelete` callbacks for Del-key file deletion via confirmation dialogs. Saves remain a dialog (not a sub-menu).

All dropdowns (including sub-menus) scroll with a scrollbar when they exceed available screen height. The shared `renderMenuDropdown()` function handles rendering for both top-level dropdowns and sub-menus.

## Font Tags in Chat

Chat messages support `<font=name>text</font>` tags. The server expands them to figlet ASCII art via `expandFontTags()` in `server_chat.go` before broadcasting. Clicking a font in the Fonts sub-menu injects `<font=name></font>` at the cursor position in the chat input.

**Theme junction characters:** `BorderSet` has a complete set of junction characters for every combination of inner divider (`─`/`│`) meeting outer border (`═`/`║`) or another inner divider. The outer-to-inner junctions (`CrossL/R/T/B`) use mixed double/single characters (e.g. `╟`, `╧`). The inner-to-inner T-junctions use all-thin characters: `InnerCrossT` (`┬`), `InnerCrossB` (`┴`), `InnerCrossL` (`├`), `InnerCrossR` (`┤`). The inner cross is `CrossX` (`┼`). The post-processing in `Window.RenderToBuf` and `Panel.Render` automatically selects the right character based on whether each divider edge touches the outer border or an interior divider.

## Concurrency — Lock Ordering

Two primary mutexes protect shared state:

| Mutex | Type | Location | Protects |
|-------|------|----------|----------|
| `CentralState.mu` | RWMutex | `internal/state/state.go` | Players, teams, game phase, chat history, network info |
| `Runtime.mu` | Mutex | `internal/engine/runtime.go` | Goja JS VM and all JS callback execution |

**Invariant:** `Runtime` must **never** acquire `CentralState.mu`. This is enforced structurally — `Runtime` has no reference to `CentralState`. Data flows through:
- **Teams:** Server builds a cache (`buildTeamsCache`) and pushes it via `SetTeamsCache()`. JS `teams()` reads the local cache.
- **Chat:** JS `chat()`/`chatPlayer()` send on a buffered channel; a server goroutine drains it and calls `broadcastChat()`.

**Callers** (`internal/server/server.go`, `internal/chrome/model.go`) must release `state.mu` **before** calling any `Runtime` Game method (`Load`, `Begin`, `Update`, `Render`, `OnInput`, etc.). All existing call sites follow this pattern — verify any new ones do too.

Other mutexes (`programsMu`, `sessionsMu`, `consoleProgramMu`, `commandRegistry.mu`, `lastUpdateMu`) are leaf locks — they don't call into JS or acquire `state.mu`.

**`lastUpdateMu`** protects the `Server.lastUpdate` field, which is written from `startingTimer()`, `resumeGame()`, and read/written in `runTicker()`. All access must go through this mutex.

## Rendering Model

Game rendering has two orthogonal settings, both controlled via the always-visible Graphics menu:

**Graphics mode** (`domain.GraphicsMode`): how the viewport is displayed.

| Mode | Requires | Description |
|---|---|---|
| **Ascii** (`ModeAscii`) | — | Game's text-based `renderAscii()` |
| **Blocks** (`ModeBlocks`) | `renderCanvas` | Canvas→Unicode quadrant blocks (▖▗▘▙) |
| **Pixels** (`ModePixels`) | `renderCanvas` + GUI client | Canvas at full window pixel resolution |

**Render location** (`renderLocal` bool): where the JS runs.

| | Remote (server) | Local (client) |
|---|---|---|
| Ascii | SSH + GUI | — |
| Blocks | SSH + GUI | GUI only (**default for GUI**) |
| Pixels | — | GUI only (always local) |

SSH clients are always remote. GUI clients default to local. The "Render locally" checkbox in the Graphics menu toggles location (disabled for SSH) and is the master switch: Pixels requires it. If the user prefers Pixels but has Render locally off, the mode degrades to Blocks. Degradation chain: Pixels → Blocks → Ascii.

**Local rendering and `Game.state`**: Local modes re-execute game JS on the client. The client never calls `update()` — only `renderCanvas()`. The engine auto-injects `Game.state._t` (elapsed seconds since `begin()`) after each `Update()`, so canvas games always have the current time. Games that need additional render state must populate `Game.state` in `update()` — module-level variables are only updated on the server.

**No charmap/spritesheet system** — removed. Games that want custom graphics use canvas rendering.

Preferences are persisted to `~/.dev-null/client.txt` as `/render-ascii`, `/render-pixels` (Blocks is default, omitted), and `/render-remote` or `/render-local` (only if non-default for the client type).

## Render Tests — Golden Files

`internal/rendertest/` contains full-frame render tests for both the server console and the player chrome view. All golden files live flat in `internal/rendertest/testdata/` as `<scenario>_console.txt` and `<scenario>_chrome.txt`.

- **Curated eval set**: edit `scenarios_test.go` to add/change test states.
- **Regenerate golden files** after a layout or content change:
  ```bash
  go test ./internal/rendertest/ -update
  ```
- Each scenario produces two golden files (`<name>_console.txt`, `<name>_chrome.txt`).
- `<name>_chrome.txt` is verified against **8 unit sub-tests** (4 execution contexts × 2 color modes) and **4 integration sub-tests** (real SSH connections) — all compared against the same golden file.
- Chrome output is normalized before comparison: ANSI stripped, trailing spaces trimmed per line, trailing blank lines dropped, and the lobby status bar line (timestamp + uptime) replaced with fixed placeholders so golden files are stable across runs.
- `time.Now()` in View() methods uses `m.api.Clock().Now()` so tests inject a fixed time via `domain.MockClock`.
- Scenarios marked `noIntegration: true` are only tested by unit tests. Use this for: playing/starting (late joiners stay in lobby) and menu/dialog scenarios (integration harness can't send keystrokes).
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
- `github.com/sinshu/go-meltysynth` — SoundFont MIDI synthesizer (pure Go)

## Detailed References

Read these as needed when working on specific areas:

- [`docs/claude/game-lifecycle.md`](docs/claude/game-lifecycle.md) — lifecycle phases, suspend/resume, teams, Game.state, game over
- [`docs/claude/game-api.md`](docs/claude/game-api.md) — Game interface (Go), JS game hooks & globals, Command struct, Message type
- [`docs/claude/game-contract-v2.md`](docs/claude/game-contract-v2.md) — **planned** v2 contract: ctx/events/render separation, state diff transport, migration steps
- [`docs/claude/ui-layout.md`](docs/claude/ui-layout.md) — Screen layout (lobby/playing/console), themes, widget reconciler
- [`docs/claude/extensions.md`](docs/claude/extensions.md) — plugins, shaders, canvas rendering, OSC protocol
- [`docs/claude/server-console.md`](docs/claude/server-console.md) — boot sequence, console UI, admin auth
- [`docs/claude/networking.md`](docs/claude/networking.md) — release/distribution, connection strategy, invite token format, SSH PTY gotcha, init files
- [`API-REFERENCE.md`](API-REFERENCE.md) — full JS game developer documentation
