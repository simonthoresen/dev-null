# dev-null API Reference

This document explains how to write games for dev-null. Games are plain JavaScript files (ES5-compatible, no modules). Drop your file in `dist/games/`, or share it via URL — no build step required.

The contract is summarized in one page at [`docs/claude/game-contract.md`](docs/claude/game-contract.md); this file is the longer tutorial.

---

## Quick orientation

| Concept | What it means |
|---------|---------------|
| **Game** | One active at a time; owns the viewport, status bar, and command bar |
| **Lobby** | The state when no game is loaded; only chat is visible |
| **Viewport** | The rectangular region your game renders into (below the game status bar, above chat) |
| **`state`** | The single gameplay object. Passed as a parameter to every hook; the framework marshals and transports it. |
| **`ctx`** | The server-only side-effects handle (chat, sound, MIDI, gameOver, …). Never passed to render. |
| **`me`** | The player that a render or UI call is "for". Framework-resolved; render receives the resolved object, not an ID string. |
| **`events`** | The flat, ordered per-tick event list passed to `update`. Inputs, joins, leaves, commands, and the always-present `tick` event arrive here. |

---

## Loading a game

```
# From a local file in dist/games/
/game load example

# From a folder-based game in dist/games/nethack/main.js
/game load nethack

# From a URL (GitHub blob or any HTTPS .js URL)
/game load https://github.com/you/repo/blob/main/mygame.js

# From a zip URL (downloaded, extracted to dist/games/<name>/)
/game load https://example.com/mygame.zip

# Local mode (no SSH server)
dev-null --local --game example
dev-null --local --game https://github.com/you/repo/blob/main/mygame.js
```

URL-loaded `.js` files are cached in `dist/games/.cache/`. Re-loading the same URL always fetches the latest version. Zip files are extracted to `dist/games/<name>/` and must contain a `main.js` at the root.

---

## Multi-file games

For larger games, use a folder structure with `include()`:

```
dist/games/mygame/
  main.js        <- entry point (must define the Game object)
  monsters.js    <- included via include("monsters")
  dungeon.js     <- included via include("dungeon")
```

Load with `/game load mygame`. The framework detects `games/mygame/main.js` automatically.

`include("filename")` evaluates a JS file from the same directory. The `.js` extension is added if omitted. Each file is included at most once (idempotent — safe to call multiple times). All included files share the same global scope, so functions and variables defined in one file are accessible in others.

To distribute a multi-file game as a URL, package it as a `.zip` file with `main.js` at the root:

```
/game load https://example.com/mygame.zip
```

The zip is extracted to `dist/games/mygame/` and then loaded normally.

---

## Writing a game

A game file must define a global `Game` object. `init` and at least one of `renderAscii`/`renderCanvas` are required; every other hook is optional.

