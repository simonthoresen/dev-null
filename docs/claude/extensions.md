# Extensions: Plugins, Shaders, Charmaps & Canvas

## Plugins (JS)

Per-player (or per-console) JavaScript extensions in `dist/plugins/`. Loaded with `/plugin load <name|url>`. Each player/console maintains their own plugin list -- plugins are not shared.

A plugin exports a `Plugin` object with an `onMessage(author, text, isSystem)` hook. The hook is called for every chat message (or log line, for console plugins). If it returns a non-empty string, that string is dispatched as if the player typed it -- starting with `/` means a command, otherwise it's sent as chat. Return `null` to do nothing.

**Loop prevention:** Messages originating from plugins are tagged with `IsFromPlugin: true` and are never fed back to plugin hooks. This prevents cross-plugin infinite loops (Plugin A -> chat -> Plugin B -> chat -> Plugin A ...). Same-player messages are also skipped (SSH only: `FromID` check). Command replies (`IsReply`) are always skipped too.

**Use cases:** auto-greeting bots, chat responders, server management scripts, auto-moderation.

**Global JS:** `log()` only (for debug output).

**Bundled plugins:** `greeter` (welcomes new players), `echo` (echoes `!echo` messages).

## Shaders (JS / Go)

Per-player (or per-console) post-processing scripts in `dist/shaders/`. Loaded with `/shader load <name|url>`. Each player/console maintains their own ordered shader list. Shaders run in sequence on the fully-rendered `ImageBuffer` **after** the screen is composed but **before** overlays (menus, dialogs) and `ToString()`.

A JS shader exports a `Shader` object with a required `process(buf, time)` method. `time` is total elapsed seconds since server start (deterministic, same value on server and client for local rendering). `buf` exposes:
- `width`, `height` -- buffer dimensions
- `getPixel(x, y)` -> `{char, fg, bg, attr}` or `null` -- read a cell
- `setChar(x, y, ch, fg, bg, attr)` -- write a cell
- `writeString(x, y, text, fg, bg, attr)` -- write text
- `fill(x, y, w, h, ch, fg, bg, attr)` -- fill rectangle
- `recolor(x, y, w, h, fg, bg, attr)` -- change colors without changing characters

Optional hooks: `init()` (called once on load), `unload()` (called on removal). **Shaders must be stateless** -- all time-based effects must derive from the `time` parameter passed to `process()`. This ensures shaders are pure functions of (buffer x time), replicable on the client for local rendering.

**Go shaders** implement `domain.Shader` interface: `Name() string`, `Process(buf *ImageBuffer, elapsed float64)`, `Unload()`. Compiled into the binary.

**Commands:** `/shader` (list), `/shader load <name>`, `/shader unload <name>`, `/shader list`, `/shader up <name>`, `/shader down <name>`.

**Menu:** File -> Shaders... shows active shaders with order and available shaders.

**Bundled shaders:** `invert` (swap fg/bg), `scanlines` (animated scrolling scanlines), `crt` (green-on-black retro terminal), `rainbow` (flowing rainbow on box-drawing borders).

| Package | Role |
|---------|------|
| `internal/engine/shader.go` | JS shader runtime: `jsShader`, `LoadShader()`, `applyShaders()`, JS buffer wrapper with `getPixel`/`setChar`/`recolor` |

## Charmaps (Sprite-Based Rendering)

Games can use **charmap-based sprite rendering** by mapping Unicode Private Use Area codepoints (U+E000-U+F8FF) to sprites in a sprite sheet. Regular SSH clients show tofu/blank for PUA codepoints; the custom `dev-null-client` renders them as sprites.

**Charmap format:** Each charmap lives in `dist/charmaps/<name>/` with a `charmap.json` and an atlas PNG:
```json
{
  "name": "pacman",
  "version": 1,
  "cellWidth": 16,
  "cellHeight": 16,
  "atlas": "atlas.png",
  "entries": [
    {"codepoint": 57344, "x": 0, "y": 0, "w": 16, "h": 16, "name": "player"},
    {"codepoint": 57345, "x": 16, "y": 0, "w": 16, "h": 16, "name": "ghost"}
  ]
}
```

**JS game usage:** Set `Game.charmap = "pacman"`, then use PUA codepoints in render:
```js
var Game = {
    charmap: "pacman",
    // ...
};
buf.setChar(x, y, "\uE000", "#ffff00", "#000000"); // renders as sprite in custom client
```

**PUA constants:** `PUA_START` (0xE000) and `PUA_END` (0xF8FF) are available in JS.

**Enhanced client protocol:** The server detects the custom client via `DEV_NULL_CLIENT=enhanced` SSH env var, then sends charmap data and viewport bounds using in-band OSC escape sequences that regular terminals silently ignore:
- `\x1b]ns;charmap;<base64 JSON>\x07` -- charmap definition (sent once on game load)
- `\x1b]ns;atlas;<base64 gzipped PNG>\x07` -- sprite sheet (sent once on game load)
- `\x1b]ns;viewport;<x>,<y>,<w>,<h>\x07` -- game viewport bounds (sent every frame)
- `\x1b]ns;frame;<base64 gzipped PNG>\x07` -- canvas frame (sent every frame when canvas mode active)
- `\x1b]ns;gamesrc;<filename>;<base64 gzipped JS>\x07` -- game source file (sent once per file on game load)
- `\x1b]ns;state;<base64 gzipped JSON>\x07` -- Game.state delta (sent when state changes)
- `\x1b]ns;mode;<local|remote>\x07` -- switch client rendering mode

