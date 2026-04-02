package engine

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dop251/goja"

	"null-space/common"
	"null-space/internal/network"
)

// JSShader wraps a goja JS runtime for a per-player post-processing shader.
// The shader exports a Shader object with a process(buf) method that receives
// the full rendered ImageBuffer and may read/write any pixel.
type JSShader struct {
	mu        sync.Mutex
	vm        *goja.Runtime
	name      string
	processFn goja.Callable // Shader.process(buf)
	unloadFn  goja.Callable // Shader.unload() — optional
}

// LoadShader reads and executes a JS shader file, extracting the Shader.process hook.
func LoadShader(path string, clock common.Clock) (*JSShader, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read shader file: %w", err)
	}

	s := &JSShader{
		vm:   goja.New(),
		name: strings.TrimSuffix(filepath.Base(path), ".js"),
	}

	// Minimal globals.
	s.vm.Set("log", func(msg string) {
		slog.Info("shader log", "shader", s.name, "msg", msg)
	})
	s.vm.Set("now", func() int64 {
		return clock.Now().UnixMilli()
	})

	// Pixel attribute constants.
	s.vm.Set("ATTR_NONE", int(common.AttrNone))
	s.vm.Set("ATTR_BOLD", int(common.AttrBold))
	s.vm.Set("ATTR_FAINT", int(common.AttrFaint))
	s.vm.Set("ATTR_ITALIC", int(common.AttrItalic))
	s.vm.Set("ATTR_UNDERLINE", int(common.AttrUnderline))
	s.vm.Set("ATTR_REVERSE", int(common.AttrReverse))

	if _, err := s.vm.RunScript(path, string(src)); err != nil {
		return nil, fmt.Errorf("execute shader script: %w", err)
	}

	shaderVal := s.vm.Get("Shader")
	if shaderVal == nil || goja.IsUndefined(shaderVal) || goja.IsNull(shaderVal) {
		return nil, fmt.Errorf("shader script must export a global Shader object")
	}
	obj := shaderVal.ToObject(s.vm)
	if obj == nil {
		return nil, fmt.Errorf("Shader is not an object")
	}

	if fn, ok := goja.AssertFunction(obj.Get("process")); ok {
		s.processFn = fn
	} else {
		return nil, fmt.Errorf("Shader.process is required and must be a function")
	}

	if fn, ok := goja.AssertFunction(obj.Get("unload")); ok {
		s.unloadFn = fn
	}

	// Call init() if present.
	if fn, ok := goja.AssertFunction(obj.Get("init")); ok {
		cancel := WatchdogJS(s.vm, "Shader.init")
		defer cancel()
		if _, err := fn(goja.Undefined()); err != nil {
			slog.Warn("shader init error", "shader", s.name, "error", err)
		}
	}

	return s, nil
}

// Name returns the shader's display name (filename stem).
func (s *JSShader) Name() string { return s.name }

// Process calls the JS process(buf) hook with a buffer wrapper.
func (s *JSShader) Process(buf *common.ImageBuffer) {
	if s.processFn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("shader panic", "shader", s.name, "panic", r)
		}
	}()

	cancel := WatchdogJS(s.vm, "Shader.process")
	defer cancel()

	jsBuf := newJSShaderBuffer(s.vm, buf)
	_, err := s.processFn(goja.Undefined(), s.vm.ToValue(jsBuf))
	if err != nil {
		slog.Error("shader process error", "shader", s.name, "error", err)
	}
}

// Unload interrupts the JS runtime, calling Shader.unload() first if defined.
func (s *JSShader) Unload() {
	if s.unloadFn != nil {
		s.mu.Lock()
		cancel := WatchdogJS(s.vm, "Shader.unload")
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("shader unload panic", "shader", s.name, "panic", r)
				}
			}()
			s.unloadFn(goja.Undefined())
		}()
		cancel()
		s.mu.Unlock()
	}
	s.vm.Interrupt("unloaded")
}

