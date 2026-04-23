# dev-null API Reference

This document explains how to write games for dev-null. Games are plain JavaScript files (ES5-compatible, no modules). Drop your file in `dist/games/`, or share it via URL — no build step required.

---

## Quick orientation

| Concept | What it means |
|---------|---------------|
| **Game** | One active at a time; owns the viewport, status bar, and command bar |
| **Lobby** | The state when no game is loaded; only chat is visible |
| **Viewport** | The rectangular region your game renders into (below the game status bar, above chat) |

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

A game file must define a global `Game` object. `load` and `renderAscii` are required; all other hooks are optional.

```js
var Game = {

    // --- Core hooks ---

    // Called once per server tick with the time elapsed since the last update
    // in seconds. Put all game logic here — movement, timers, collision
    // detection, gameOver() calls. This is called exactly once per tick
    // regardless of how many players are connected. Use dt for all timing.
    update: function(dt) {},

    // Called when a player disconnects mid-game.
    onPlayerLeave: function(playerID) {},

    // Called every time a player presses a key while in game mode (not while chatting).
    // key is a string: "up", "down", "left", "right", "enter", "esc", "space",
    // "a"–"z", "A"–"Z", "0"–"9", "ctrl+c", "ctrl+z", "f1"–"f12", etc.
    onInput: function(playerID, key) {},

    // Renders the game viewport into buf at position (ox, oy) with dimensions width × height.
    // buf is an ImageBuffer — call buf.setChar(x, y, ch, fg, bg), buf.writeString(x, y, text, fg, bg),
    // buf.fill(x, y, w, h, ch, fg, bg) to write pixels directly. Colors are "#RRGGBB" strings or null.
    // Coordinates are relative to the buffer region (0,0 = top-left of the game viewport).
    // Called per player on every render tick. Must be pure rendering — no game state mutation.
    // Used when the player's graphics preference is Ascii, or as fallback when canvas is unavailable.
    renderAscii: function(buf, playerID, ox, oy, width, height) {
        buf.writeString(0, 0, "Hello!", "#FFFFFF", null);
    },

    // Returns a declarative widget tree describing the game window.
    // If defined, the framework renders real themed NC panels/labels instead of
    // using the raw renderAscii() output. Games can embed {type: "gameview"} nodes to
    // include the raw renderAscii() output within the layout. If layout returns null
    // or is not defined, the framework falls back to renderAscii(). See "Widget Tree Layout" below.
    layout: function(playerID, width, height) {
        return {
            type: 'hsplit',
            children: [
                { type: 'gameview', weight: 1 },           // raw renderAscii() here
                { type: 'panel', title: 'Info', width: 20, // NC panel on the right
                  children: [{ type: 'label', text: 'Score: 42' }] }
            ]
        };
    },

    // Returns the text for the game status bar (1 row, below the menu bar).
    // Keep content shorter than width.
    statusBar: function(playerID) {
        return "HP: 100  Score: 0";
    },

    // Returns the idle hint shown in the command bar (above the framework status bar).
    // Example: "[↑↓←→] Move  [Enter] Chat"
    // Return "" to show the default hint.
    commandBar: function(playerID) {
        return "";
    },

    // --- Properties (all optional) ---

    // Display name shown in the menu bar and splash screen. If omitted, the filename stem is used.
    gameName: "My Awesome Game",

    // Supported team count range. The framework blocks loading if the lobby has
    // fewer or more teams. Zero means no constraint on that end.
    // Omit to allow any number of teams.
    teamRange: { min: 2, max: 4 },

    // --- Lifecycle ---

    // Called on game load with persisted state (or null on first run). Mandatory.
    // teams() global is available. Use savedState to restore previous session.
    load: function(savedState) {
        if (savedState) {
            // restore previous state
            score = savedState.score;
        }
    },

    // Called at starting→playing transition. Set up real-time game state here. Optional.
    begin: function() {
        // game begins — teams() available
    },

    // Called when the game signals game-over, before the ending screen is shown. Optional.
    // Use this for cleanup, final score calculations, etc.
    end: function() {
        // optional cleanup
    },

    // Called on game-over, /game unload, AND after suspend() during /game suspend.
    // Return PERSISTENT state (high scores, unlocks) — saved to dist/state/<game>.json
    // and passed back via load(persistentState) on the next fresh load or resume.
    unload: function() {
        return { highScore: highScore };
    },

    // Called on /game suspend BEFORE unload(). Return SESSION state (current board,
    // score in progress) to store in the suspend save file.
    // Return undefined/null if the game has no meaningful mid-session state.
    suspend: function() {
        return { score: score, board: board };
    },

    // Called INSTEAD OF begin() when restoring from a suspend save.
    // sessionState is the value previously returned by suspend().
    // If not defined, falls back to begin() — old games without this hook still work.
    resume: function(sessionState) {
        if (sessionState) {
            score = sessionState.score;
            board = sessionState.board;
        }
    }
};
```

