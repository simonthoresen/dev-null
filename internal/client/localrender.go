package client

import (
	"encoding/json"
	"image"
	"log"
	"sync"

	"github.com/dop251/goja"
	"github.com/hajimehoshi/ebiten/v2"

	"dev-null/internal/engine"
	"dev-null/internal/render"
)

// LocalRenderer runs game JS locally on the client, rendering from
// server-provided Game.state instead of receiving pre-rendered frames.
type LocalRenderer struct {
	mu       sync.Mutex
	vm       *goja.Runtime
	loaded   bool
	// contractVersion is 1 for v1 games (legacy render signatures) and 2 for
	// v2 games. v2 uses renderCanvas(state, me, canvas) / renderAscii(state,
	// me, cells) and does not bind ctx-style globals on the client VM.
	contractVersion int
	resolveMeFn     goja.Callable // optional game-provided resolveMe(state, pid)
	renderAsciiFn   goja.Callable // v1: (buf, pid, x, y, w, h); v2: (state, me, cells)
	canvasFn        goja.Callable // v1: (ctx, pid, w, h);       v2: (state, me, canvas)

}

// NewLocalRenderer creates a renderer ready to receive game source and state.
func NewLocalRenderer() *LocalRenderer {
	return &LocalRenderer{}
}

// LoadGame loads game JS source files into the goja VM.
// Called when the client receives ns;gamesrc OSC sequences.
func (lr *LocalRenderer) LoadGame(files []GameSrcFile) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	lr.vm = goja.New()
	lr.loaded = false
	lr.renderAsciiFn = nil
	lr.canvasFn = nil
	lr.resolveMeFn = nil
	lr.contractVersion = 1

	// Always-safe globals that games may rely on at module-load time,
	// regardless of contract version. These are pure computations that don't
	// reach into framework state, so exposing them on the client doesn't
	// compromise the "no impure calls from render" property.
	lr.vm.Set("figlet", func(goja.FunctionCall) goja.Value { return lr.vm.ToValue("") })
	lr.vm.Set("include", func(string) {}) // includes are pre-expanded by the server

	// Peek at the source to detect the contract version before executing,
	// so we only stub v1 globals when they're actually needed. v2 games
	// declare `contract: 2` on their Game object; we look for that literal.
	// Ugly but effective — the alternative is executing with every global
	// bound and then pruning, which defeats the "v2 render can't call ctx"
	// guarantee.
	isV2 := sourceDeclaresV2(files)
	lr.contractVersion = 1
	if isV2 {
		lr.contractVersion = 2
	}

	if !isV2 {
		// v1 legacy globals — stubbed as no-ops on the client, since all
		// side-effecty globals belong to server-side execution anyway.
		lr.vm.Set("log", func(msg string) { log.Println("[game]", msg) })
		lr.vm.Set("chat", func(string) {})
		lr.vm.Set("chatPlayer", func(string, string) {})
		lr.vm.Set("teams", func() []any { return nil })
		lr.vm.Set("now", func() int64 { return 0 })
		lr.vm.Set("gameOver", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("registerCommand", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("midiNote", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("midiProgram", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("midiCC", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("midiPitch", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		lr.vm.Set("midiSilence", func(goja.FunctionCall) goja.Value { return goja.Undefined() })

		// v1-only global pixel attribute constants. v2 games read these from
		// the cells object (cells.ATTR_BOLD, etc.) which the server's render
		// dispatch installs.
		lr.vm.Set("ATTR_NONE", int(render.AttrNone))
		lr.vm.Set("ATTR_BOLD", int(render.AttrBold))
		lr.vm.Set("ATTR_FAINT", int(render.AttrFaint))
		lr.vm.Set("ATTR_ITALIC", int(render.AttrItalic))
		lr.vm.Set("ATTR_UNDERLINE", int(render.AttrUnderline))
		lr.vm.Set("ATTR_REVERSE", int(render.AttrReverse))
	}

	// Execute all source files in order.
	for _, f := range files {
		if _, err := lr.vm.RunScript(f.Name, f.Content); err != nil {
			log.Printf("local render: error loading %s: %v", f.Name, err)
			return
		}
	}

	// Extract render functions from the Game object.
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

	// v1: call load(null) so module-level wiring runs. v2: the framework
	// calls init(ctx) on the server; the client's state arrives fully
	// formed via SetState and no init is needed here.
	if !isV2 {
		if loadFn, ok := goja.AssertFunction(gameObj.Get("load")); ok {
			loadFn(goja.Undefined(), goja.Null())
		}
	}

	lr.loaded = true
}

// sourceDeclaresV2 returns true if any of the loaded source files declares
// a v2 contract via `contract: 2` on the Game object. Matches the literal
// pattern the design doc specifies; a false positive would at worst cause
// the client to skip the legacy stubs for a v1 game, which we'd catch
// immediately when the game calls an undefined global.
func sourceDeclaresV2(files []GameSrcFile) bool {
	for _, f := range files {
		if containsContractV2(f.Content) {
			return true
		}
	}
	return false
}

func containsContractV2(src string) bool {
	// Match "contract: 2" or "contract:2" allowing minor whitespace variance.
	for i := 0; i+10 < len(src); i++ {
		if src[i] == 'c' && src[i:i+8] == "contract" {
			// Scan past "contract", optional whitespace, ':', optional whitespace, '2'.
			j := i + 8
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == ':' {
				j++
				for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
					j++
				}
				if j < len(src) && src[j] == '2' {
					return true
				}
			}
		}
	}
	return false
}

// SetState updates Game.state in the JS VM from a full JSON baseline,
// replacing any previous state.
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
	if gameObj != nil {
		gameObj.Set("state", lr.vm.ToValue(state))
	}
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
			continue
		}
		var val any
		if err := json.Unmarshal(raw, &val); err != nil {
			continue
		}
		stateObj.Set(k, lr.vm.ToValue(val))
	}
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

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("local render panic: %v", r)
			}
		}()
		if lr.contractVersion >= 2 {
			me := lr.resolveMe(playerID)
			if goja.IsUndefined(me) || goja.IsNull(me) {
				return
			}
			// Install ATTR_* and log on the cells object for v2 authors.
			jsBuf["ATTR_NONE"] = int(render.AttrNone)
			jsBuf["ATTR_BOLD"] = int(render.AttrBold)
			jsBuf["ATTR_FAINT"] = int(render.AttrFaint)
			jsBuf["ATTR_ITALIC"] = int(render.AttrItalic)
			jsBuf["ATTR_UNDERLINE"] = int(render.AttrUnderline)
			jsBuf["ATTR_REVERSE"] = int(render.AttrReverse)
			jsBuf["log"] = func(msg string) { log.Printf("[cells.log] %s", msg) }
			lr.renderAsciiFn(goja.Undefined(), lr.gameState(), me, lr.vm.ToValue(jsBuf))
			return
		}
		lr.renderAsciiFn(goja.Undefined(),
			lr.vm.ToValue(jsBuf),
			lr.vm.ToValue(playerID),
			lr.vm.ToValue(0), lr.vm.ToValue(0),
			lr.vm.ToValue(width), lr.vm.ToValue(height))
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

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("local renderCanvas panic: %v", r)
			}
		}()
		if lr.contractVersion >= 2 {
			me := lr.resolveMe(playerID)
			if goja.IsUndefined(me) || goja.IsNull(me) {
				return
			}
			ctx["log"] = func(msg string) { log.Printf("[canvas.log] %s", msg) }
			lr.canvasFn(goja.Undefined(), lr.gameState(), me, lr.vm.ToValue(ctx))
			return
		}
		lr.canvasFn(goja.Undefined(),
			lr.vm.ToValue(ctx),
			lr.vm.ToValue(playerID),
			lr.vm.ToValue(pixelW),
			lr.vm.ToValue(pixelH))
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
		return goja.Undefined()
	}
	players := stateObj.Get("players")
	if players == nil || goja.IsUndefined(players) || goja.IsNull(players) {
		return goja.Undefined()
	}
	playersObj := players.ToObject(lr.vm)
	if playersObj == nil {
		return goja.Undefined()
	}
	me := playersObj.Get(playerID)
	if me == nil || goja.IsUndefined(me) || goja.IsNull(me) {
		return goja.Undefined()
	}
	return me
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

