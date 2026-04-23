# Game contract v2 — design

Status: **planned, not yet implemented.** This document locks the shape we
agreed on so the migration can proceed in reviewable pieces.

## Why

The v1 contract has landed a string of bugs whose common root is that the
framework's boundaries live in comments, not in code:

| Bug | Root cause |
|-----|------------|
| Wolf3d "Spectating" under local render | `renderCanvas` received a client-supplied `playerID` that had to be looked up in `state.players` — no framework resolution |
| Laser not visible | Ephemeral event lived in state with a TTL shorter than a tick |
| Render-path slog feedback loop | Global `log()` / `chat()` / `teams()` callable from render; only a runtime stack-walker keeps it out |
| Module-level state silently desyncs on client | Nothing about `Game.state = …` binds the author to put gameplay state there |
| `playerID` flag mismatch | Client and server had two different ideas of what `playerID` meant; no contract said which one renders see |

The shared pattern: **the framework hands the author globals and loose
conventions, and relies on vigilance to avoid misuse**. v2 makes the contract
structural.

## The contract

```js
Game = {
    gameName:  "…",
    teamRange: { min: 1, max: N },

    // One-time setup on the server. Returns initial state.
    init: function(ctx) { … return initialState; },

    // Server-only. May mutate state, may side-effect via ctx, may emit events.
    // events is a flat, ordered list this tick; input/join/leave/tick all arrive here.
    update: function(state, dt, events, ctx) { … },

    // Optional server-only hooks. Same (state, ctx) signature.
    begin: function(state, ctx) { … },
    end:   function(state, ctx) { … },

    // Render. Runs on server and client with identical semantics.
    // NEVER receives ctx. state is frozen. me is pre-resolved.
    // A game implements whichever makes sense:
    renderCanvas: function(state, me, canvas) { … },
    renderAscii:  function(state, me, cells)  { … },

    // Optional, pure, no ctx.
    statusBar:  function(state, me) { return "…"; },
    commandBar: function(state, me) { return "…"; }
};
```

### `ctx` (server-only)

Side-effecty capabilities live here. Render never receives `ctx`, so render
cannot call any of these. The client's VM doesn't bind `ctx` at all — calling
`ctx.anything` in render throws.

