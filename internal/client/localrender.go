package client

import (
	"encoding/json"
	"image"
	"image/color"
	"log"
	"sync"

	"github.com/dop251/goja"
	"github.com/hajimehoshi/ebiten/v2"

	"null-space/internal/engine"
	"null-space/internal/render"
)

// LocalRenderer runs game JS locally on the client, rendering from
// server-provided Game.state instead of receiving pre-rendered frames.
type LocalRenderer struct {
	mu       sync.Mutex
	vm       *goja.Runtime
	loaded   bool
	renderFn goja.Callable // Game.render(buf, playerID, x, y, w, h)
	canvasFn goja.Callable // Game.renderCanvas(ctx, playerID, w, h)

	// Canvas scale — controlled locally by the client.
	CanvasScale int
}

// NewLocalRenderer creates a renderer ready to receive game source and state.
func NewLocalRenderer() *LocalRenderer {
	return &LocalRenderer{
		CanvasScale: 8, // default
	}
}

// LoadGame loads game JS source files into the goja VM.
// Called when the client receives ns;gamesrc OSC sequences.
func (lr *LocalRenderer) LoadGame(files []GameSrcFile) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	lr.vm = goja.New()
	lr.loaded = false
	lr.renderFn = nil
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

	// Pixel attribute constants.
	lr.vm.Set("ATTR_NONE", int(render.AttrNone))
	lr.vm.Set("ATTR_BOLD", int(render.AttrBold))
	lr.vm.Set("ATTR_FAINT", int(render.AttrFaint))
	lr.vm.Set("ATTR_ITALIC", int(render.AttrItalic))
	lr.vm.Set("ATTR_UNDERLINE", int(render.AttrUnderline))
	lr.vm.Set("ATTR_REVERSE", int(render.AttrReverse))
	lr.vm.Set("PUA_START", int(render.PUAStart))
	lr.vm.Set("PUA_END", int(render.PUAEnd))

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

	if fn, ok := goja.AssertFunction(gameObj.Get("render")); ok {
		lr.renderFn = fn
	}
	if fn, ok := goja.AssertFunction(gameObj.Get("renderCanvas")); ok {
		lr.canvasFn = fn
	}

	// Call init with nil (we'll receive state via SetState).
	if initFn, ok := goja.AssertFunction(gameObj.Get("init")); ok {
		initFn(goja.Undefined(), goja.Null())
	}

	lr.loaded = true
}

// SetState updates Game.state in the JS VM from JSON.
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

// RenderCells calls Game.render() locally and returns the ImageBuffer.
func (lr *LocalRenderer) RenderCells(playerID string, width, height int) *render.ImageBuffer {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if !lr.loaded || lr.renderFn == nil {
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
		lr.renderFn(goja.Undefined(),
			lr.vm.ToValue(jsBuf),
			lr.vm.ToValue(playerID),
			lr.vm.ToValue(0), lr.vm.ToValue(0),
			lr.vm.ToValue(width), lr.vm.ToValue(height))
	}()

	return buf
}

// RenderCanvas calls Game.renderCanvas() locally and returns an Ebitengine image.
func (lr *LocalRenderer) RenderCanvas(playerID string, pixelW, pixelH int) *ebiten.Image {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if !lr.loaded || lr.canvasFn == nil {
		return nil
	}

	canvas := engine.NewJSCanvas(pixelW, pixelH)
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

	return ebiten.NewImageFromImage(canvas.ToRGBA())
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

// bufferToImage converts an ImageBuffer to an Ebitengine image by drawing
// each cell as a colored rectangle (text rendering is done separately).
func bufferToImage(buf *render.ImageBuffer) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, buf.Width*cellW, buf.Height*cellH))
	for y := 0; y < buf.Height; y++ {
		for x := 0; x < buf.Width; x++ {
			p := &buf.Pixels[y*buf.Width+x]
			bg := color.RGBA{A: 255}
			if p.Bg != nil {
				r, g, b, _ := p.Bg.RGBA()
				bg = color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
			}
			// Fill the cell rectangle with bg color.
			px := x * cellW
			py := y * cellH
			for dy := 0; dy < cellH; dy++ {
				for dx := 0; dx < cellW; dx++ {
					img.SetRGBA(px+dx, py+dy, bg)
				}
			}
		}
	}
	return img
}
