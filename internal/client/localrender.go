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
	renderAsciiFn goja.Callable // Game.renderAscii(buf, playerID, x, y, w, h)
	canvasFn goja.Callable // Game.renderCanvas(ctx, playerID, w, h)

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

	// Register stub globals — client doesn't need game logic, just rendering.
	lr.vm.Set("log", func(msg string) {
		log.Println("[game]", msg)
	})
	lr.vm.Set("chat", func(string) {})           // no-op on client
	lr.vm.Set("chatPlayer", func(string, string) {}) // no-op on client
	lr.vm.Set("teams", func() []any { return nil })  // no teams on client render
	lr.vm.Set("now", func() int64 { return 0 })      // games should use state for time
	lr.vm.Set("gameOver", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("registerCommand", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("figlet", func(goja.FunctionCall) goja.Value { return lr.vm.ToValue("") })
	lr.vm.Set("include", func(string) {}) // includes are pre-expanded by the server

	// MIDI stubs — client doesn't play audio, but games may call these at init time.
	lr.vm.Set("midiNote", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("midiProgram", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("midiCC", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("midiPitch", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	lr.vm.Set("midiSilence", func(goja.FunctionCall) goja.Value { return goja.Undefined() })

	// Pixel attribute constants.
	lr.vm.Set("ATTR_NONE", int(render.AttrNone))
	lr.vm.Set("ATTR_BOLD", int(render.AttrBold))
	lr.vm.Set("ATTR_FAINT", int(render.AttrFaint))
	lr.vm.Set("ATTR_ITALIC", int(render.AttrItalic))
	lr.vm.Set("ATTR_UNDERLINE", int(render.AttrUnderline))
	lr.vm.Set("ATTR_REVERSE", int(render.AttrReverse))

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

	// Call load with nil (we'll receive state via SetState).
	if loadFn, ok := goja.AssertFunction(gameObj.Get("load")); ok {
		loadFn(goja.Undefined(), goja.Null())
	}

	lr.loaded = true
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
		lr.canvasFn(goja.Undefined(),
			lr.vm.ToValue(ctx),
			lr.vm.ToValue(playerID),
			lr.vm.ToValue(pixelW),
			lr.vm.ToValue(pixelH))
	}()

	return canvas.ToRGBA()
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