### Minimal working example

```js
var players = {};

var Game = {
    onPlayerJoin: function(playerID, playerName) {
        players[playerID] = { name: playerName, x: 10, y: 5 };
    },

    onPlayerLeave: function(playerID) {
        delete players[playerID];
    },

    onInput: function(playerID, key) {
        var p = players[playerID];
        if (!p) return;
        if (key === "up")    p.y = Math.max(0, p.y - 1);
        if (key === "down")  p.y++;
        if (key === "left")  p.x = Math.max(0, p.x - 1);
        if (key === "right") p.x++;
    },

    renderAscii: function(buf, playerID, ox, oy, width, height) {
        for (var y = 0; y < height; y++) {
            for (var x = 0; x < width; x++) {
                var ch = ".";
                for (var id in players) {
                    if (players[id].x === x && players[id].y === y) {
                        ch = (id === playerID) ? "@" : "O";
                        break;
                    }
                }
                buf.setChar(x, y, ch, null, null);
            }
        }
    },

    // The framework renders the starting screen (figlet game name) and the
    // ending screen (figlet "GAME OVER" + ranked results). Games don't provide
    // their own.

    statusBar: function(playerID) {
        var p = players[playerID];
        var n = Object.keys(players).length;
        return p ? "pos: (" + p.x + "," + p.y + ")  players: " + n : "";
    },

    commandBar: function(playerID) {
        return "[↑↓←→] Move  [Enter] Chat";
    }
};
```

### Registering game commands

Call `registerCommand` at the top level of your script (not inside Game hooks). The command becomes available as `/commandname` to all players as long as the game is loaded.

```js
registerCommand({
    name: "score",
    description: "Show your score",
    adminOnly: false,       // optional; defaults to false
    firstArgIsPlayer: false, // optional; tab-completes first arg against player names
    handler: function(playerID, isAdmin, args) {
        // args is an array of strings (the words after /score)
        // use chat() or chatPlayer() to reply — ctx.Reply is not available in JS
        chatPlayer(playerID, "Your score: " + getScore(playerID));
    }
});
```

---

## Global functions

These are available in games.

