package client

import (
	"encoding/json"
	"image"
	"log"
	"math"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/hajimehoshi/ebiten/v2"

	"dev-null/internal/engine"
	"dev-null/internal/render"
)

// driftLogThreshold is the |local _gameTime − server _gameTime| above which
// the renderer reports drift via DriftLogger. Below it, snap silently.
const driftLogThreshold = 0.2 // seconds

// LocalRenderer runs game render hooks locally on the client, reading from
// server-provided Game.state (via SetState / MergeStatePatch) instead of
// receiving pre-rendered frames.
//
// State.\_gameTime contract: the server is authoritative; clients extrapolate
// _gameTime from their own wall clock between snapshots. Whenever a snapshot
// brings a fresh _gameTime, the local clock snaps to it. The renderer measures
// the gap between extrapolated and snapped values and reports drift through
// DriftLogger so a UI layer can surface it.
type LocalRenderer struct {
	mu            sync.Mutex
	vm            *goja.Runtime
	loaded        bool
	resolveMeFn   goja.Callable // optional: resolveMe(state, pid) -> me | null
	renderAsciiFn goja.Callable // renderAscii(state, me, cells)
	canvasFn      goja.Callable // renderCanvas(state, me, canvas)

	// Local clock extrapolation. The renderer reads serverGameTime as
	// "what the server said _gameTime was at snapAt", and at render time
	// publishes (serverGameTime + wall-elapsed) onto Game.state._gameTime.
	clockKnown      bool
	serverGameTime  float64
	snapAt          time.Time
	now             func() time.Time           // overridable for tests
	driftLogger     func(driftSec float64)     // called when |drift| > threshold
}

// NewLocalRenderer creates a renderer ready to receive game source and state.
func NewLocalRenderer() *LocalRenderer {
	return &LocalRenderer{
		now: time.Now,
		driftLogger: func(d float64) {
			log.Printf("[clock drift] _gameTime snap moved local clock by %+0.3fs", d)
		},
	}
}

// SetDriftLogger replaces the callback fired on every drift > driftLogThreshold.
// Pass nil to silence drift reports.
func (lr *LocalRenderer) SetDriftLogger(fn func(driftSec float64)) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.driftLogger = fn
}

// LoadGame loads game JS source files into the goja VM.
// Called when the client receives ns;gamesrc OSC sequences.
//
// The client never runs update; it only calls renderCanvas / renderAscii
// with state received via SetState. No ctx is bound — any render-time
// attempt to reach for chat/gameOver/midiNote throws, surfacing impurity
// instead of silently diverging.
func (lr *LocalRenderer) LoadGame(files []GameSrcFile) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	lr.vm = goja.New()
	lr.loaded = false
	lr.renderAsciiFn = nil
	lr.canvasFn = nil
	lr.resolveMeFn = nil

	// Only load-time-safe globals are bound client-side. figlet is pure;
	// include is a no-op because the server already pre-expanded includes
	// into the source file list before broadcasting.
	lr.vm.Set("figlet", func(goja.FunctionCall) goja.Value { return lr.vm.ToValue("") })
	lr.vm.Set("include", func(string) {})

	// Execute all source files in order.
	for _, f := range files {
		if _, err := lr.vm.RunScript(f.Name, f.Content); err != nil {
			log.Printf("local render: error loading %s: %v", f.Name, err)
			return
		}
	}

	gameVal := lr.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		log.Println("local render: no Game object found")
		return
	}
	gameObj := gameVal.ToObject(lr.vm)
	if gameObj == nil {
		return
	}
	if fn, ok := goja.AssertFunction(gameObj.Get("renderAscii")); ok {
		lr.renderAsciiFn = fn
	}
	if fn, ok := goja.AssertFunction(gameObj.Get("renderCanvas")); ok {
		lr.canvasFn = fn
	}
	if fn, ok := goja.AssertFunction(gameObj.Get("resolveMe")); ok {
		lr.resolveMeFn = fn
	}
	lr.loaded = true
}

// SetState updates Game.state in the JS VM from a full JSON baseline,
// replacing any previous state. A baseline is treated as the authoritative
// reset of the local clock — any prior extrapolation is discarded without
// drift logging, since baselines arrive on connect / mode switch / game
// (re)load where comparing to whatever stale clock we held would be noise.
func (lr *LocalRenderer) SetState(jsonData []byte) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if !lr.loaded {
		return
	}

	var state any
	if err := json.Unmarshal(jsonData, &state); err != nil {
		return
	}

	gameVal := lr.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return
	}
	gameObj := gameVal.ToObject(lr.vm)
	if gameObj == nil {
		return
	}
	gameObj.Set("state", lr.vm.ToValue(state))

	// Reset the local clock from whatever the baseline carried.
	if m, ok := state.(map[string]any); ok {
		if gt, ok := numberFromAny(m["_gameTime"]); ok {
			lr.serverGameTime = gt
			lr.snapAt = lr.now()
			lr.clockKnown = true
			return
		}
	}
	// No _gameTime in baseline (game without renderCanvas, or pre-begin
	// snapshot): clear known-flag so the next patch starts fresh.
	lr.clockKnown = false
}