**Rendering rules:** Charmaps apply only to the game viewport. NC chrome (menus, dialogs, chat, status bars) always renders as text. Drop shadows on PUA cells clear the sprite and fill with shadow color.

## Canvas Rendering (Server-Side 2D Graphics)

Games can define an optional `renderCanvas(ctx, playerID, w, h)` hook for server-side 2D canvas rendering. The server rasterizes using `fogleman/gg` and sends PNG frames to enhanced clients. Regular SSH players still see the cell-based `render()` output.

**Canvas API (subset of HTML5 Canvas2D):**
- **State:** `save()`, `restore()`
- **Transforms:** `translate(x,y)`, `rotate(angle)`, `scale(sx,sy)`
- **Style:** `setFillStyle(color)`, `setStrokeStyle(color)`, `setLineWidth(w)`
- **Rectangles:** `fillRect(x,y,w,h)`, `strokeRect(x,y,w,h)`, `clearRect(x,y,w,h)`
- **Paths:** `beginPath()`, `closePath()`, `moveTo(x,y)`, `lineTo(x,y)`, `arc(x,y,r,start,end)`, `fill()`, `stroke()`
- **Curves:** `quadraticCurveTo(cpx,cpy,x,y)`, `bezierCurveTo(cp1x,cp1y,cp2x,cp2y,x,y)`
- **Circles:** `fillCircle(x,y,r)`, `strokeCircle(x,y,r)`, `fillEllipse(x,y,rx,ry)`, `strokeEllipse(x,y,rx,ry)`
- **Text:** `fillText(text,x,y)`
- **Pixels:** `setPixel(x,y,color)`
- **Constants:** `PI`, `TAU`

**JS game usage:**
```js
var Game = {
    renderCanvas: function(ctx, playerID, w, h) {
        ctx.setFillStyle("#000000");
        ctx.fillRect(0, 0, w, h);
        ctx.setFillStyle("#ffff00");
        ctx.fillCircle(w/2, h/2, 20);
    },
    render: function(buf, playerID, ox, oy, w, h) {
        // Optional: cell-based overlay (score, HUD) on top of canvas
        buf.writeString(ox, oy, "Score: 42", "#fff", null);
    }
};
```

**Canvas scale:** Admin sets the scaling factor with `/canvas scale <n>` (pixels per cell). `/canvas info` shows current scale, pixel dimensions, and estimated bandwidth per user. `/canvas off` disables canvas rendering. Scale is stored in `CentralState.CanvasScale`. Canvas dimensions = viewport cells x scale. The `/canvas` command shows bandwidth estimates at the console's viewport size.

**Render modes:** Each player has a per-player `renderMode` (type `domain.RenderMode`) selectable from the **Graphics** menu. Four modes exist:

| Mode | Description | Requirements |
|------|-------------|-------------|
| **Text** | Standard cell-based `game.Render()` only | Always available |
| **Quadrant** | Canvas → Unicode quadrant block chars (U+2580–U+259F), 2×2 pixels per cell | Game defines `renderCanvas` |
| **Canvas** | Server renders canvas → PNG frames streamed via OSC | Game defines `renderCanvas` + enhanced client + canvasScale > 0 |
| **Canvas HD** | Client renders canvas locally from game JS + state JSON | Same as Canvas |

On game load, `bestRenderMode()` auto-selects the highest-fidelity mode (Canvas HD > Canvas > Quadrant > Text). Modes not supported by the client or game are shown disabled in the Graphics menu. The quadrant renderer (`internal/render/quadrant.go`) partitions each 2×2 pixel block into optimal fg/bg color groups using exhaustive 2-means clustering.

**Canvas HD** sends game JS source files and state deltas via OSC; the client runs the game in its own goja VM and renders canvas at display-pixel resolution (no compression artifacts, scales to any resolution). **Canvas** streams server-rendered PNG frames — higher bandwidth but works without client-side JS execution. Both modes fill the viewport with `render.CanvasCell` placeholder cells that the client treats as transparent; menus/dialogs that overlap the viewport replace placeholders with real cells, rendering on top of the canvas.

Per-player state: `Model.renderMode` enum + `Model.oscModeSent` flag. Client-side: `renderer.go` checks `renderMode == "local"` to decide whether to use the local JS renderer or the server-sent canvas frame.

| Package | Role |
|---------|------|
| `common/charmap.go` | CharMapDef/CharMapEntry types, PUA constants, JSON loader |
| `common/osc.go` | OSC escape sequence encoding for charmap/atlas/viewport/frame, bandwidth estimator |
| `internal/engine/canvas.go` | Headless Canvas2D context (fogleman/gg) exposed to goja |
| `internal/client/` | SSH connection, ANSI parser, Ebitengine renderer, local JS game renderer |
| `internal/client/localrender.go` | Client-side goja runtime: loads game JS, sets Game.state from server deltas, calls render/renderCanvas locally |