```js
var Game = {

    // --- Properties ---

    // Display name shown in the menu bar. If omitted, the filename stem is used.
    gameName: "My Awesome Game",

    // Supported team count range. The framework blocks loading outside the range.
    // Omit to allow any number of teams.
    teamRange: { min: 2, max: 4 },

    // --- Lifecycle (server-only, receive ctx) ---

    // Called once on game load. Returns the initial state object; the framework
    // installs it as Game.state. Mandatory. Teams are NOT yet assembled here —
    // state.teams is first valid in begin().
    //
    // savedState is the value previously returned by unload(), or null on the
    // very first load of this game. Seed persistent fields (high scores,
    // unlocks) from it.
    init: function(ctx, savedState) {
        return {
            players: {},
            score:   0,
            elapsed: 0,
            highScore: (savedState && savedState.highScore) || 0
        };
    },

    // Called after teams are assembled and the starting screen closes. Use this
    // to spawn per-team records, seed initial positions, etc. state.teams is
    // valid here and every tick after.
    begin: function(state, ctx) {
        var t = state.teams || [];
        // ... spawn players from teams ...
    },

    // Called once per server tick. Drains every event queued since the last
    // tick and runs all gameplay logic. This is the ONLY place state is
    // meant to mutate.
    update: function(state, dt, events, ctx) {
        state.elapsed += dt;
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "input")  { /* handle input */ }
            if (e.type === "join")   { /* add player to state */ }
            if (e.type === "leave")  { /* remove player */ }
            if (e.type === "tick")   { /* exactly one per update */ }
            if (e.type === "command"){ /* custom slash command */ }
        }
    },

    // Called when the game transitions to game-over, before the results
    // screen is shown. Optional cleanup site.
    end: function(state, ctx) { },

    // --- Render (pure, no ctx) ---

    // Renders the game viewport as characters. Runs on the server for SSH
    // clients and on the GUI client itself for local ascii/blocks rendering.
    // NEVER receives ctx; side-effecty calls throw a TypeError.
    renderAscii: function(state, me, cells) {
        cells.writeString(0, 0, "Hello, " + me.name, "#FFFFFF", null);
    },

    // Renders the viewport as pixels. Runs client-side when the player is in
    // Blocks or Pixels mode. Also never receives ctx.
    renderCanvas: function(state, me, canvas) {
        canvas.fillRect(0, 0, canvas.width, canvas.height, "#000");
    },

    // --- Optional chrome (pure, no ctx) ---

    statusBar:  function(state, me) { return "HP: 100  Score: " + state.score; },
    commandBar: function(state, me) { return "[↑↓←→] Move  [Enter] Chat"; },

    // --- Optional: custom me resolution ---

    // By default the framework sets me = state.players[playerID]. Games that
    // store players under a different key provide this hint. Returning null
    // makes the framework draw a "connecting…" splash and skip render.
    resolveMe: function(state, playerID) {
        return state.entities[playerID] || null;
    },

    // --- Optional persistence (see "State persistence") ---

    unload:  function()             { return { /* persistent data */ }; },
    suspend: function()             { return { /* session snapshot */ }; },
    resume:  function(sessionState) { /* restore from suspend() */ }
};
```

### Minimal working example

```js
var Game = {
    gameName: "Dots",
    teamRange: { min: 1, max: 4 },

    init: function(ctx) {
        return { players: {} };
    },

    begin: function(state, ctx) {
        var t = state.teams || [];
        for (var i = 0; i < t.length; i++) {
            for (var j = 0; j < t[i].players.length; j++) {
                var p = t[i].players[j];
                state.players[p.id] = { id: p.id, name: p.name, x: 10, y: 5 };
            }
        }
    },

    update: function(state, dt, events, ctx) {
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "leave") { delete state.players[e.playerID]; continue; }
            if (e.type !== "input") continue;
            var p = state.players[e.playerID];
            if (!p) continue;
            if (e.key === "up")    p.y = Math.max(0, p.y - 1);
            if (e.key === "down")  p.y++;
            if (e.key === "left")  p.x = Math.max(0, p.x - 1);
            if (e.key === "right") p.x++;
        }
    },

    renderAscii: function(state, me, cells) {
        for (var y = 0; y < cells.height; y++) {
            for (var x = 0; x < cells.width; x++) {
                var ch = ".";
                for (var id in state.players) {
                    var p = state.players[id];
                    if (p.x === x && p.y === y) {
                        ch = (id === me.id) ? "@" : "O";
                        break;
                    }
                }
                cells.setChar(x, y, ch, null, null);
            }
        }
    },

    statusBar: function(state, me) {
        var p = state.players[me.id];
        return p ? "pos: (" + p.x + "," + p.y + ")" : "";
    }
};
```

The framework renders the starting screen (figlet game name) and the ending screen (figlet "GAME OVER" + ranked results). Games don't provide their own.

---

## `ctx` — server-only capabilities

