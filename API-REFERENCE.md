# null-space API Reference

This document explains how to write games for null-space. Games are plain JavaScript files (ES5-compatible, no modules). Drop your file in `dist/games/`, or share it via URL — no build step required.

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
null-space --local --game example
null-space --local --game https://github.com/you/repo/blob/main/mygame.js
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

A game file must define a global `Game` object. `init` and `render` are required; all other hooks are optional.

```js
var Game = {

    // --- Core hooks ---

    // Called once per server tick (~100ms) with the time elapsed since the last
    // update in seconds (typically ~0.1). Put all game logic here — movement,
    // timers, collision detection, gameOver() calls. This is called exactly once
    // per tick regardless of how many players are connected.
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
    // Called per player on every render tick (~10 fps). Must be pure rendering — no game state mutation.
    render: function(buf, playerID, ox, oy, width, height) {
        buf.writeString(0, 0, "Hello!", "#FFFFFF", null);
    },

    // Returns a declarative widget tree describing the game window.
    // If defined, the framework renders real themed NC panels/labels instead of
    // using the raw render() string. Games can embed {type: "gameview"} nodes to
    // include the raw render() output within the layout. If layout returns null
    // or is not defined, the framework falls back to render(). See "Widget Tree Layout" below.
    layout: function(playerID, width, height) {
        return {
            type: 'hsplit',
            children: [
                { type: 'gameview', weight: 1 },           // raw render() here
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

    // Called before splash with persisted state (or null on first run). Mandatory.
    // teams() global is available.
    init: function(savedState) {
        if (savedState) {
            // restore previous state
        }
    },

    // Called at splash→playing transition. Set up game state here. Optional.
    start: function() {
        // game begins — teams() available
    },

    // --- Suspend/resume (optional, requires canSuspend: true) ---

    // Set to true to enable /game suspend and the Resume Game menu.
    canSuspend: true,

    // Called when admin runs /game suspend. Return session state to persist.
    // This is separate from gameOver state — suspend saves are per-session.
    suspend: function() {
        return { board: board, turn: turn };
    },

    // Called when game is resumed. sessionState is null for warm resume
    // (runtime still in memory), or the saved state for cold resume.
    resume: function(sessionState) {
        if (sessionState) {
            board = sessionState.board;
            turn = sessionState.turn;
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

    render: function(buf, playerID, ox, oy, width, height) {
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

    // Custom splash screen rendering. Optional — if omitted or returns false,
    // the framework renders a figlet game name centered in the viewport.
    // Same buf API as render(). Return true to indicate custom rendering was done.
    // renderSplash: function(buf, playerID, ox, oy, width, height) { ... return true; },

    // Custom game-over screen rendering. Optional — if omitted or returns false,
    // the framework renders a figlet "GAME OVER" title with ranked results.
    // results is an array of {name, result} objects in ranked order.
    // renderGameOver: function(buf, playerID, ox, oy, width, height, results) { ... return true; },

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
| `gameOver()` | Signals that the game has ended. Transitions to the game-over screen. |
| `gameOver(results)` | Same as above, with ranked results displayed on the game-over screen. `results` is an array of `{ name, result }` in ranked order. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). |
| `gameOver(results, state)` | Same as above, plus persists `state` to `dist/state/<gamename>.json` for the next run. Received via `config.savedState` in `init()`. |
| `now()` | Returns the server time as epoch milliseconds (same as `Date.now()` but uses the framework's central clock, which is mockable in tests). Available in both games and plugins. |
| `include(name)` | Evaluates another `.js` file from the same directory as the game file. Used for multi-file games in `games/<name>/` folders. The `.js` extension is added automatically if omitted. Each file is only included once (idempotent). Path traversal (`..`) is rejected. |

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
- **Playing**: Normal game mode — `update(dt)` is called once per tick, then `render()`/`layout()`, `onInput()`, `statusBar()`, `commandBar()` are called per player.
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

Games can persist data between runs. Saved state is stored as JSON in `dist/state/<gamename>.json`.

**Saving**: Pass state as the second argument to `gameOver(results, state)`. Only saved when the game ends naturally — manual `/game unload` does not persist state.

**Loading**: Receive previous state as the argument to `init(savedState)` (null on first run).

## Suspend/resume

Games can support suspend/resume for long-running sessions (e.g. RPGs). This is separate from `gameOver` state — suspend saves are per-session, while `gameOver` state is global (high scores, etc.). Multiple suspended sessions of the same game can coexist.

**Opt in**: Set `canSuspend: true` on the `Game` object.

```javascript
var Game = {
    canSuspend: true,

    suspend: function() {
        // Return session state to persist (similar to gameOver's 2nd arg).
        return { board: board, turn: turn, scores: scores };
    },

    resume: function(sessionState) {
        // sessionState is null for warm resume (runtime still in memory),
        // or the saved state object for cold resume (server was restarted).
        if (sessionState) {
            board = sessionState.board;
            turn = sessionState.turn;
            scores = sessionState.scores;
        }
        // Re-start timers, etc.
    },

    // ... other hooks ...
};
```

| Hook | When called | Return value |
|------|------------|--------------|
| `suspend()` | Admin runs `/game suspend` | Session state object (persisted to JSON) |
| `resume(sessionState)` | Game is resumed | — |

**Commands**:
- `/game suspend [saveName]` — suspends the active game. Auto-generates a timestamp name if omitted.
- `/game resume <gameName/saveName>` — resumes a saved session. Tab-completes against existing saves.

**Save location**: `dist/state/saves/<gameName>/<saveName>.json`

**Warm vs cold resume**: If the server hasn't restarted, the JS runtime is still alive and `resume(null)` is called. If the server was restarted, the game is loaded fresh (`init` + `start` are called first), then `resume(sessionState)` is called with the saved state.

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
│ chat (remaining rows, min 5)       │
├────────────────────────────────────┤
│ command bar (1 row)                │  ← Game.commandBar() when idle; text input on Enter
├────────────────────────────────────┤
│ status bar (1 row)                 │  ← framework: server time (always present)
└────────────────────────────────────┘
```