| Function | Description |
|----------|-------------|
| `log(message)` | Writes to the server log panel (never shown to players). Useful for debugging. |
| `chat(message)` | Broadcasts a system chat message to all players. `author` will be empty (renders as `[system] message`). |
| `chatPlayer(playerID, message)` | Sends a private message to one player. |
| `teams()` | Returns an array of `{ name, color, players: [{id, name}, ...] }`. During a game, returns the game teams snapshot. |
| `figlet(text)` | Renders `text` as ASCII art using the built-in `"standard"` font. Returns a multi-line string. |
| `figlet(text, font)` | Same, using the named font. Built-in fonts: `"standard"`, `"larry3d"`. Falls back to `"standard"` for unknown fonts. |
| `registerCommand(spec)` | Registers a slash command. See below. |
| `addMenu(label, items)` | Adds a top-level menu to the NC action bar. Call at the top level of your script. `label` is the menu title. `items` is an array of menu item objects; see below. |
| `messageBox(playerID, opts)` | Shows a modal dialog to a specific player. `opts` is `{ title, message, buttons, onClose }`. See below. |
| `gameOver()` | Signals that the game has ended. Transitions to the ending screen. |
| `gameOver(results)` | Same as above, with ranked results displayed on the ending screen. `results` is an array of `{ name, result }` in ranked order. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). |
| `now()` | Returns the server time as epoch milliseconds (same as `Date.now()` but uses the framework's central clock, which is mockable in tests). Available in both games and plugins. |
| `include(name)` | Evaluates another `.js` file from the same directory as the game file. Used for multi-file games in `games/<name>/` folders. The `.js` extension is added automatically if omitted. Each file is only included once (idempotent). Path traversal (`..`) is rejected. |
| `playSound(filename, opts)` | Plays an audio file on graphical clients. File must be a game asset (.ogg, .mp3, .wav). Options: `{ loop: true }` for looping, `{ alt: "text" }` for chat fallback text on non-graphical clients. |
| `stopSound(filename)` | Stops playback of the named audio file. Call with no arguments or empty string to stop all sounds. |
| `midiNote(channel, note, velocity, durationMs)` | Plays a MIDI note on all graphical clients. Channel 0-15, note 0-127, velocity 0-127. Duration in ms (0 = NoteOn only, no auto-off). Requires a SoundFont on the client (`/synth` command). |
| `midiNotePlayer(playerID, ch, note, vel, dur)` | Same as `midiNote` but only for one player. |
| `midiProgram(channel, program)` | Changes the instrument on a MIDI channel for all players. Program 0-127 (General MIDI). |
| `midiProgramPlayer(playerID, ch, program)` | Same as `midiProgram` but only for one player. |
| `midiCC(channel, controller, value)` | Sends a MIDI Control Change. Common controllers: 7=volume, 10=pan, 64=sustain. |

### `registerCommand(spec)`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Command name without the `/` |
| `description` | string | no | Shown in `/help` |
| `adminOnly` | bool | no | If true, non-admins cannot use it |
| `firstArgIsPlayer` | bool | no | Tab-completes first argument against player names |
| `handler` | function(playerID, isAdmin, args) | yes | Called when the command runs |

The handler signature:
- `playerID` — the player who ran the command (empty string = server console)
- `isAdmin` — whether the caller is an admin
- `args` — array of string arguments after the command name

Use `chatPlayer(playerID, text)` to reply privately to the caller.

---

### `addMenu(label, items)`

Registers a top-level entry in the NC-style action bar. Call at the **top level** of your script (not inside a hook). The menu persists for the lifetime of the game.

```js
addMenu("&Game", [
    { label: "&New Game", onClick: function(playerID) { /* ... */ } },
    { label: "---" },  // separator
    { label: "&High Scores", onClick: function(playerID) {
        messageBox(playerID, {
            title: "High Scores",
            message: "1. Alice  4200\n2. Bob    3100",
            buttons: ["OK"]
        });
    }}
]);
```

**Item fields:**

| Field | Type | Description |
|-------|------|-------------|
| `label` | string | Display text. Prefix a character with `&` to mark it as the keyboard shortcut (e.g. `"&Save"` → **S** is the shortcut, rendered highlighted). A label of `"---"` (or any all-dashes string) renders as a separator line. |
| `disabled` | bool | Optional. If true, the item is shown greyed out and cannot be selected. |
| `onClick` | function(playerID) | Called when the item is activated. `playerID` is the player who selected it. |

**Keyboard shortcuts:** Use `&` in labels to designate shortcut keys (e.g. `"&File"`, `"&Save"`). The shortcut character is rendered highlighted. `Alt+letter` opens a menu directly. When the bar is focused, pressing the letter key opens the matching menu. Inside a dropdown, pressing the letter activates the matching item.

**Navigation:** Press `F10` or `Alt+letter` to activate the action bar. `Left`/`Right` move between menu titles. `Down` or `Enter` opens the dropdown. Inside a dropdown: `Up`/`Down` navigate items, `Enter` or shortcut letter selects, `Esc` closes back to the bar. `F10` or `Esc` at the bar level deactivates.

---

### `messageBox(playerID, opts)`

Shows a modal dialog to the specified player. The dialog overlays the current view and intercepts all keyboard input until dismissed.

```js
messageBox(playerID, {
    title: "Confirm",
    message: "Are you sure you want to restart?",
    buttons: ["Yes", "No"],
    onClose: function(button) {
        if (button === "Yes") {
            // restart game
        }
    }
});
```

**`opts` fields:**

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Dialog title bar text. |
| `message` | string | Body text. `\n` creates line breaks. |
| `buttons` | array | Button labels. Defaults to `["OK"]` if omitted. |
| `onClose` | function(button) | Called with the label of the button pressed, or `""` if the dialog was dismissed with `Esc`. |

**Navigation:** `Tab` or `Left`/`Right` cycle through buttons. `Enter` or `Space` activates the focused button. `Esc` calls `onClose("")`.

---

## Key strings for `onInput`

Keys are passed as Bubble Tea key strings. Common values:

| Key | String |
|-----|--------|
| Arrow keys | `"up"` `"down"` `"left"` `"right"` |
| Enter | `"enter"` |
| Escape | `"esc"` |
| Space | `" "` |
| Backspace | `"backspace"` |
| Tab | `"tab"` |
| Page Up/Down | `"pgup"` `"pgdown"` |
| Home / End | `"home"` `"end"` |
| Function keys | `"f1"` … `"f12"` |
| Letters | `"a"` … `"z"` (lowercase) `"A"` … `"Z"` (uppercase/shift) |
| Digits | `"0"` … `"9"` |
| Ctrl combos | `"ctrl+a"` … `"ctrl+z"` |

> `onInput` is only called when the player is in game mode (not when they're typing a chat message). Players enter chat mode by pressing Enter and return to game mode by pressing Escape or submitting.

---

## Game lifecycle

```
LOBBY (teams panel + chat)
  │  /game load <name>
  ▼
SPLASH (game splash or default, up to 10s — admin presses Enter to start early)
  │  admin presses Enter or 10s timeout
  ▼
PLAYING (game viewport + chat)
  │  JS calls gameOver()
  ▼
GAME OVER (framework results screen, up to 15s)
  │  all players press Enter or 15s timeout
  ▼
LOBBY (game unloaded, back to teams + chat)
```

- **Load**: Framework snapshots teams for the game (lobby stays independent), loads saved state, calls `init(savedState)`. `teams()` returns game teams.
- **Splash screen**: If `renderSplash(buf, playerID, x, y, w, h)` is defined and returns true, that custom rendering is used. Otherwise, the framework renders the game name in figlet ASCII art centered in the viewport. The admin can press Enter to skip, or it auto-starts after 10s.
- **Splash→Playing**: Framework calls `start()`. Game sets up its playing state.
- **Playing**: Normal game mode — `update(dt)` is called once per tick, then `renderAscii()`/`renderCanvas()`/`layout()`, `onInput()`, `statusBar()`, `commandBar()` are called per player.
- **Game over**: Triggered when JS calls `gameOver(results, state)`. The framework renders a "GAME OVER" screen with the ranked results list. Players press Enter to acknowledge; after 15 seconds the game unloads automatically.
- **Late joiners**: Players connecting during a game see the lobby and can chat. Lobby teams are independent — players can organize for the next round.
- **Reconnect**: If a player disconnects mid-game and reconnects with the same name, they rejoin the game automatically. Game teams persist through disconnects.

## Teams

Players start unassigned and configure teams in the lobby before a game starts. Use the team panel (Tab to focus, Up/Down to switch teams) to join or create teams.

The first player in a team is the team leader:
- **Enter** — rename the team
- **Left/Right** — cycle the team color

Games access teams via the `teams()` global:
```js
teams()  // [{name, color, players: [{id, name}, ...]}, ...]
```

Games can declare a `teamRange` property to enforce a valid team count:
```js
Game.teamRange = { min: 2, max: 4 };
```
The framework blocks loading if the lobby has too few or too many teams.

## State persistence

Games use two separate hooks to persist different kinds of data:

| Hook | Returns | Stored in | Called when |
|------|---------|-----------|-------------|
| `unload()` | **Persistent state** (high scores, unlocks) | `dist/state/<gameName>.json` | Game-over, `/game unload`, AND after `suspend()` during `/game suspend` |
| `suspend()` | **Session state** (board, current score) | suspend save file | `/game suspend` only |

**Persistent state** survives across all sessions and is received via `load(persistentState)` on every fresh load and resume. Return it from `unload()`.

**Session state** is a mid-game snapshot stored in the suspend save file. It is received via `resume(sessionState)` when restoring from that save. Return it from `suspend()`.

```js
var Game = {
    state: { score: 0, highScore: 0 },

    // load: called with persistent state on EVERY load (fresh and resume).
    load: function(saved) {
        if (saved) Game.state.highScore = saved.highScore || 0;
    },

    begin: function() {
        Game.state.score = 0; // fresh start — not called on resume
    },

    // unload: returns PERSISTENT state. Called on game-over, /game unload,
    // AND after suspend() during /game suspend.
    unload: function() {
        if (Game.state.score > Game.state.highScore)
            Game.state.highScore = Game.state.score;
        return { highScore: Game.state.highScore };
    },

    // suspend: returns SESSION state (mid-game snapshot).
    // Called before unload() during /game suspend.
    suspend: function() {
        return { score: Game.state.score, board: Game.state.board };
    },

    // resume: called instead of begin() when restoring from a suspend save.
    // Falls back to begin() if this hook is not defined.
    resume: function(saved) {
        if (saved) {
            Game.state.score = saved.score;
            Game.state.board = saved.board;
        }
    },
};
```

## Suspend/resume

Any playing game can be suspended — no opt-in flag is required.

**Commands**:
- `/game suspend [saveName]` — suspends the active game. Auto-generates a timestamp name if omitted.
- `/game resume <gameName/saveName>` — resumes a saved session. Tab-completes against existing saves.
- **File → Saves...** — lists all saves; Load (chrome admin) or Remove (console).

**Save location**: `dist/state/saves/<gameName>/<saveName>.json`

**Lifecycle on suspend**:
1. `suspend()` — session snapshot stored in the save file (board state, current score, etc.)
2. `unload()` — persistent state saved immediately to `dist/state/<gameName>.json` (high scores are not lost even if the save is later deleted)

**Lifecycle on resume**:
1. `load(persistentState)` — persistent state (high scores) loaded first, same as a fresh load
2. `resume(sessionState)` — session state restored; called **instead of** `begin()`
   - If `resume` is not defined, falls back to `begin()` (game starts fresh but keeps persistent state)

**Backward compatibility**: Games without `suspend()` return nil session state — on resume, `resume(null)` falls back to `begin()`, starting fresh while preserving persistent state. Games that don't define `resume()` also fall back to `begin()`.

## Layout and sizing

**Lobby:**
```
┌────────────────────────┬───────────┐
│ menu bar (full width)               │  ← framework: server name, players, uptime
├────────────────────────┬───────────┤
│ chat (70% width)       │ teams     │
│                        │ panel     │
│                        │ (30%)     │
├────────────────────────┴───────────┤
│ input row (full width)              │  ← text input / team controls
├─────────────────────────────────────┤
│ status bar (full width)             │  ← framework: server time (always present)
└─────────────────────────────────────┘
```

**In-game (playing):**
```
┌────────────────────────────────────┐
│ menu bar (1 row)                   │  ← framework: game name
├────────────────────────────────────┤
│ status bar (1 row)                 │  ← Game.statusBar(playerID)
├────────────────────────────────────┤
│                                    │
│ game viewport (width × height)     │  ← Game.render(buf, playerID, ox, oy, w, h)
│                                    │
├────────────────────────────────────┤
│ chat (5–10 rows, per View menu)    │
├────────────────────────────────────┤
│ command bar (1 row)                │  ← Game.commandBar() when idle; text input on Enter
├────────────────────────────────────┤
│ status bar (1 row)                 │  ← framework: server time (always present)
└────────────────────────────────────┘
```

- `width` = full terminal width
- `height` = terminal height minus 7 rows of chrome (menu bar, window borders, two dividers, command bar, status bar) minus the chat size (default 5, configurable 5–10 via View > Chat size)
- Return exactly `height` newline-separated rows from `renderAscii()`. Fewer rows are padded; more are clipped.
- The menu bar is full width — `gameName` can use it entirely.

---

## Tips

**State is global and shared.** All players see the result of the same `Game` object — there is no per-player instance. Design your state with this in mind (`var players = {}`).

**`renderAscii()` is called per player per tick.** Keep it fast and side-effect-free — no game state mutation. Put all game logic in `update(dt)` instead.

**`update(dt)` is called once per tick.** The `dt` argument is seconds since the last update. All game logic — movement, timers, collision, gameOver() calls — belongs here. Always use `dt` for timing (accumulate elapsed time, count down timers by subtracting `dt`) — never count ticks.

**Rendering is character-based.** Each character is one cell wide. For box-drawing or emoji that span multiple columns, count display width carefully — the framework does not reflow.

**ImageBuffer API.** The `buf` parameter in `renderAscii()` supports these methods:

| Method | Description |
|--------|-------------|
| `buf.setChar(x, y, ch, fg, bg)` | Set one character. `fg`/`bg` are `"#RRGGBB"` or `null` (default). |
| `buf.writeString(x, y, text, fg, bg)` | Write plain text starting at (x, y). |
| `buf.fill(x, y, w, h, ch, fg, bg)` | Fill a rectangle with a character. |
| `buf.width` / `buf.height` | Dimensions of the game viewport. |

All methods accept an optional trailing `attr` parameter (bitmask): `ATTR_BOLD`, `ATTR_FAINT`, `ATTR_ITALIC`, `ATTR_UNDERLINE`, `ATTR_REVERSE`. Coordinates are relative to the viewport (0,0 = top-left).

```js
renderAscii: function(buf, playerID, ox, oy, width, height) {
    buf.writeString(0, 0, "Hello, " + playerID, "#00FF00", null);
    buf.setChar(5, 2, "@", "#FFFF00", "#000080", ATTR_BOLD);
}
```

**ANSI escape codes** still work in `statusBar()` and `commandBar()` output.

**Pixel mode and `Game.state`.** In Pixels render mode, the GUI client re-executes your game JS locally and calls `renderCanvas()` each frame — but never calls `update()`. Any mutable state your renderer needs must be on `Game.state` so the server can send it to the client each tick. The engine automatically injects `Game.state._gameTime` (cumulative elapsed seconds since `begin()`), so canvas games can always read the current time:

```js
renderCanvas: function(ctx, pid, w, h) {
    // _gameTime is available in both server and pixel-mode client
    if (Game.state && Game.state._gameTime !== undefined) time = Game.state._gameTime;
    // ... render using time ...
}
```

The client extrapolates `_gameTime` between server snapshots from its own
wall clock, so games that derive everything from `_gameTime` get smooth
motion at the client's render fps even though the server only ticks at
~10 Hz. Whenever a non-clock state change arrives, the snapshot's
`_gameTime` is treated as authoritative and the local clock is snapped
to it.

If your game needs additional state for rendering (player positions, scores, etc.), set them on `Game.state` in `update()` and read them back in `renderCanvas()`. Module-level variables are only updated on the server — they stay at their initial values on the pixel-mode client.

---

## Sharing your game

Host your `.js` file anywhere publicly accessible over HTTPS — a GitHub repo is the simplest option. Anyone running dev-null can then load it directly:

```
/game load https://github.com/you/your-repo/blob/main/mygame.js
```

GitHub blob URLs are automatically converted to raw download URLs. The file is cached locally in `.cache/` so it survives restarts; re-running the load command fetches the latest version.

---

## Widget Tree Layout (`layout`)

If your game defines `layout(playerID, width, height)`, it should return a tree of widget nodes. The framework renders these as real themed NC-style panels with proper borders, respecting the player's current theme. This is optional — games that only define `renderAscii()` work unchanged.

### Node types

| Type | Description | Key properties |
|------|-------------|----------------|
| `gameview` | Renders the raw `renderAscii()` output in this region | (none — calls `renderAscii(buf, playerID, x, y, w, h)` with the computed sub-area) |
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
layout: function(playerID, width, height) {
    return {
        type: 'hsplit',
        children: [
            {
                type: 'vsplit', weight: 1,
                children: [
                    { type: 'gameview', weight: 1 },              // raw renderAscii() in the main area
                    { type: 'panel', title: 'Stats', height: 5,   // NC panel at bottom
                      children: [
                          { type: 'label', text: 'HP: ' + hp + '/' + maxHp },
                          { type: 'label', text: 'Score: ' + score }
                      ] }
                ]
            },
            {
                type: 'panel', title: 'Players', width: 25,       // NC panel on the right
                children: [{
                    type: 'table',
                    rows: playerRows
                }]
            }
        ]
    };
}
```

This renders the raw game view in the top-left, a stats panel below it, and a players panel on the right — all using the framework's themed NC borders. Existing games that don't define `layout` are unaffected.

---

## Shaders (post-processing)

Shaders are per-player scripts that modify the rendered screen buffer before it's displayed. They run after all game/lobby content is drawn but before menus and dialogs are overlaid.

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

Shaders are also accessible from the **File → Shaders...** menu.

### Writing a shader

Shaders are JS files in `dist/shaders/`. A shader exports a global `Shader` object with a required `process(buf)` method:

```javascript
const Shader = {
    // Optional: called once when the shader is loaded.
    init() { },

    // Optional: called every tick with elapsed seconds.
    // Use this to animate shader effects over time.
    update(dt) { },

    // Required: called every frame with the full screen buffer.
    process(buf) {
        for (var y = 0; y < buf.height; y++) {
            for (var x = 0; x < buf.width; x++) {
                var p = buf.getPixel(x, y);
                if (p) {
                    // Swap foreground and background
                    buf.setChar(x, y, p.char, p.bg, p.fg, p.attr);
                }
            }
        }
    },

    // Optional: called when the shader is unloaded.
    unload() { }
};
```

### Buffer API

The `buf` object passed to `process()` provides:

| Method | Description |
|--------|-------------|
| `buf.width` | Buffer width in columns |
| `buf.height` | Buffer height in rows |
| `buf.getPixel(x, y)` | Returns `{char, fg, bg, attr}` or `null` if out of bounds. Colors are `"#rrggbb"` strings or `null` (default). |
| `buf.setChar(x, y, ch, fg, bg, attr)` | Set a single cell. `ch` is a string (first character used). Colors are `"#rrggbb"` or `null`. |
| `buf.writeString(x, y, text, fg, bg, attr)` | Write text starting at (x, y) |
| `buf.fill(x, y, w, h, ch, fg, bg, attr)` | Fill a rectangle |
| `buf.recolor(x, y, w, h, fg, bg, attr)` | Change colors/attributes without changing characters |

### Global constants

| Constant | Value | Description |
|----------|-------|-------------|
| `ATTR_NONE` | 0 | No attributes |
| `ATTR_BOLD` | 1 | Bold text |
| `ATTR_FAINT` | 2 | Dim/faint text |
| `ATTR_ITALIC` | 4 | Italic text |
| `ATTR_UNDERLINE` | 8 | Underlined text |
| `ATTR_REVERSE` | 16 | Reverse video |

### Global functions

| Function | Description |
|----------|-------------|
| `log(msg)` | Log a debug message (visible in server log) |
| `now()` | Server time in epoch milliseconds |

### Bundled shaders

| Shader | Effect |
|--------|--------|
| `invert` | Swaps foreground and background colors |
| `scanlines` | Animated scrolling scanlines (CRT effect) |
| `crt` | Green-on-black retro terminal look |
| `rainbow` | Cycles border/box-drawing characters through a flowing rainbow |

### Execution order

Multiple shaders run in the order they were loaded. Use `/shader up` and `/shader down` to reorder. Each shader receives the buffer as modified by the previous shader in the chain.
