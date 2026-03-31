# null-space API Reference

This document explains how to write games and plugins for null-space. Both are plain JavaScript files (ES5-compatible, no modules). Drop your file in `dist/games/` or `dist/plugins/`, or share it via URL — no build step required.

---

## Quick orientation

| Concept | What it means |
|---------|---------------|
| **Game** | One active at a time; owns the viewport, status bar, and command bar |
| **Plugin** | Multiple active simultaneously; passive hooks; persists across game switches |
| **Lobby** | The state when no game is loaded; only chat is visible |
| **Viewport** | The rectangular region your game renders into (below the status bar, above chat) |

---

## Loading a game or plugin

```
# From a local file in dist/games/ or dist/plugins/
/game load example
/plugin load profanity-filter

# From a URL (GitHub blob or any HTTPS .js URL)
/game load https://github.com/you/repo/blob/main/mygame.js
/plugin load https://raw.githubusercontent.com/you/repo/main/myplugin.js

# Local mode (no SSH server)
null-space --local --game example
null-space --local --game https://github.com/you/repo/blob/main/mygame.js
null-space --local --plugins profanity-filter,https://github.com/you/plugin.js
```

URL-loaded files are cached in `dist/games/.cache/` or `dist/plugins/.cache/`. Re-loading the same URL always fetches the latest version.

---

## Writing a game

A game file must define a global `Game` object. `init` and `view` are required; all other hooks are optional.

```js
var Game = {

    // --- Core hooks ---

    // Called when a player disconnects mid-game.
    onPlayerLeave: function(playerID) {},

    // Called every time a player presses a key while in game mode (not while chatting).
    // key is a string: "up", "down", "left", "right", "enter", "esc", "space",
    // "a"–"z", "A"–"Z", "0"–"9", "ctrl+c", "ctrl+z", "f1"–"f12", etc.
    onInput: function(playerID, key) {},

    // Returns the game viewport as a plain string (newline-separated rows).
    // width and height are the exact dimensions available — fill them exactly
    // or the framework will pad/clip. Called on every render tick (~10 fps).
    view: function(playerID, width, height) {
        return "";
    },

    // Returns the text for the top status bar (1 row).
    // The framework appends a braille spinner at the right edge — do not include one.
    // Keep content shorter than width to avoid overwriting the spinner.
    statusBar: function(playerID) {
        return "My Game";
    },

    // Returns the idle hint shown in the command bar when the player is not chatting.
    // Example: "[↑↓←→] Move  [Enter] Chat"
    // Return "" to show the default hint.
    commandBar: function(playerID) {
        return "";
    },

    // --- Properties (all optional) ---

    // Display name for splash screen and status bar. If omitted, the filename stem is used.
    gameName: "My Awesome Game",

    // Supported team count range. The framework blocks loading if the lobby has
    // fewer or more teams. Zero means no constraint on that end.
    // Omit to allow any number of teams.
    teamRange: { min: 2, max: 4 },

    // Custom splash screen content (multi-line string). If omitted, the framework
    // renders the game name centered in a box. Can be set dynamically in init().
    splashScreen: "=== MY GAME ===\nPress Enter to start",

    // --- Lifecycle ---

    // Called before splash with persisted state (or null on first run). Mandatory.
    // teams() global is available. Can set Game.splashScreen dynamically.
    init: function(savedState) {
        if (savedState) {
            // restore previous state
        }
    },

    // Called at splash→playing transition. Set up game state here. Optional.
    start: function() {
        // game begins — teams() available
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

    view: function(playerID, width, height) {
        var lines = [];
        for (var y = 0; y < height; y++) {
            var row = "";
            for (var x = 0; x < width; x++) {
                var ch = ".";
                for (var id in players) {
                    if (players[id].x === x && players[id].y === y) {
                        ch = (id === playerID) ? "@" : "O";
                        break;
                    }
                }
                row += ch;
            }
            lines.push(row);
        }
        return lines.join("\n");
    },

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

## Writing a plugin

A plugin file must define a global `Plugin` object. All hooks are optional.

```js
var Plugin = {

    // Called for every chat message before it reaches the history.
    // Return the (possibly modified) msg to allow it, or return null to drop it.
    // msg fields: author (string), text (string), isPrivate (bool), toID (string), fromID (string)
    onChatMessage: function(msg) {
        return msg; // pass through unchanged
    },

    // Called when a player connects.
    onPlayerJoin: function(playerID, playerName) {},

    // Called when a player disconnects.
    onPlayerLeave: function(playerID) {},

    // Optional skin: overrides the framework chrome colors for all connected clients.
    // Only the first loaded plugin that defines a skin is used.
    // Omit any field (or the entire skin property) to keep the framework default for that slot.
    // Colors are CSS hex strings (e.g. "#ff79c6") or standard terminal color names.
    skin: {
        statusBg: "#5e81ac", // status bar background
        statusFg: "#eceff4", // status bar foreground
        chatBg:   "#2e3440", // chat area background
        chatFg:   "#d8dee9", // chat area foreground
        cmdBg:    "#3b4252", // command bar background (idle hint mode)
        cmdFg:    "#4c566a", // command bar foreground (idle hint mode)
        inputBg:  "#3b4252", // input box background (while typing)
        inputFg:  "#eceff4"  // input box foreground (while typing)
    }
};
```

### Minimal working example

```js
var BANNED = ["badword"];

