# Game Lifecycle, State & Suspend/Resume

## Game Lifecycle
```
LOBBY (teams + chat) -> STARTING -> PLAYING -> GAME OVER -> LOBBY
                                       |
                                   SUSPENDED -> LOBBY
                                       ^
                                    RESUME
```
1. **Lobby**: Players configure teams, chat. Admin loads game with `/game load <name>`.
2. **Load**: Framework snapshots teams, loads persistent state from `dist/state/<gameName>.json`, calls `load(persistentState)`. `teams()` returns game teams.
3. **Starting**: Framework shows a figlet of the game name. Admin presses Enter to start, or auto-starts after 10s.
4. **Starting→Playing**: Framework calls `begin()`. Game sets up its playing state.
5. **Reconnect**: If a player disconnects mid-game and reconnects with the same name, they rejoin automatically.
6. **Playing**: Normal game mode. Game calls `gameOver(results)` when done.
7. **Game Over**: Framework renders ranked results screen. All players press Enter or 15s auto-transition. `end()` hook fires, then `unload()` saves persistent state.
8. Back to **Lobby** — game unloaded.

Late joiners see the lobby and can chat but don't join the active game. Lobby teams are independent from game teams — players can freely organize for the next round while a game is running.

## Suspend/Resume

Any playing game can be suspended. There is no opt-in flag required.

```
Suspend:  suspend() → session snapshot → dist/state/saves/<game>/<save>.json
          unload()  → persistent state → dist/state/<game>.json

Resume:   load(persistentState)   ← high scores etc. restored first
          resume(sessionState)    ← board/positions etc. restored (no begin())
```

**JS hooks:**
- `suspend()` — return the mid-session snapshot (board state, current scores, etc.) to store in the save file. Return `undefined`/`null` if the game has no session state worth saving.
- `resume(sessionState)` — called **instead of `begin()`** when restoring from a save. `sessionState` is the value previously returned by `suspend()`. If this hook is not defined, falls back to calling `begin()` (existing games without resume support still work, they just start fresh).

**Note:** `unload()` is also called during suspend (after `suspend()`) to save persistent state immediately — so high scores are not lost even if the save is later deleted without resuming.

**Save files**: `dist/state/saves/<gameName>/<saveName>.json` — contains team snapshot, disconnected player map, and session state from `suspend()`. Deleted after successful resume.

**Commands**:
- `/game suspend [saveName]` — admin only. Auto-generates timestamp name if omitted.
- `/game resume <gameName/saveName>` — admin only. Tab-completes saved sessions.
- File → Saves... menu — lists all saves; Load (chrome admin) or Remove (console).

## State Separation

| Hook | Called when | Returns | Stored in |
|------|-------------|---------|-----------|
| `load(persistentState)` | Every fresh load AND before resume | — | — |
| `begin()` | Starting→Playing (fresh load only) | — | — |
| `unload()` | Game-over, /game unload, AND after suspend | Persistent state (high scores, unlocks) | `dist/state/<game>.json` |
| `suspend()` | /game suspend (before unload) | Session state (board, current score) | suspend save file |
| `resume(sessionState)` | /game resume (instead of begin) | — | — |

Example pattern:
```js
var Game = {
    state: { score: 0, highScore: 0 },

    load: function(saved) {
        // Receives persistent state on EVERY load (fresh and resume).
        if (saved) Game.state.highScore = saved.highScore || 0;
    },
    begin: function() {
        Game.state.score = 0; // fresh start
    },
    unload: function() {
        // Returns persistent state — survives across all sessions.
        if (Game.state.score > Game.state.highScore)
            Game.state.highScore = Game.state.score;
        return { highScore: Game.state.highScore };
    },
    suspend: function() {
        // Returns session snapshot — restored on resume.
        return { score: Game.state.score };
    },
    resume: function(saved) {
        // Called instead of begin() on /game resume.
        if (saved) Game.state.score = saved.score;
    },
};
```

## Teams

Players manage teams in the lobby panel (right side, fixed 32 chars). New players start **unassigned** (shown under "Unassigned" at the top of the team list). Tab switches focus between chat and team panel. Navigation in team panel:
- **Down** from unassigned → join first team (or create one if none exist)
- **Down** from a team → move to team below
- **Down** from last team → create new "Team \<your name\>" (blocked if you're the sole member, to avoid drop/recreate churn)
- **Up** from a team → move to team above
- **Up** from first team → become unassigned
- **Enter** (first player in team) → rename team
- **Left/Right** (first player in team) → cycle team color

New teams default to "Team \<creator name\>" and the first unused palette color. Games can declare `teamRange: {min, max}` to enforce valid team counts. Games access teams via the `teams()` global, which returns `[{name, color, players: [{id, name}, ...]}, ...]`. Game teams are a snapshot taken at load time — lobby teams remain editable during a game. Unassigned players are excluded from the game snapshot.

## Central Clock (`internal/domain/clock.go`)

The framework provides a central `Clock` interface (`Now() time.Time`) used for all time-related operations. Games access it via the `now()` JS global (epoch milliseconds). In tests, inject a `MockClock` to control time. `Update(dt)` receives the real elapsed seconds between ticks.

## Game Over

Games call `gameOver(results)` where `results` is an array of `{ name, result }` in ranked order. The framework renders the game-over screen — games don't need to provide their own. `name` is the display name (player or team). `result` is a freeform string (e.g. `"4200 pts"`, `"1st"`, `"DNF"`). After all players acknowledge (or 15s timeout), `end()` then `unload()` are called and the server returns to lobby.