// newJSShaderBuffer creates a JS-friendly wrapper for the full ImageBuffer.
// Unlike the game render buffer, shaders see the entire screen (no offset clipping).
// Includes getPixel() for reading, plus all the standard write methods.
func newJSShaderBuffer(vm *goja.Runtime, buf *common.ImageBuffer) map[string]any {
	return map[string]any{
		"width":  buf.Width,
		"height": buf.Height,

		// getPixel(x, y) → {char, fg, bg, attr} or null if out of bounds.
		"getPixel": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			if x < 0 || x >= buf.Width || y < 0 || y >= buf.Height {
				return goja.Null()
			}
			p := &buf.Pixels[y*buf.Width+x]
			obj := vm.NewObject()
			obj.Set("char", string(p.Char))
			if p.Fg != nil {
				r, g, b, _ := p.Fg.RGBA()
				obj.Set("fg", fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
			} else {
				obj.Set("fg", goja.Null())
			}
			if p.Bg != nil {
				r, g, b, _ := p.Bg.RGBA()
				obj.Set("bg", fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
			} else {
				obj.Set("bg", goja.Null())
			}
			obj.Set("attr", int(p.Attr))
			return obj
		},

		"setChar": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			ch := call.Argument(2).String()
			fg := ParseJSColor(call.Argument(3))
			bg := ParseJSColor(call.Argument(4))
			attr := ParseJSAttr(call.Argument(5))
			if len(ch) > 0 {
				buf.SetChar(x, y, []rune(ch)[0], fg, bg, attr)
			}
			return goja.Undefined()
		},

		"writeString": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			text := call.Argument(2).String()
			fg := ParseJSColor(call.Argument(3))
			bg := ParseJSColor(call.Argument(4))
			attr := ParseJSAttr(call.Argument(5))
			buf.WriteString(x, y, text, fg, bg, attr)
			return goja.Undefined()
		},

		"fill": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			fw := int(call.Argument(2).ToInteger())
			fh := int(call.Argument(3).ToInteger())
			ch := call.Argument(4).String()
			fg := ParseJSColor(call.Argument(5))
			bg := ParseJSColor(call.Argument(6))
			attr := ParseJSAttr(call.Argument(7))
			fillCh := ' '
			if len(ch) > 0 {
				fillCh = []rune(ch)[0]
			}
			buf.Fill(x, y, fw, fh, fillCh, fg, bg, attr)
			return goja.Undefined()
		},

		"recolor": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			w := int(call.Argument(2).ToInteger())
			h := int(call.Argument(3).ToInteger())
			fg := ParseJSColor(call.Argument(4))
			bg := ParseJSColor(call.Argument(5))
			attr := ParseJSAttr(call.Argument(6))
			buf.RecolorRect(x, y, w, h, fg, bg, attr)
			return goja.Undefined()
		},
	}
}

// ResolveShaderPath resolves a shader name or URL to a local file path,
// downloading and caching if it's a URL.
func ResolveShaderPath(nameOrURL, dataDir string) (name, path string, err error) {
	if network.IsURL(nameOrURL) {
		cacheDir := filepath.Join(dataDir, "shaders", ".cache")
		local, err := network.DownloadToCache(nameOrURL, cacheDir)
		if err != nil {
			return "", "", fmt.Errorf("download shader: %w", err)
		}
		return strings.TrimSuffix(filepath.Base(local), ".js"), local, nil
	}
	return nameOrURL, filepath.Join(dataDir, "shaders", nameOrURL+".js"), nil
}

// ApplyShaders runs all shaders in sequence on the given buffer.
func ApplyShaders(shaders []common.Shader, buf *common.ImageBuffer) {
	for _, s := range shaders {
		s.Process(buf)
	}
}

// ColorToHex converts a color.Color to a "#rrggbb" string, or "" if nil.
func ColorToHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