function censor(text) {
    BANNED.forEach(function(word) {
        text = text.replace(new RegExp(word, "gi"), "*".repeat(word.length));
    });
    return text;
}

var Plugin = {
    onChatMessage: function(msg) {
        msg.text = censor(msg.text);
        return msg;
    }
};
```

### Registering plugin commands

Same as for games — call `registerCommand` at the top level. Commands are unregistered automatically when the plugin is unloaded.

```js
registerCommand({
    name: "mute",
    description: "Mute a player (admin only)",
    adminOnly: true,
    firstArgIsPlayer: true,
    handler: function(playerID, isAdmin, args) {
        if (args.length < 1) {
            chatPlayer(playerID, "Usage: /mute <player>");
            return;
        }
        // ... mute logic ...
        chat("[system] " + args[0] + " was muted.");
    }
});

var Plugin = { /* ... */ };
```

---

## Global functions

These are available in both games and plugins.

| Function | Description |
|----------|-------------|
| `log(message)` | Writes to the server log panel (never shown to players). Useful for debugging. |
| `chat(message)` | Broadcasts a system chat message to all players. `author` will be empty (renders as `[system] message`). |
| `chatPlayer(playerID, message)` | Sends a private message to one player. |
| `teams()` | Returns an array of `{ name, color, players: [{id, name}, ...] }`. During a game, returns the game teams snapshot. |
| `registerCommand(spec)` | Registers a slash command. See below. |
| `gameOver()` | Signals that the game has ended. Transitions to the game-over screen. |
| `gameOver(results)` | Same as above, with ranked results displayed on the game-over screen. `results` is an array of `{ name, result }` in ranked order. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). |
| `gameOver(results, state)` | Same as above, plus persists `state` to `dist/state/<gamename>.json` for the next run. Received via `config.savedState` in `init()`. |

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

- **Load**: Framework snapshots teams for the game (lobby stays independent), loads saved state, calls `init(savedState)`. `teams()` returns game teams. Game can set `Game.splashScreen` dynamically.
- **Splash screen**: If `splashScreen` is set, that content is rendered. Otherwise, the game name is displayed in a centered box. The admin can press Enter to skip, or it auto-starts after 10s.
- **Splash→Playing**: Framework calls `start()`. Game sets up its playing state.
- **Playing**: Normal game mode — `view()`, `onInput()`, `statusBar()`, `commandBar()` are called.
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

## Layout and sizing

**Lobby:**
```
┌────────────────────────┬───────────┐
│ status bar (full width)             │
├────────────────────────┬───────────┤
│ chat (70% width)       │ teams     │
│                        │ panel     │
│                        │ (30%)     │
├────────────────────────┴───────────┤
│ command bar (full width)            │
└─────────────────────────────────────┘
```

**In-game (playing):**
```
┌────────────────────────────────────┐
│ status bar (1 row)                 │  ← Game.statusBar(); spinner added at right edge
├────────────────────────────────────┤
│                                    │
│ game viewport (width × height)     │  ← Game.view(playerID, width, height)
│                                    │
├────────────────────────────────────┤
│ chat (remaining rows, min 5)       │
├────────────────────────────────────┤
│ command bar (1 row)                │  ← Game.commandBar() when idle
└────────────────────────────────────┘
```

- `width` = full terminal width
- `height` = `width * 9 / 16` (clamped down if terminal is too short to leave 5 rows for chat)
- Return exactly `height` newline-separated rows from `view()`. Fewer rows are padded; more are clipped.
- The status bar has **one reserved character at the right edge** for the braille spinner — keep content shorter than the full width.

---

## Tips

**State is global and shared.** All players see the result of the same `Game` object — there is no per-player instance. Design your state with this in mind (`var players = {}`).

**`view()` is called frequently.** Keep it fast. Do not make network calls or heavy computations inside `view`.

**Rendering is character-based.** Each character is one cell wide. For box-drawing or emoji that span multiple columns, count display width carefully — the framework does not reflow.

**ANSI escape codes work.** You can use ANSI color codes in `view()`, `statusBar()`, and `commandBar()` output. Example:

```js
view: function(playerID, width, height) {
    return "\x1b[32mHello, \x1b[33m" + playerID + "\x1b[0m";
}
```

**Plugins run in load order.** If two plugins both implement `onChatMessage`, they form a pipeline — each one receives the output of the previous. Returning `null` drops the message for all subsequent plugins too.

**Commands from different sources share one namespace.** If a game and a plugin both register `/score`, the second one wins. Use unique command names.

---

## Sharing your game or plugin

Host your `.js` file anywhere publicly accessible over HTTPS — a GitHub repo is the simplest option. Anyone running null-space can then load it directly:

```
/game load https://github.com/you/your-repo/blob/main/mygame.js
```

GitHub blob URLs are automatically converted to raw download URLs. The file is cached locally in `.cache/` so it survives restarts; re-running the load command fetches the latest version.