| Method | Purpose |
|---|---|
| `ctx.chat(msg)` | broadcast a chat message |
| `ctx.chatPlayer(pid, msg)` | direct-message one player |
| `ctx.playSound(file, loop)` | trigger sound playback on clients |
| `ctx.stopSound(file)` | stop one or all sounds |
| `ctx.midiNote(ch, note, vel, durMs)` | broadcast a MIDI note |
| `ctx.midiNotePlayer(pid, ch, …)` | direct-to-one-player MIDI |
| `ctx.midiProgram`, `ctx.midiCC`, `ctx.midiPitch`, `ctx.midiSilence` | MIDI control |
| `ctx.emit(kind, payload)` | broadcast a one-shot ephemeral event (replaces TTL'd state) |
| `ctx.gameOver(results)` | signal end-of-game with ranked results |
| `ctx.showDialog(pid, dlg)` | open a modal dialog on one client |
| `ctx.registerCommand(def)` | register a slash-command handler |
| `ctx.log(msg)` | debug logging from update |

### `events`

A flat array passed to every `update` call. Replaces the scattered
`onInput` / `onPlayerJoin` / `onPlayerLeave` hooks. Each event is a plain
object:

```js
{ type: "input",  playerID, key }
{ type: "join",   playerID, playerName }
{ type: "leave",  playerID }
{ type: "tick"  }                          // always present exactly once
{ type: "command", playerID, name, args }  // registered via ctx.registerCommand
```

Games iterate the list in order. One update per tick; events since the
previous tick arrive batched.

### `me`

Framework-resolved before render. In the simplest shape `me = state.players[sessionID]`.
Games that store players under a different key (or don't have per-player
records at all, like orbits-style games with per-team cameras) provide a hint:

```js
Game = {
    resolveMe: function(state, playerID) {
        var teamIdx = state.playerTeams[playerID];
        return { id: playerID, teamIdx: teamIdx, camera: state.cameras[teamIdx] };
    },
    …
};
```

If `resolveMe` isn't defined the framework defaults to
`state.players[playerID] || null`. If `me` resolves to null, the framework
draws a "connecting…" splash into the viewport and does not invoke render.
No game writes its own spectator fallback.

### `canvas` and `cells`

Native primitives, not a polymorphic "draw" layer. A game implements
`renderCanvas` if it wants pixel output, `renderAscii` if it wants cell
output, or both (with potentially different scenes — wolf3d pixels =
first-person raycaster, wolf3d ASCII = top-down minimap).

`canvas` exposes the full JSCanvas surface (fillRect, fillCircle, fillText,
beginPath/moveTo/lineTo/stroke, fillTriangle3DLit, gradients, etc.) plus a
narrow debug escape hatch: `canvas.log(msg)` prints to stderr and never
appears in game state.

`cells` exposes the ImageBuffer surface: `setChar`, `writeString`, `fill`,
`paintANSI`, plus the `ATTR_*` constants (`cells.ATTR_BOLD`, etc.) and
`cells.log`. Constants are scoped to the object so they only exist where
they apply.

### Framework-injected state

The server populates these keys on `state` each tick before marshalling:

| Key | Shape | Notes |
|-----|-------|-------|
| `state.teams` | `[{name, color, players: [{id, name}]}]` | replaces the `teams()` global; authors read it like any other state |
| `state._t` | seconds since game start | already injected in v1; carried forward |

Games may not write to these keys. In dev mode they're frozen; in prod the
framework overwrites them after update returns.

### Ephemeral events

A laser flash exists for ~300ms. Putting it in state means setting a TTL,
worrying about whether the TTL outlives the tick, worrying about whether
it survives a serialization round-trip. Move it off state entirely:

```js
// In update, when a player fires:
ctx.emit("laser", { from: {x,z}, to: {x,z}, color, ttl: 0.3 });
```

The framework broadcasts the event once to every client. Each client queues
it and replays against its own clock for `ttl` seconds. The client's render
function sees queued events via a separate parameter:

```js
renderCanvas: function(state, me, canvas, events) {
    for (var e of events) {
        if (e.kind === "laser") drawLaser(canvas, e.payload);
    }
}
```

Events never enter the diff machinery; they're point-in-time, fire-and-forget.

## State transport

### Top-level key diff

The server keeps `lastSentKeys: map[string][]byte` per player. Each tick:

1. Marshal each top-level key of `state` individually.
2. For each key whose marshaled bytes differ from `lastSentKeys[k]`, add it
   to the outgoing patch.
3. For each key removed from `state`, add `k: null` to the patch.
4. If the patch is empty, skip the broadcast.
5. gzip the patch, send as `ns;state-patch`.
6. Update `lastSentKeys` with the new bytes for every key we sent.

On first send (lastSentKeys is empty), send the full state via the existing
`ns;state` path so the client has a baseline.

On reconnect, mode switch (remote→local), or game load, clear `lastSentKeys`
so the next broadcast is a full snapshot.

### Client side

`SetState(bytes)` replaces Game.state (existing behaviour, used for
baseline/reconnect).
`SetStatePatch(bytes)` merges: for each key, either set `state[k] = value` or
`delete state[k]` if value is null. Does not recurse — depth-1 merge only.

### Wire format

```
ns;state;<base64(gzip(json(state)))>\x07        # full baseline
ns;state-patch;<base64(gzip(json(patch)))>\x07  # delta
```

## Impact on bugs we've hit

| Bug | How v2 prevents it |
|-----|-------------------|
| "Spectating" under local render | Framework resolves `me`; if it can't, it draws the splash itself. Game never sees an unresolved playerID. |
| Laser invisible | Lasers are ephemeral events, not state. No TTL. |
| Render-path slog/chat/teams calls | Not callable — render doesn't receive ctx, client VM doesn't bind them. |
| Module-level gameplay state | No API to "keep state in a module var". State arrives as a function parameter; there's nowhere else to put it. |
| PlayerID mismatch | Render receives `me`, not a string; the framework owns the ID→record lookup. |
| Globals leaking into render | Constants like ATTR_BOLD move onto `cells`. No relevant globals remain. |

## Migration plan

Six commits, roughly in order:

1. **Diff transport** — add `EncodeStatePatchOSC`, server-side key diff,
   client-side merge. Works for v1 state too (treat v1 state as one big
   top-level key). ~150 lines Go.
2. **Framework-injected `state.teams`** — server populates it each tick
   before marshalling. v1 games that read `teams()` still work.
3. **v2 contract dispatch** — Runtime detects v2 (by marker field
   `Game.contract === 2` or by the presence of the new-shape `init`/`update`
   signatures), builds ctx, assembles events, resolves me, passes
   canvas/cells objects to render. v1 games keep working via the old path.
4. **Ephemeral events pipe** — `ctx.emit`, OSC `ns;event`, client replay
   queue. Separate from state.
5. **Migrate games** — one commit per game, smallest first (orbits, cube,
   example) to shake out bugs, heaviest last (wolf3d, voyage, quake).
   `orbits2.js` gets rewritten against the real v2 shape in this phase and
   `orbits.js` is deleted.
6. **Retire v1** — remove v1 dispatch, global stubs in localrender, slog
   runtime guard, `teams()` / `chat()` / `midiNote()` JS bindings. The
   shape of `engine.Runtime` gets simpler here.

Steps 1 and 2 are orthogonal to the contract change and land value
immediately even before anything else moves. Steps 3–6 are sequential.

## Decisions recorded

- `ctx` is a dedicated third object, not part of state or events.
- `state.teams` is framework-injected read-only.
- Client VM binds no ctx at all — render calls to ctx throw, not no-op.
- `canvas.log` and `cells.log` exist as narrow debug escape hatches;
  everything else on ctx is strictly server-side.
- Top-level key diff for state; no deep diff, no content-addressed blobs
  yet — defer those until a game's scale demands them.
- `orbits2.js` is a pre-v2 spike and will be deleted during step 5.
