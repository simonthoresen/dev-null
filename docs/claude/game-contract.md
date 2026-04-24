# Game contract

## Design rationale

The contract exists to enforce framework boundaries in code rather than in comments.

| Problem | How the contract prevents it |
|---------|------------------------------|
| Render-path slog/chat/teams calls | Render never receives `ctx`; calling those APIs throws a TypeError |
| "Spectating" view under local render | Framework resolves `me`; if it can't, it draws the splash itself — game never sees an unresolved playerID |
| Module-level gameplay state | State arrives as a function parameter; no API for module-level mutation |
| PlayerID mismatch between client and server | Render receives `me`, not a string; framework owns the ID→record lookup |
| Globals leaking into render | Constants like ATTR_BOLD live on `cells`, not in global scope |

## The contract

```js
Game = {
    gameName:  "…",
    teamRange: { min: 1, max: N },

    // One-time setup on the server. Returns initial state.
    init: function(ctx) { … return initialState; },

    // Server-only. May mutate state, may side-effect via ctx.
    // events is a flat, ordered list this tick; input/join/leave/tick arrive here.
    update: function(state, dt, events, ctx) { … },

    // Optional server-only hooks. Same (state, ctx) signature.
    begin: function(state, ctx) { … },
    end:   function(state, ctx) { … },

    // Render. Runs on server and client with identical semantics.
    // NEVER receives ctx. state is frozen. me is pre-resolved.
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
| `ctx.emit(kind, payload)` | broadcast a one-shot ephemeral event |
| `ctx.gameOver(results)` | signal end-of-game with ranked results |
| `ctx.showDialog(pid, dlg)` | open a modal dialog on one client |
| `ctx.registerCommand(def)` | register a slash-command handler |
| `ctx.log(msg)` | debug logging from update |

### `events`

A flat array passed to every `update` call. Each event is a plain object:

```js
{ type: "input",   playerID, key }
{ type: "join",    playerID, playerName }
{ type: "leave",   playerID }
{ type: "tick" }                          // always present exactly once
{ type: "command", playerID, name, args } // registered via ctx.registerCommand
```

Games iterate the list in order. One update per tick; events since the
previous tick arrive batched.

### `me`

Framework-resolved before render. In the simplest shape `me = state.players[sessionID]`.
Games that store players under a different key provide a hint:

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

### `canvas` and `cells`

`canvas` exposes the full JSCanvas surface (fillRect, fillCircle, fillText,
beginPath/moveTo/lineTo/stroke, fillTriangle3DLit, gradients, etc.) plus a
narrow debug escape hatch: `canvas.log(msg)` prints to stderr.

`cells` exposes the ImageBuffer surface: `setChar`, `writeString`, `fill`,
`paintANSI`, plus the `ATTR_*` constants (`cells.ATTR_BOLD`, etc.) and
`cells.log`. Constants are scoped to the object so they only exist where
they apply.

### Framework-injected state

The server populates these keys on `state` each tick before marshalling:

| Key | Shape | Notes |
|-----|-------|-------|
| `state.teams` | `[{name, color, players: [{id, name}]}]` | authors read it like any other state |
| `state._gameTime` | seconds since game start | always present after `begin()` |

Games may not write to these keys; the framework overwrites them after update returns.

### Ephemeral events

For point-in-time events (laser flashes, explosions) use `ctx.emit` rather
than storing them in state with a TTL:

```js
ctx.emit("laser", { from: {x,z}, to: {x,z}, color, ttl: 0.3 });
```

The framework broadcasts the event once to every client. Events never enter
the diff machinery; they're fire-and-forget.

## State transport

### Top-level key diff

The server keeps `lastSentKeys: map[string][]byte` per player. Each tick:

1. Marshal each top-level key of `state` individually.
2. For each key whose marshaled bytes differ from `lastSentKeys[k]`, add it to the patch.
3. For each key removed from `state`, add `k: null` to the patch.
4. If the patch is empty, skip the broadcast.
5. Gzip the patch, send as `ns;state-patch`.
6. Update `lastSentKeys` with the new bytes for every key sent.

On first send, send the full state via `ns;state` so the client has a baseline.
On reconnect, mode switch, or game load, clear `lastSentKeys` so the next broadcast is a full snapshot.

### Wire format

```
ns;state;<base64(gzip(json(state)))>\x07        # full baseline
ns;state-patch;<base64(gzip(json(patch)))>\x07  # delta
```

## Decisions recorded

- `ctx` is a dedicated third object, not part of state or events.
- `state.teams` is framework-injected read-only.
- Client VM binds no ctx at all — render calls to ctx throw, not no-op.
- `canvas.log` and `cells.log` exist as narrow debug escape hatches; everything else on ctx is strictly server-side.
- Top-level key diff for state transport; no deep diff.