`ctx` is the handle that gives server-side hooks (`init`, `begin`, `update`, `end`) access to side-effecty framework features. **Render hooks never receive `ctx`** — calling any method on it from render throws a TypeError. That's deliberate: it prevents impurity in render paths from silently diverging between the server and the locally-rendering client.

| Method | Purpose |
|---|---|
| `ctx.log(msg)` | Write to the server log panel (debug). |
| `ctx.chat(msg)` | Broadcast a system chat message. |
| `ctx.chatPlayer(pid, msg)` | Direct-message one player. |
| `ctx.playSound(file, opts)` | Play an audio asset. Options: `{ loop: true, alt: "text" }`. |
| `ctx.stopSound(file)` | Stop playback. Call with no argument to stop all sounds. |
| `ctx.midiNote(ch, note, vel, durMs)` | Broadcast a MIDI note. Duration 0 = NoteOn only. |
| `ctx.midiNotePlayer(pid, ch, note, vel, durMs)` | Direct-to-one-player MIDI note. |
| `ctx.midiProgram(ch, program)` | Change the instrument on a MIDI channel (GM 0-127). |
| `ctx.midiProgramPlayer(pid, ch, program)` | Per-player program change. |
| `ctx.midiCC(ch, controller, value)` | Send a MIDI Control Change (7=volume, 10=pan, 64=sustain). |
| `ctx.teams()` | Snapshot of current teams (read-only; `state.teams` is the usual way to read). |
| `ctx.gameOver(results)` | Signal end-of-game. `results` is a ranked array of `{ name, result }`. |
| `ctx.showDialog(pid, opts)` | Open a modal dialog on one client. See "Dialogs" below. |
| `ctx.registerCommand(spec)` | Register a slash-command handler. See "Custom commands". |
| `ctx.now()` | Server time as epoch milliseconds (from the framework's mockable central clock). |

### Dialogs

```js
ctx.showDialog(playerID, {
    title: "Confirm",
    message: "Restart the game?",
    buttons: ["Yes", "No"],
    onClose: function(button) {
        if (button === "Yes") { /* … */ }
    }
});
```

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Dialog title bar text. |
| `message` | string | Body text. `\n` creates line breaks. |
| `buttons` | array | Button labels. Defaults to `["OK"]`. |
| `onClose` | function(button) | Called with the label pressed, or `""` if dismissed with Esc. |

Navigation: Tab / Left / Right cycle buttons. Enter or Space activates. Esc calls `onClose("")`.

---

## `events` — per-tick input

`update(state, dt, events, ctx)` receives every event that happened since the previous tick as a flat, ordered array. Each event is a plain object with a `type`:

```js
{ type: "input",   playerID, key }                 // player pressed a key in game mode
{ type: "join",    playerID, playerName }          // player joined the running game
{ type: "leave",   playerID }                      // player disconnected
{ type: "tick" }                                   // always present exactly once per update
{ type: "command", playerID, name, args }          // registered via ctx.registerCommand
```

Iterate the list in order. One `update` call per tick; events since the previous tick arrive batched.

### Key strings for `input` events

Keys are passed as Bubble Tea key strings. Common values:

| Key | String |
|-----|--------|
| Arrow keys | `"up"` `"down"` `"left"` `"right"` |
| Enter | `"enter"` |
| Escape | `"esc"` |
| Space | `" "` (or `"space"`) |
| Backspace | `"backspace"` |
| Tab | `"tab"` |
| Page Up/Down | `"pgup"` `"pgdown"` |
| Home / End | `"home"` `"end"` |
| Function keys | `"f1"` … `"f12"` |
| Letters | `"a"` … `"z"` (lowercase) `"A"` … `"Z"` (uppercase/shift) |
| Digits | `"0"` … `"9"` |
| Ctrl combos | `"ctrl+a"` … `"ctrl+z"` |

> `input` events are only delivered while the player is in game mode (not chatting). Players enter chat mode by pressing Enter in the command bar; they return to game mode by submitting or pressing Esc.

---

## `me` and `resolveMe`

Render and UI hooks never see a raw `playerID` string — they receive a resolved `me` object. By default the framework sets `me = state.players[playerID]`. If the game keeps per-player data under a different key, define `resolveMe`:

```js
Game.resolveMe = function(state, playerID) {
    var teamIdx = state.playerTeams[playerID];
    return { id: playerID, teamIdx: teamIdx, camera: state.cameras[teamIdx] };
};
```

If `resolveMe` returns null, the framework draws a "connecting…" splash into the viewport and does not invoke render. That keeps games out of the awkward "mid-join with no `me` yet" state.

---

## `cells` — the ASCII render surface

`cells` is the object passed to `renderAscii(state, me, cells)`. It's a view into the player's current viewport; attributes it carries:

| Member | Description |
|--------|-------------|
| `cells.width` / `cells.height` | Dimensions of the game viewport (in characters). |
| `cells.setChar(x, y, ch, fg, bg, attr?)` | Set one character. `fg`/`bg` are `"#RRGGBB"` or `null` (default). `attr` is a bitmask (see below). |
| `cells.writeString(x, y, text, fg, bg, attr?)` | Write text starting at (x, y). |
| `cells.fill(x, y, w, h, ch, fg, bg, attr?)` | Fill a rectangle with a character. |
| `cells.paintANSI(x, y, w, ansiText)` | Paint a pre-formatted ANSI string into a single row. |
| `cells.log(msg)` | Debug log. Kept narrow so it's the one impure escape hatch allowed from render. |

### Attribute constants

Constants live on the `cells` object so they only exist inside render:

| Constant | Value | Description |
|----------|-------|-------------|
| `cells.ATTR_NONE` | 0 | No attributes |
| `cells.ATTR_BOLD` | 1 | Bold text |
| `cells.ATTR_FAINT` | 2 | Dim/faint text |
| `cells.ATTR_ITALIC` | 4 | Italic text |
| `cells.ATTR_UNDERLINE` | 8 | Underlined text |
| `cells.ATTR_REVERSE` | 16 | Reverse video |

```js
renderAscii: function(state, me, cells) {
    cells.writeString(0, 0, "Hello, " + me.name, "#00FF00", null);
    cells.setChar(5, 2, "@", "#FFFF00", "#000080", cells.ATTR_BOLD);
}
```

Coordinates are relative to the viewport (0,0 = top-left).

---

## `canvas` — the pixel render surface

`canvas` is the object passed to `renderCanvas(state, me, canvas)`. It exposes a Canvas2D-like surface plus a 3D triangle rasterizer.

| Area | Members |
|------|---------|
| **Dimensions** | `canvas.width`, `canvas.height` (logical pixels) |
| **State** | `canvas.save()`, `canvas.restore()` |
| **Transforms** | `canvas.translate(x, y)`, `canvas.rotate(angle)` |
| **Paint style** | `canvas.setFillStyle(color_or_gradient)`, `canvas.setStrokeStyle(…)` — accepts either a `#rgb(a)?`/`#rrggbb(aa)?` string or a gradient handle. |
| **2D fill** | `canvas.fillRect(x, y, w, h)`, `canvas.fillCircle(x, y, r)`, `canvas.fillText(text, x, y)` |
| **Paths** | `canvas.beginPath()`, `canvas.moveTo(x, y)`, `canvas.lineTo(x, y)`, `canvas.stroke()`, `canvas.closePath()` |
| **Gradients** | `canvas.createLinearGradient(x0, y0, x1, y1)`, `canvas.createRadialGradient(x0, y0, r0, x1, y1, r1)` → handle with `addColorStop(offset, color)` |
| **3D** | `canvas.fillTriangle3D(v0, v1, v2, [c0, c1, c2])`, `canvas.fillTriangle3DFlat(v0, v1, v2, color)`, `canvas.fillTriangle3DLit(v0, v1, v2, n0, n1, n2, lightDir, baseColor, ambient)`, `canvas.clearDepth()` |
| **Debug** | `canvas.log(msg)` — the render-path escape hatch |
| **Constants** | `canvas.PI`, `canvas.TAU` |

3D vertex parameters are `[x, y, z]` arrays. The depth buffer is shared across all `fillTriangle3D*` calls and is not automatically cleared between frames — call `canvas.clearDepth()` at the start of each 3D pass.

---

## Custom slash commands

Register commands from any server-only hook (typically `init`):

```js
Game = {
    init: function(ctx) {
        ctx.registerCommand({
            name: "score",
            description: "Show your score",
            adminOnly: false,
            firstArgIsPlayer: false,
            handler: function(playerID, isAdmin, args) {
                ctx.chatPlayer(playerID, "Your score: " + lookupScore(playerID));
            }
        });
        return { /* initial state */ };
    }
};
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Command name without the `/` |
| `description` | string | no | Shown in `/help` |
| `adminOnly` | bool | no | If true, non-admins cannot run it |
| `firstArgIsPlayer` | bool | no | Tab-completes first argument against player names |
| `handler` | function(playerID, isAdmin, args) | yes | Called when the command runs |

Handler args:
- `playerID` — the player who ran the command (empty string = server console)
- `isAdmin` — whether the caller is an admin
- `args` — array of string arguments after the command name

When a command fires, it also appears in `update`'s event stream as `{ type: "command", playerID, name, args }` so games can keep command routing in one place.

---

## Framework-injected state

The framework populates these keys on `state` each tick. Games may read them but must not write — the framework overwrites them after `update` returns.

| Key | Shape | Notes |
|-----|-------|-------|
| `state.teams` | `[{ name, color, players: [{ id, name }] }]` | Valid from `begin()` onward. |
| `state._gameTime` | seconds since `begin()` | Always present after the game starts. |

`_gameTime` is the **contract clock**. The client extrapolates it locally between server snapshots from its own wall clock, so games that derive motion from `state._gameTime` get smooth render-fps animation even though the server only ticks at ~10 Hz. Whenever a non-clock state change arrives, the snapshot's `_gameTime` is treated as authoritative and the local clock is snapped to it.

Games that derive render state from time should read it from `state._gameTime` — never from `ctx.now()` or wall-clock — so motion stays smooth at the client's render fps regardless of the server's tick rate.

---

## Global functions

Only two free functions live at module scope in games:

| Function | Description |
|----------|-------------|
| `figlet(text, font?)` | Renders `text` as ASCII art. Built-in fonts: `"standard"` (default), `"larry3d"`. Unknown fonts fall back to `"standard"`. |
| `include(name)` | Evaluates another `.js` file from the same directory. The `.js` extension is added if omitted. Idempotent. Path traversal (`..`) is rejected. |

Everything side-effecty (log, chat, midi, gameOver, …) lives on `ctx` and is therefore unreachable from render.

---

## Game lifecycle

```
LOBBY (teams panel + chat)
  │  /game load <name>
  ▼
STARTING (framework splash, up to 10s — admin presses Enter to start early)
  │  admin presses Enter or 10s timeout
  ▼
PLAYING (game viewport + chat)
  │  ctx.gameOver() called from update
  ▼
LOBBY (chat history preserved, ranked results posted as a system message)
```

- **Load**: Framework snapshots teams, reads any persistent state from `dist/state/<gameName>.json`, calls `init(ctx, savedState)`, installs the return value as `Game.state`. On resume, `init` is still called (for initial state and persistent data), then `resume(sessionState)` runs in place of `begin`.
- **Starting**: Framework renders the game name in figlet ASCII art centered in the viewport. The admin can press Enter to skip, or it auto-starts after 10s.
- **Starting → Playing**: Framework injects `state.teams`, calls `begin(state, ctx)`.
- **Playing**: `update(state, dt, events, ctx)` runs once per tick, followed by `renderAscii`/`renderCanvas`/`layout`/`statusBar`/`commandBar` per player.
- **Game over**: Triggered when JS calls `ctx.gameOver(results)`. The framework posts the ranked results to chat as a system message and unloads the game. Chat history persists.
- **Late joiners**: Players connecting during a game see the lobby and can chat. Lobby teams are independent — players can organize for the next round. The `join` event is delivered to the next `update` call.
- **Reconnect**: If a player disconnects mid-game and reconnects with the same name, they rejoin automatically. Game teams persist through disconnects.

---

## Teams

Players start unassigned and configure teams in the lobby before a game starts. Use the team panel (Tab to focus, Up/Down to switch teams) to join or create teams.

The first player in a team is the team leader:
- **Enter** — rename the team
- **Left/Right** — cycle the team color

Games read teams via `state.teams` (preferred) or `ctx.teams()`:

```js
// In any server-only hook:
state.teams  // [{ name, color, players: [{ id, name }, …] }, …]
```

Games can declare a `teamRange` property to enforce a valid team count:

```js
Game.teamRange = { min: 2, max: 4 };
```

The framework blocks loading if the lobby has too few or too many teams.

---

## State persistence

Games use two separate hooks to persist different kinds of data:

| Hook | Returns | Stored in | Called when | Read back as |
|------|---------|-----------|-------------|--------------|
| `unload()` | **Persistent state** (high scores, unlocks) | `dist/state/<gameName>.json` | Game-over, `/game unload`, AND after `suspend()` during `/game suspend` | `init(ctx, savedState)` |
| `suspend()` | **Session state** (mid-game snapshot) | suspend save file | `/game suspend` only | `resume(sessionState)` |

Both hooks take no parameters; read from `Game.state` directly.

**Persistent state** survives across all sessions. The framework writes `unload()`'s return value to disk on every game-over and feeds it back into `init(ctx, savedState)` on the next load. `savedState` is `null` on the very first load of a game.

**Session state** is a mid-game snapshot stored in the suspend save file. It is received via `resume(sessionState)` when restoring from that save.

```js
var Game = {
    init: function(ctx, savedState) {
        return {
            score:     0,
            highScore: (savedState && savedState.highScore) || 0
        };
    },

    unload: function() {
        return { highScore: Game.state.highScore };
    },

    suspend: function() {
        return { score: Game.state.score, board: Game.state.board };
    },

    resume: function(sessionState) {
        if (sessionState) {
            Game.state.score = sessionState.score;
            Game.state.board = sessionState.board;
        }
    }
};
```

### Suspend / resume

Any playing game can be suspended — no opt-in flag is required.

**Commands**:
- `/game suspend [saveName]` — suspends the active game. Auto-generates a timestamp name if omitted.
- `/game resume <gameName/saveName>` — resumes a saved session. Tab-completes against existing saves.
- **File → Saves…** — lists all saves; Load (chrome admin) or Remove (console).

**Save location**: `dist/state/saves/<gameName>/<saveName>.json`

**On suspend**:
1. `suspend()` runs, session snapshot stored in the save file.
2. `unload()` runs, persistent state saved to `dist/state/<gameName>.json`.

**On resume**:
1. `init(ctx, savedState)` runs with the persistent data the same as a fresh load.
2. `resume(sessionState)` runs in place of `begin`, with the session snapshot.
3. If `resume` is not defined, the framework falls back to `begin` — persistent state is still seeded by `init`, but the mid-game session snapshot is lost.

---

## Layout and sizing

**Lobby:**
```
┌─────────────────────────────────────┐
│ menu bar (full width)               │
├─────────────────────────┬───────────┤
│ chat (~70%)             │ teams     │
│                         │ panel     │
├─────────────────────────┴───────────┤
│ input row (full width)              │
├─────────────────────────────────────┤
│ status bar (full width)             │
└─────────────────────────────────────┘
```

**In-game:**
```
┌─────────────────────────────────────┐
│ menu bar (1 row — gameName)         │
├─────────────────────────────────────┤
│ status bar (1 row — Game.statusBar) │
├─────────────────────────────────────┤
│                                     │
│ game viewport (cells.width × .height)
│                                     │
├─────────────────────────────────────┤
│ chat (5–10 rows, per View menu)     │
├─────────────────────────────────────┤
│ command bar (1 row — Game.commandBar)
├─────────────────────────────────────┤
│ status bar (1 row — server time)    │
└─────────────────────────────────────┘
```

- `cells.width` = full terminal width
- `cells.height` = terminal height minus 7 rows of chrome (menu bar, window borders, two dividers, command bar, status bar) minus the chat size (default 5, configurable 5–10 via View > Chat size)
- The menu bar is full width — `gameName` can use it entirely.

---

## Render modes and `state`

Each player chooses a render mode (Ascii, Blocks, Pixels) and a render location (Remote, Local). Blocks and Pixels require `renderCanvas`. Local modes re-execute the game's JS on the GUI client and call only `renderCanvas` (or `renderAscii`) — never `update`. Everything your renderer needs must therefore be on `state`, because that's the only thing transported to the client each tick.

Module-level `var`s are not shared: they're initialized once on each VM (server and client) and only the server's `update` runs against its copy. The client re-executes the game file once on load, then holds its VM idle except for render calls.

Rule of thumb: if a value affects what `renderAscii`/`renderCanvas` draws, put it on `state` inside `update`. If it's pure bookkeeping (caches, private maps, one-time constants), module scope is fine.

### Render tips

**`renderAscii`/`renderCanvas` is called per player per tick.** Keep it fast and side-effect-free. Don't mutate state from render.

**`update(state, dt, events, ctx)` is called once per tick.** All gameplay logic belongs here. Always use `dt` for timing (accumulate elapsed seconds, count down timers by subtracting `dt`) — never count ticks; tick rate is configurable.

**Use `state._gameTime` for time-based render.** Never call `ctx.now()` from render (you can't — ctx isn't there) and avoid wall-clock in general. `state._gameTime` is the one clock that extrapolates smoothly on the client.

**Rendering is character-based in Ascii/Blocks modes.** Each character is one cell wide. For emoji or box-drawing that spans multiple columns, count display width carefully — the framework does not reflow.

---

## Sharing your game

Host your `.js` file anywhere publicly accessible over HTTPS — a GitHub repo is the simplest option. Anyone running dev-null can then load it directly:

```
/game load https://github.com/you/your-repo/blob/main/mygame.js
```

GitHub blob URLs are automatically converted to raw download URLs. The file is cached locally in `.cache/` so it survives restarts; re-running the load command fetches the latest version.

---

## Widget tree layout (`layout`)

If your game defines `layout(state, me)`, it should return a tree of widget nodes. The framework renders these as real themed NC-style panels with proper borders, respecting the player's current theme. This is optional — games that only define `renderAscii` or `renderCanvas` work unchanged.

The widget tree itself encodes sizing via `weight`/`width`/`height` node properties; viewport pixel dimensions aren't passed to the hook because the reconciler handles distribution.

### Node types

| Type | Description | Key properties |
|------|-------------|----------------|
| `gameview` | Renders the raw `renderAscii`/`renderCanvas` output in this region | (none — calls the game's render with a sub-area) |
| `panel` | Bordered NC panel with optional title | `title`, `children` |
| `label` | Single line of text | `text`, `align` (`"left"`, `"center"`, `"right"`) |
| `hsplit` | Horizontal split — children placed side by side | `children` |
| `vsplit` | Vertical split — children stacked top to bottom | `children` |
| `divider` | Horizontal divider line | (none) |
| `table` | Aligned columns | `rows` (array of arrays of strings) |

### Sizing

Every node can have:
- `weight` (float) — flex weight for distributing space in a split (default: 1)
- `width` (int) — fixed width in chars (overrides weight, for hsplit children)
- `height` (int) — fixed height in rows (overrides weight, for vsplit children)

### Example: hybrid layout

```js
layout: function(state, me) {
    return {
        type: 'hsplit',
        children: [
            {
                type: 'vsplit', weight: 1,
                children: [
                    { type: 'gameview', weight: 1 },
                    { type: 'panel', title: 'Stats', height: 5,
                      children: [
                          { type: 'label', text: 'HP: ' + me.hp },
                          { type: 'label', text: 'Score: ' + state.score }
                      ] }
                ]
            },
            {
                type: 'panel', title: 'Players', width: 25,
                children: [{ type: 'table', rows: buildPlayerRows(state) }]
            }
        ]
    };
}
```

Renders the raw game view in the top-left, a stats panel below it, and a players panel on the right — all using the framework's themed NC borders.

---

## Shaders (post-processing)

Shaders are per-player scripts that modify the rendered screen buffer before it's displayed. They run after all game/lobby content is drawn but before menus and dialogs are overlaid. Shaders are independent of the game contract — they use their own flat API (no `ctx`, no `state`).

### Loading shaders

```
/shader load invert       # Load from dist/shaders/invert.js
/shader load https://...  # Load from URL (cached locally)
/shader unload invert     # Remove shader
/shader list              # Show active shaders in order
/shader up invert         # Move shader earlier in the chain
/shader down invert       # Move shader later in the chain
/shader                   # List available + active shaders
```

Shaders are also accessible from the **File → Shaders…** menu.

### Writing a shader

Shaders are JS files in `dist/shaders/`. A shader exports a global `Shader` object with a required `process(buf)` method:

```javascript
const Shader = {
    init()   { },        // optional: called once on load
    update(dt) { },      // optional: called every tick
    process(buf) {       // required: called every frame
        for (var y = 0; y < buf.height; y++) {
            for (var x = 0; x < buf.width; x++) {
                var p = buf.getPixel(x, y);
                if (p) buf.setChar(x, y, p.char, p.bg, p.fg, p.attr); // swap fg/bg
            }
        }
    },
    unload() { }         // optional: called on removal
};
```

### Shader buffer API

| Method | Description |
|--------|-------------|
| `buf.width` / `buf.height` | Dimensions in cells. |
| `buf.getPixel(x, y)` | Returns `{char, fg, bg, attr}` or `null`. |
| `buf.setChar(x, y, ch, fg, bg, attr)` | Set a single cell. |
| `buf.writeString(x, y, text, fg, bg, attr)` | Write text. |
| `buf.fill(x, y, w, h, ch, fg, bg, attr)` | Fill a rectangle. |
| `buf.recolor(x, y, w, h, fg, bg, attr)` | Change colors/attributes without changing characters. |

### Shader globals

| Constant | Value |
|----------|-------|
| `ATTR_NONE` / `ATTR_BOLD` / `ATTR_FAINT` / `ATTR_ITALIC` / `ATTR_UNDERLINE` / `ATTR_REVERSE` | Attribute bitmask values |

| Function | Description |
|----------|-------------|
| `log(msg)` | Log a debug message (visible in server log). |
| `now()` | Server time in epoch milliseconds. |

### Bundled shaders

| Shader | Effect |
|--------|--------|
| `invert` | Swaps foreground and background colors |
| `scanlines` | Animated scrolling scanlines (CRT effect) |
| `crt` | Green-on-black retro terminal look |
| `rainbow` | Cycles border/box-drawing characters through a flowing rainbow |

### Execution order

Multiple shaders run in the order they were loaded. Use `/shader up` and `/shader down` to reorder. Each shader receives the buffer as modified by the previous shader in the chain.