// MergeStatePatch applies a depth-1 JSON merge patch to Game.state. Keys in
// the patch replace the corresponding top-level key; keys whose patch value
// is JSON null are deleted. Keys not present in the patch are left alone.
//
// The patch machinery assumes SetState has been called at least once this
// session to seed Game.state — the server's broadcast path guarantees this.
func (lr *LocalRenderer) MergeStatePatch(jsonData []byte) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if !lr.loaded {
		return
	}

	var patch map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &patch); err != nil {
		return
	}

	gameVal := lr.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return
	}
	gameObj := gameVal.ToObject(lr.vm)
	if gameObj == nil {
		return
	}
	stateVal := gameObj.Get("state")
	if stateVal == nil || goja.IsUndefined(stateVal) || goja.IsNull(stateVal) {
		// No baseline yet — patch without a baseline is ill-defined.
		// Drop silently; the next baseline send will restore full state.
		return
	}
	stateObj := stateVal.ToObject(lr.vm)
	if stateObj == nil {
		return
	}

	for k, raw := range patch {
		if isJSONNull(raw) {
			stateObj.Delete(k)
			if k == "_gameTime" {
				lr.clockKnown = false
			}
			continue
		}
		var val any
		if err := json.Unmarshal(raw, &val); err != nil {
			continue
		}
		stateObj.Set(k, lr.vm.ToValue(val))

		// Snap-and-report on the framework clock. Server suppresses
		// clock-only patches, so when _gameTime arrives it's because some
		// other state also changed — that's our chance to re-anchor and
		// notice if our extrapolation drifted.
		if k == "_gameTime" {
			if gt, ok := numberFromAny(val); ok {
				wallNow := lr.now()
				if lr.clockKnown && lr.driftLogger != nil {
					extrap := lr.serverGameTime + wallNow.Sub(lr.snapAt).Seconds()
					drift := gt - extrap
					if math.Abs(drift) > driftLogThreshold {
						lr.driftLogger(drift)
					}
				}
				lr.serverGameTime = gt
				lr.snapAt = wallNow
				lr.clockKnown = true
			}
		}
	}
}

// injectLocalGameTime overwrites Game.state._gameTime with the locally
// extrapolated value (server-snap + wall-clock elapsed). Called right before
// each render so canvas/ascii hooks see a continuously advancing clock even
// while the server stays silent. No-op if we never received a snapshot. Caller
// holds lr.mu.
func (lr *LocalRenderer) injectLocalGameTime() {
	if !lr.clockKnown {
		return
	}
	state := lr.gameState()
	if goja.IsUndefined(state) || goja.IsNull(state) {
		return
	}
	stateObj := state.ToObject(lr.vm)
	if stateObj == nil {
		return
	}
	t := lr.serverGameTime + lr.now().Sub(lr.snapAt).Seconds()
	stateObj.Set("_gameTime", t)
}

// numberFromAny tolerantly extracts a float from a Go value that might be a
// json.Number, float64, int, or absent. Returns false on missing/wrong type.
func numberFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// isJSONNull checks whether raw decodes to JSON null (whitespace-tolerant).
func isJSONNull(raw json.RawMessage) bool {
	for _, b := range raw {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		return b == 'n' // start of "null"
	}
	return false
}

// IsLoaded returns true if game JS has been loaded and render functions extracted.
func (lr *LocalRenderer) IsLoaded() bool {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return lr.loaded
}

// HasCanvas returns true if the game has a renderCanvas hook.
func (lr *LocalRenderer) HasCanvas() bool {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return lr.canvasFn != nil
}

// RenderCells calls Game.renderAscii() locally and returns the ImageBuffer.
func (lr *LocalRenderer) RenderCells(playerID string, width, height int) *render.ImageBuffer {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if !lr.loaded || lr.renderAsciiFn == nil {
		return nil
	}

	buf := render.NewImageBuffer(width, height)
	jsBuf := newLocalJSBuffer(lr.vm, buf, 0, 0, width, height)

	lr.injectLocalGameTime()
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("local render panic: %v", r)
			}
		}()
		me := lr.resolveMe(playerID)
		if goja.IsUndefined(me) || goja.IsNull(me) {
			return
		}
		jsBuf["ATTR_NONE"] = int(render.AttrNone)
		jsBuf["ATTR_BOLD"] = int(render.AttrBold)
		jsBuf["ATTR_FAINT"] = int(render.AttrFaint)
		jsBuf["ATTR_ITALIC"] = int(render.AttrItalic)
		jsBuf["ATTR_UNDERLINE"] = int(render.AttrUnderline)
		jsBuf["ATTR_REVERSE"] = int(render.AttrReverse)
		jsBuf["log"] = func(msg string) { log.Printf("[cells.log] %s", msg) }
		lr.renderAsciiFn(goja.Undefined(), lr.gameState(), me, lr.vm.ToValue(jsBuf))
	}()

	return buf
}

