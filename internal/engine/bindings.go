package engine

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

func (r *Runtime) registerGlobals() {
	// Pixel attribute constants for buf.setChar/writeString/fill.
	r.vm.Set("ATTR_NONE", int(render.AttrNone))
	r.vm.Set("ATTR_BOLD", int(render.AttrBold))
	r.vm.Set("ATTR_FAINT", int(render.AttrFaint))
	r.vm.Set("ATTR_ITALIC", int(render.AttrItalic))
	r.vm.Set("ATTR_UNDERLINE", int(render.AttrUnderline))
	r.vm.Set("ATTR_REVERSE", int(render.AttrReverse))

	// PUA codepoint range constants for charmap-based sprite rendering.
	r.vm.Set("PUA_START", int(render.PUAStart))
	r.vm.Set("PUA_END", int(render.PUAEnd))

	r.vm.Set("log", func(msg string) {
		if r.logFn != nil {
			r.logFn(msg)
		}
	})

	r.vm.Set("chat", func(msg string) {
		if r.chatCh != nil {
			select {
			case r.chatCh <- domain.Message{Text: msg}:
			default:
				slog.Warn("JS chat channel full, dropping message", "text", msg)
			}
		}
	})

	r.vm.Set("chatPlayer", func(playerID, msg string) {
		if r.chatCh != nil {
			select {
			case r.chatCh <- domain.Message{Text: msg, IsPrivate: true, ToID: playerID}:
			default:
				slog.Warn("JS chatPlayer channel full, dropping message")
			}
		}
	})

	r.vm.Set("playSound", func(call goja.FunctionCall) goja.Value {
		if r.chatCh == nil {
			return goja.Undefined()
		}
		filename := ""
		if v := call.Argument(0); !goja.IsUndefined(v) && !goja.IsNull(v) {
			filename = v.String()
		}
		msg := domain.Message{SoundFile: filename}
		if optsVal := call.Argument(1); !goja.IsUndefined(optsVal) && !goja.IsNull(optsVal) {
			if opts := optsVal.ToObject(r.vm); opts != nil {
				if v := opts.Get("loop"); v != nil && !goja.IsUndefined(v) {
					msg.SoundLoop = v.ToBoolean()
				}
				if v := opts.Get("alt"); v != nil && !goja.IsUndefined(v) {
					msg.Text = v.String()
				}
			}
		}
		select {
		case r.chatCh <- msg:
		default:
			slog.Warn("JS playSound channel full, dropping", "file", filename)
		}
		return goja.Undefined()
	})

	r.vm.Set("stopSound", func(call goja.FunctionCall) goja.Value {
		if r.chatCh == nil {
			return goja.Undefined()
		}
		filename := ""
		if v := call.Argument(0); !goja.IsUndefined(v) && !goja.IsNull(v) {
			filename = v.String()
		}
		select {
		case r.chatCh <- domain.Message{SoundStop: true, SoundFile: filename}:
		default:
			slog.Warn("JS stopSound channel full, dropping", "file", filename)
		}
		return goja.Undefined()
	})

	r.vm.Set("teams", func() []map[string]any {
		// Return a deep copy of the cached snapshot to prevent JS mutation.
		result := make([]map[string]any, len(r.cachedTeams))
		for i, t := range r.cachedTeams {
			cp := make(map[string]any, len(t))
			for k, v := range t {
				if k == "players" {
					if players, ok := v.([]any); ok {
						pCopy := make([]any, len(players))
						for j, p := range players {
							if pm, ok := p.(map[string]any); ok {
								entry := make(map[string]any, len(pm))
								for pk, pv := range pm {
									entry[pk] = pv
								}
								pCopy[j] = entry
							} else {
								pCopy[j] = p
							}
						}
						cp[k] = pCopy
					}
				} else {
					cp[k] = v
				}
			}
			result[i] = cp
		}
		return result
	})

	r.vm.Set("gameOver", func(call goja.FunctionCall) goja.Value {
		r.gameOverPending = true
		r.gameOverResults = nil

		// First arg: results array [{name, result}, ...]
		if len(call.Arguments) > 0 {
			arg := call.Argument(0)
			if arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
				obj := arg.ToObject(r.vm)
				if obj != nil {
					for _, key := range obj.Keys() {
						item := obj.Get(key)
						if item == nil || goja.IsUndefined(item) {
							continue
						}
						itemObj := item.ToObject(r.vm)
						if itemObj == nil {
							continue
						}
						entry := domain.GameResult{}
						if v := itemObj.Get("name"); v != nil && !goja.IsUndefined(v) {
							entry.Name = v.String()
						}
						if v := itemObj.Get("result"); v != nil && !goja.IsUndefined(v) {
							entry.Result = v.String()
						}
						r.gameOverResults = append(r.gameOverResults, entry)
					}
				}
			}
		}

		return goja.Undefined()
	})

	r.vm.Set("figlet", func(text string, font string) string {
		return Figlet(text, font)
	})

	r.vm.Set("addMenu", func(call goja.FunctionCall) goja.Value {
		label := ""
		if v := call.Argument(0); !goja.IsUndefined(v) {
			label = v.String()
		}
		if label == "" {
			return goja.Undefined()
		}
		itemsVal := call.Argument(1)
		var items []domain.MenuItemDef
		if !goja.IsUndefined(itemsVal) && !goja.IsNull(itemsVal) {
			arr := itemsVal.ToObject(r.vm)
			for _, k := range arr.Keys() {
				el := arr.Get(k)
				if el == nil || goja.IsUndefined(el) || goja.IsNull(el) {
					continue
				}
				obj := el.ToObject(r.vm)
				itemLabel := ""
				if v := obj.Get("label"); v != nil && !goja.IsUndefined(v) {
					itemLabel = v.String()
				}
				disabled := false
				if v := obj.Get("disabled"); v != nil && !goja.IsUndefined(v) {
					disabled = v.ToBoolean()
				}
				var handler goja.Callable
				if v := obj.Get("onClick"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					handler, _ = goja.AssertFunction(v)
				}
				var goHandler func(string)
				if handler != nil {
					capturedHandler := handler
					goHandler = func(playerID string) {
						r.mu.Lock()
						defer r.mu.Unlock()
						_, _ = capturedHandler(goja.Undefined(), r.vm.ToValue(playerID))
					}
				}
				items = append(items, domain.MenuItemDef{
					Label:    itemLabel,
					Disabled: disabled,
					Handler:  goHandler,
				})
			}
		}
		r.menus = append(r.menus, domain.MenuDef{Label: label, Items: items})
		return goja.Undefined()
	})

	r.vm.Set("messageBox", func(call goja.FunctionCall) goja.Value {
		playerID := ""
		if v := call.Argument(0); !goja.IsUndefined(v) {
			playerID = v.String()
		}
		optsVal := call.Argument(1)
		if goja.IsUndefined(optsVal) || goja.IsNull(optsVal) {
			return goja.Undefined()
		}
		opts := optsVal.ToObject(r.vm)

		title := ""
		if v := opts.Get("title"); v != nil && !goja.IsUndefined(v) {
			title = v.String()
		}
		message := ""
		if v := opts.Get("message"); v != nil && !goja.IsUndefined(v) {
			message = v.String()
		}
		var buttons []string
		if v := opts.Get("buttons"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			arr := v.ToObject(r.vm)
			for _, k := range arr.Keys() {
				if el := arr.Get(k); el != nil && !goja.IsUndefined(el) {
					buttons = append(buttons, el.String())
				}
			}
		}
		var onClose func(string)
		if v := opts.Get("onClose"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			if cb, ok := goja.AssertFunction(v); ok {
				onClose = func(button string) {
					r.mu.Lock()
					defer r.mu.Unlock()
					_, _ = cb(goja.Undefined(), r.vm.ToValue(button))
				}
			}
		}
		d := domain.DialogRequest{
			Title:   title,
			Body:    message,
			Buttons: buttons,
			OnClose: onClose,
		}
		if r.showDialogFn != nil {
			go r.showDialogFn(playerID, d)
		}
		return goja.Undefined()
	})

	r.vm.Set("registerCommand", func(call goja.FunctionCall) goja.Value {
		specVal := call.Argument(0)
		specObj := specVal.ToObject(r.vm)

		name := ""
		if v := specObj.Get("name"); v != nil {
			name = v.String()
		}
		desc := ""
		if v := specObj.Get("description"); v != nil {
			desc = v.String()
		}
		adminOnly := false
		if v := specObj.Get("adminOnly"); v != nil && !goja.IsUndefined(v) {
			adminOnly = v.ToBoolean()
		}
		firstArgIsPlayer := false
		if v := specObj.Get("firstArgIsPlayer"); v != nil && !goja.IsUndefined(v) {
			firstArgIsPlayer = v.ToBoolean()
		}
		handler, _ := goja.AssertFunction(specObj.Get("handler"))

		if name == "" || handler == nil {
			slog.Warn("JS registerCommand: name and handler are required")
			return goja.Undefined()
		}

		cmd := domain.Command{
			Name:             name,
			Description:      desc,
			AdminOnly:        adminOnly,
			FirstArgIsPlayer: firstArgIsPlayer,
			Handler: func(ctx domain.CommandContext, args []string) {
				r.mu.Lock()
				defer r.mu.Unlock()

				jsArgs := make([]interface{}, len(args))
				for i, a := range args {
					jsArgs[i] = a
				}
				argsVal := r.vm.ToValue(jsArgs)

				_, err := handler(goja.Undefined(),
					r.vm.ToValue(ctx.PlayerID),
					r.vm.ToValue(ctx.IsAdmin),
					argsVal,
				)
				if err != nil {
					slog.Error("JS command handler error", "name", name, "error", err)
					ctx.Reply(fmt.Sprintf("Command error: %v", err))
				}
			},
		}
		r.commands = append(r.commands, cmd)
		return goja.Undefined()
	})

	// now() — returns server time in epoch milliseconds (mockable via Clock).
	r.vm.Set("now", func() int64 {
		return r.clock.Now().UnixMilli()
	})

	// include("file.js") — evaluate another JS file relative to the game's directory.
	// This enables multi-file games stored in games/<name>/ folders.
	included := map[string]bool{} // track already-included files to prevent cycles
	r.vm.Set("include", func(name string) {
		// Sanitize: no path traversal, must end in .js
		if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
			panic(r.vm.NewGoError(fmt.Errorf("include: invalid path %q (no directories or ..)", name)))
		}
		if !strings.HasSuffix(name, ".js") {
			name += ".js"
		}
		absPath := filepath.Join(r.baseDir, name)
		if included[absPath] {
			return // already included
		}
		included[absPath] = true
		src, err := os.ReadFile(absPath)
		if err != nil {
			panic(r.vm.NewGoError(fmt.Errorf("include %q: %w", name, err)))
		}
		// Record for client-side replication.
		r.SourceFiles = append(r.SourceFiles, domain.GameSourceFile{Name: name, Content: string(src)})
		_, err = r.vm.RunScript(name, string(src))
		if err != nil {
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