- `width` = full terminal width
- `height` = `width * 9 / 16` (clamped down if terminal is too short to leave 5 rows for chat)
- Return exactly `height` newline-separated rows from `render()`. Fewer rows are padded; more are clipped.
- The menu bar is full width — `gameName` can use it entirely.

---

## Tips

**State is global and shared.** All players see the result of the same `Game` object — there is no per-player instance. Design your state with this in mind (`var players = {}`).

**`render()` is called per player per tick.** Keep it fast and side-effect-free — no game state mutation. Put all game logic in `update(dt)` instead.

**`update(dt)` is called once per tick.** The `dt` argument is seconds since the last update (typically ~0.1). All game logic — movement, timers, collision, gameOver() calls — belongs here.

**Rendering is character-based.** Each character is one cell wide. For box-drawing or emoji that span multiple columns, count display width carefully — the framework does not reflow.

**ImageBuffer API.** The `buf` parameter in `render()` supports these methods:

| Method | Description |
|--------|-------------|
| `buf.setChar(x, y, ch, fg, bg)` | Set one character. `fg`/`bg` are `"#RRGGBB"` or `null` (default). |
| `buf.writeString(x, y, text, fg, bg)` | Write plain text starting at (x, y). |
| `buf.fill(x, y, w, h, ch, fg, bg)` | Fill a rectangle with a character. |
| `buf.width` / `buf.height` | Dimensions of the game viewport. |

All methods accept an optional trailing `attr` parameter (bitmask): `ATTR_BOLD`, `ATTR_FAINT`, `ATTR_ITALIC`, `ATTR_UNDERLINE`, `ATTR_REVERSE`. Coordinates are relative to the viewport (0,0 = top-left).

```js
render: function(buf, playerID, ox, oy, width, height) {
    buf.writeString(0, 0, "Hello, " + playerID, "#00FF00", null);
    buf.setChar(5, 2, "@", "#FFFF00", "#000080", ATTR_BOLD);
}
```

**ANSI escape codes** still work in `statusBar()` and `commandBar()` output.

---

## Sharing your game

Host your `.js` file anywhere publicly accessible over HTTPS — a GitHub repo is the simplest option. Anyone running null-space can then load it directly:

```
/game load https://github.com/you/your-repo/blob/main/mygame.js
```

GitHub blob URLs are automatically converted to raw download URLs. The file is cached locally in `.cache/` so it survives restarts; re-running the load command fetches the latest version.

---

## Widget Tree Layout (`layout`)

If your game defines `layout(playerID, width, height)`, it should return a tree of widget nodes. The framework renders these as real themed NC-style panels with proper borders, respecting the player's current theme. This is optional — games that only define `render()` work unchanged.

### Node types

| Type | Description | Key properties |
|------|-------------|----------------|
| `gameview` | Renders the raw `render()` output in this region | (none — calls `render(buf, playerID, x, y, w, h)` with the computed sub-area) |
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
                    { type: 'gameview', weight: 1 },              // raw render() in the main area
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

    // Optional: called every tick (~100ms) with elapsed seconds.
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