// renderCanvasRaw calls Game.renderCanvas() locally and returns the raw RGBA image.
func (lr *LocalRenderer) renderCanvasRaw(playerID string, pixelW, pixelH int) *image.RGBA {
	if !lr.loaded || lr.canvasFn == nil {
		return nil
	}

	canvas := engine.NewJSCanvas(pixelW, pixelH, 1.0)
	ctx := canvas.ToJSObject(lr.vm)

	lr.injectLocalGameTime()
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("local renderCanvas panic: %v", r)
			}
		}()
		me := lr.resolveMe(playerID)
		if goja.IsUndefined(me) || goja.IsNull(me) {
			return
		}
		ctx["log"] = func(msg string) { log.Printf("[canvas.log] %s", msg) }
		lr.canvasFn(goja.Undefined(), lr.gameState(), me, lr.vm.ToValue(ctx))
	}()

	return canvas.ToRGBA()
}

// gameState returns the current Game.state as a goja.Value. Caller holds lr.mu.
func (lr *LocalRenderer) gameState() goja.Value {
	gameVal := lr.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return goja.Undefined()
	}
	gameObj := gameVal.ToObject(lr.vm)
	if gameObj == nil {
		return goja.Undefined()
	}
	v := gameObj.Get("state")
	if v == nil {
		return goja.Undefined()
	}
	return v
}

// resolveMe mirrors the server's me-resolution: prefer a game-provided
// resolveMe(state, pid), fall back to state.players[pid]. Returns goja
// undefined when me can't be resolved; callers skip the render call in
// that case so chrome can paint a not-ready splash.
func (lr *LocalRenderer) resolveMe(playerID string) goja.Value {
	state := lr.gameState()
	if lr.resolveMeFn != nil {
		me, err := lr.resolveMeFn(goja.Undefined(), state, lr.vm.ToValue(playerID))
		if err != nil || me == nil || goja.IsNull(me) {
			return goja.Undefined()
		}
		return me
	}
	stateObj := state.ToObject(lr.vm)
	if stateObj == nil {
		return lr.minimalMe(playerID)
	}
	players := stateObj.Get("players")
	if players == nil || goja.IsUndefined(players) || goja.IsNull(players) {
		return lr.minimalMe(playerID)
	}
	playersObj := players.ToObject(lr.vm)
	if playersObj == nil {
		return lr.minimalMe(playerID)
	}
	me := playersObj.Get(playerID)
	if me == nil || goja.IsUndefined(me) || goja.IsNull(me) {
		return lr.minimalMe(playerID)
	}
	return me
}

// minimalMe returns {id: playerID}, matching the server's default.
func (lr *LocalRenderer) minimalMe(playerID string) goja.Value {
	obj := lr.vm.NewObject()
	obj.Set("id", playerID)
	return obj
}

// RenderCanvas calls Game.renderCanvas() locally and returns an Ebitengine image.
func (lr *LocalRenderer) RenderCanvas(playerID string, pixelW, pixelH int) *ebiten.Image {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	img := lr.renderCanvasRaw(playerID, pixelW, pixelH)
	if img == nil {
		return nil
	}
	return ebiten.NewImageFromImage(img)
}

// RenderCanvasImage calls Game.renderCanvas() locally and returns a raw RGBA image
// (for quadrant block conversion in blocks-local mode).
func (lr *LocalRenderer) RenderCanvasImage(playerID string, pixelW, pixelH int) *image.RGBA {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return lr.renderCanvasRaw(playerID, pixelW, pixelH)
}

// newLocalJSBuffer creates a JS-friendly buffer wrapper (same API as server-side).
func newLocalJSBuffer(vm *goja.Runtime, buf *render.ImageBuffer, ox, oy, w, h int) map[string]any {
	return map[string]any{
		"width":  w,
		"height": h,
		"setChar": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			ch := call.Argument(2).String()
			fg := engine.ParseJSColor(call.Argument(3))
			bg := engine.ParseJSColor(call.Argument(4))
			attr := engine.ParseJSAttr(call.Argument(5))
			if len(ch) > 0 {
				buf.SetChar(ox+x, oy+y, []rune(ch)[0], fg, bg, attr)
			}
			return goja.Undefined()
		},
		"writeString": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			text := call.Argument(2).String()
			fg := engine.ParseJSColor(call.Argument(3))
			bg := engine.ParseJSColor(call.Argument(4))
			attr := engine.ParseJSAttr(call.Argument(5))
			buf.WriteString(ox+x, oy+y, text, fg, bg, attr)
			return goja.Undefined()
		},
		"fill": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			fw := int(call.Argument(2).ToInteger())
			fh := int(call.Argument(3).ToInteger())
			ch := call.Argument(4).String()
			fg := engine.ParseJSColor(call.Argument(5))
			bg := engine.ParseJSColor(call.Argument(6))
			attr := engine.ParseJSAttr(call.Argument(7))
			fillCh := ' '
			if len(ch) > 0 {
				fillCh = []rune(ch)[0]
			}
			buf.Fill(ox+x, oy+y, fw, fh, fillCh, fg, bg, attr)
			return goja.Undefined()
		},
		"paintANSI": func(call goja.FunctionCall) goja.Value {
			return goja.Undefined() // not supported in local render
		},
	}
}

