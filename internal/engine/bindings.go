package engine

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

func (r *Runtime) registerGlobals() {
	// Only pure utilities are globals. Everything side-effecty (log, chat,
	// midi*, teams, gameOver, registerCommand, …) is exposed on the ctx object
	// handed to server-only hooks; render JS never sees ctx, so misusing those
	// APIs from render becomes a type error instead of a silent divergence.

	r.vm.Set("figlet", func(text string, font string) string {
		return Figlet(text, font)
	})

	// include("file.js") evaluates another JS file relative to the game's
	// directory. Enables multi-file games stored in games/<name>/ folders.
	included := map[string]bool{}
	r.vm.Set("include", func(name string) {
		if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
			panic(r.vm.NewGoError(fmt.Errorf("include: invalid path %q (no directories or ..)", name)))
		}
		if !strings.HasSuffix(name, ".js") {
			name += ".js"
		}
		absPath := filepath.Join(r.baseDir, name)
		if included[absPath] {
			return
		}
		included[absPath] = true
		src, err := os.ReadFile(absPath)
		if err != nil {
			panic(r.vm.NewGoError(fmt.Errorf("include %q: %w", name, err)))
		}
		r.SourceFiles = append(r.SourceFiles, domain.GameSourceFile{Name: name, Content: string(src)})
		if _, err := r.vm.RunScript(name, string(src)); err != nil {
			panic(r.vm.NewGoError(fmt.Errorf("include %q: %w", name, err)))
		}
	})
}

// newJSImageBuffer creates a JS-friendly wrapper around an ImageBuffer region.
// JS games call buf.setChar(x, y, ch, fg, bg), buf.writeString(x, y, text, fg, bg),
// buf.fill(x, y, w, h, ch, fg, bg) to write directly into the buffer.
func (r *Runtime) newJSImageBuffer(buf *render.ImageBuffer, ox, oy, w, h int) map[string]any {
	return map[string]any{
		"width":  w,
		"height": h,
		"setChar": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			if x < 0 || x >= w || y < 0 || y >= h {
				return goja.Undefined()
			}
			ch := call.Argument(2).String()
			fg := ParseJSColor(call.Argument(3))
			bg := ParseJSColor(call.Argument(4))
			attr := ParseJSAttr(call.Argument(5))
			if len(ch) > 0 {
				r := []rune(ch)[0]
				buf.SetCharInherit(ox+x, oy+y, r, fg, bg, attr)
			}
			return goja.Undefined()
		},
		"writeString": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			if y < 0 || y >= h || x >= w {
				return goja.Undefined()
			}
			if x < 0 {
				x = 0
			}
			text := call.Argument(2).String()
			fg := ParseJSColor(call.Argument(3))
			bg := ParseJSColor(call.Argument(4))
			attr := ParseJSAttr(call.Argument(5))
			// Clip to viewport width; WriteString uses buf.Width which can
			// spill one cell into the right border.
			buf.PaintANSILine(ox+x, oy+y, w-x, text, fg, bg, attr)
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
			// Clip to viewport bounds so JS cannot overwrite the border.
			if x < 0 {
				fw += x
				x = 0
			}
			if y < 0 {
				fh += y
				y = 0
			}
			if x+fw > w {
				fw = w - x
			}
			if y+fh > h {
				fh = h - y
			}
			if fw <= 0 || fh <= 0 {
				return goja.Undefined()
			}
			buf.FillInherit(ox+x, oy+y, fw, fh, fillCh, fg, bg, attr)
			return goja.Undefined()
		},
	}
}

// ParseJSColor converts a JS value to a color.Color.
// Accepts: null/undefined → nil, "#RRGGBB" hex string → color.RGBA.
func ParseJSColor(v goja.Value) color.Color {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	s := v.String()
	if len(s) == 7 && s[0] == '#' {
		r := hexByte(s[1], s[2])
		g := hexByte(s[3], s[4])
		b := hexByte(s[5], s[6])
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return nil
}

func hexByte(hi, lo byte) uint8 {
	return hexNibble(hi)<<4 | hexNibble(lo)
}

func hexNibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// ParseJSAttr converts a JS value to a PixelAttr.
// Accepts: null/undefined → AttrNone, number → PixelAttr bitmask.
func ParseJSAttr(v goja.Value) render.PixelAttr {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return render.AttrNone
	}
	return render.PixelAttr(v.ToInteger())
}

// gojaToWidgetNode recursively converts a goja JS object into a WidgetNode tree.
func gojaToWidgetNode(vm *goja.Runtime, val goja.Value) *domain.WidgetNode {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	obj := val.ToObject(vm)
	if obj == nil {
		return nil
	}

	node := &domain.WidgetNode{}

	if v := obj.Get("type"); v != nil && !goja.IsUndefined(v) {
		node.Type = v.String()
	}
	if v := obj.Get("title"); v != nil && !goja.IsUndefined(v) {
		node.Title = v.String()
	}
	if v := obj.Get("text"); v != nil && !goja.IsUndefined(v) {
		node.Text = v.String()
	}
	if v := obj.Get("align"); v != nil && !goja.IsUndefined(v) {
		node.Align = v.String()
	}
	if v := obj.Get("weight"); v != nil && !goja.IsUndefined(v) {
		node.Weight = v.ToFloat()
	}
	if v := obj.Get("width"); v != nil && !goja.IsUndefined(v) {
		node.Width = int(v.ToInteger())
	}
	if v := obj.Get("height"); v != nil && !goja.IsUndefined(v) {
		node.Height = int(v.ToInteger())
	}

	// Interactive control fields
	if v := obj.Get("action"); v != nil && !goja.IsUndefined(v) {
		node.Action = v.String()
	}
	if v := obj.Get("focusable"); v != nil && !goja.IsUndefined(v) {
		node.IsFocusable = v.ToBoolean()
	}
	if v := obj.Get("tabIndex"); v != nil && !goja.IsUndefined(v) {
		node.TabIndex = int(v.ToInteger())
	}
	if v := obj.Get("checked"); v != nil && !goja.IsUndefined(v) {
		node.Checked = v.ToBoolean()
	}
	if v := obj.Get("value"); v != nil && !goja.IsUndefined(v) {
		node.Value = v.String()
	}
	if v := obj.Get("lines"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		arr := v.ToObject(vm)
		for _, key := range arr.Keys() {
			line := arr.Get(key)
			if line != nil && !goja.IsUndefined(line) {
				node.Lines = append(node.Lines, line.String())
			}
		}
	}

	// Children array
	if v := obj.Get("children"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		arr := v.ToObject(vm)
		for _, key := range arr.Keys() {
			child := gojaToWidgetNode(vm, arr.Get(key))
			if child != nil {
				node.Children = append(node.Children, child)
			}
		}
	}

	// Rows for table (array of arrays of strings)
	if v := obj.Get("rows"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		rowsArr := v.ToObject(vm)
		for _, rk := range rowsArr.Keys() {
			rowVal := rowsArr.Get(rk)
			if rowVal == nil || goja.IsUndefined(rowVal) {
				continue
			}
			rowObj := rowVal.ToObject(vm)
			var cells []string
			for _, ck := range rowObj.Keys() {
				cell := rowObj.Get(ck)
				if cell != nil && !goja.IsUndefined(cell) {
					cells = append(cells, cell.String())
				}
			}
			node.Rows = append(node.Rows, cells)
		}
	}

	return node
}
