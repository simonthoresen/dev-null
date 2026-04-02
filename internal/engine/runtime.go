package engine

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"null-space/common"
)

// JSCallTimeout is how long a JS method can run before being interrupted.
const JSCallTimeout = 2 * time.Second

// TraceJS logs entry/exit of a JS method. Returns a function to call on exit.
func TraceJS(_ *goja.Runtime, method string) func() {
	start := time.Now()
	slog.Debug("JS enter", "method", method)
	return func() {
		dur := time.Since(start)
		if dur > 100*time.Millisecond {
			slog.Warn("JS slow call", "method", method, "duration", dur)
		} else {
			slog.Debug("JS exit", "method", method, "duration", dur)
		}
	}
}

// WatchdogJS starts a goroutine that interrupts the VM after timeout.
// Call the returned cancel func when the JS call completes.
func WatchdogJS(vm *goja.Runtime, method string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(JSCallTimeout):
			slog.Error("JS call timed out, interrupting VM", "method", method, "timeout", JSCallTimeout)
			vm.Interrupt("timeout: " + method)
		}
	}()
	return func() { close(done) }
}

// LOCK ORDERING INVARIANT
//
// The system has two primary mutexes:
//   1. CentralState.mu   — protects shared game state (state.go)
//   2. JSRuntime.mu       — protects the goja JS VM (this file)
//
// Permitted lock order: JSRuntime.mu → (nothing external)
//
// JSRuntime must NEVER hold or acquire CentralState.mu. To enforce this
// structurally, JSRuntime has no reference to CentralState. All data flows:
//   - Teams data: cached snapshot set by server via SetTeamsCache()
//   - Chat output: buffered channel drained by a server goroutine
//
// Callers (server.go, chrome.go) must release state.mu BEFORE calling
// any JSRuntime Game method (Init, Start, View, OnInput, etc.).

// JSRuntime wraps a goja JS runtime and implements common.Game.
type JSRuntime struct {
	mu      sync.Mutex
	vm      *goja.Runtime
	baseDir string       // directory containing the game file (for include() resolution)
	clock   common.Clock // server clock exposed to JS as now()

	commands    []common.Command
	cachedTeams []map[string]any   // snapshot set by server; read by JS teams()
	logFn       func(string)
	ChatCh      chan common.Message // drained by server goroutine; closed on unload

	// game object methods (nil if not defined)
	updateFn      goja.Callable
	onPlayerLeave goja.Callable
	onInput       goja.Callable
	renderFn      goja.Callable
	renderNCFn    goja.Callable
	statusBarFn   goja.Callable
	commandBarFn  goja.Callable

	// lifecycle
	gameNameProp     string
	teamRangeProp    common.TeamRange
	splashScreenProp string // read from Game.splashScreen after init
	initFn           goja.Callable
	startFn          goja.Callable

	// gameOver() callback state — set by JS, detected by tick loop
	gameOverPending bool
	gameOverResults []common.GameResult // results passed to gameOver()
	gameOverState   goja.Value          // state argument passed as second arg to gameOver()

	menus        []common.MenuDef
	ShowDialogFn func(playerID string, d common.DialogRequest) // injected by server
}

// LoadGame loads and executes a JS file from games/, extracts the Game object
// methods, and returns a common.Game. Init() is NOT called here — the server
// calls it at the splash→playing transition when GamePlayerIDs are set.
func LoadGame(path string, logFn func(string), chatCh chan common.Message, clock common.Clock) (common.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	rt := &JSRuntime{
		vm:      goja.New(),
		baseDir: filepath.Dir(path),
		logFn:   logFn,
		ChatCh:  chatCh,
		clock:   clock,
	}

	rt.registerGlobals()

	_, err = rt.vm.RunScript(path, string(src))
	if err != nil {
		return nil, fmt.Errorf("execute game script: %w", err)
	}

	if err := rt.extractGameObject(); err != nil {
		return nil, fmt.Errorf("extract game object: %w", err)
	}

	return rt, nil
}

func (r *JSRuntime) Init(savedState any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Init")
	defer TraceJS(r.vm, "Init")()
	cancel := WatchdogJS(r.vm, "Init")
	defer cancel()
	_, _ = r.initFn(goja.Undefined(), r.vm.ToValue(savedState))

	// Re-read splashScreen — init() may have set it dynamically.
	gameVal := r.vm.Get("Game")
	if gameVal != nil && !goja.IsUndefined(gameVal) && !goja.IsNull(gameVal) {
		if obj := gameVal.ToObject(r.vm); obj != nil {
			if v := obj.Get("splashScreen"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				r.splashScreenProp = v.String()
			}
		}
	}
}

func (r *JSRuntime) Start() {
	if r.startFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Start")
	defer TraceJS(r.vm, "Start")()
	cancel := WatchdogJS(r.vm, "Start")
	defer cancel()
	_, _ = r.startFn(goja.Undefined())
}

func (r *JSRuntime) registerGlobals() {
	// Pixel attribute constants for buf.setChar/writeString/fill.
	r.vm.Set("ATTR_NONE", int(common.AttrNone))
	r.vm.Set("ATTR_BOLD", int(common.AttrBold))
	r.vm.Set("ATTR_FAINT", int(common.AttrFaint))
	r.vm.Set("ATTR_ITALIC", int(common.AttrItalic))
	r.vm.Set("ATTR_UNDERLINE", int(common.AttrUnderline))
	r.vm.Set("ATTR_REVERSE", int(common.AttrReverse))

	r.vm.Set("log", func(msg string) {
		if r.logFn != nil {
			r.logFn(msg)
		}
	})

	r.vm.Set("chat", func(msg string) {
		if r.ChatCh != nil {
			select {
			case r.ChatCh <- common.Message{Text: msg}:
			default:
				slog.Warn("JS chat channel full, dropping message", "text", msg)
			}
		}
	})

	r.vm.Set("chatPlayer", func(playerID, msg string) {
		if r.ChatCh != nil {
			select {
			case r.ChatCh <- common.Message{Text: msg, IsPrivate: true, ToID: playerID}:
			default:
				slog.Warn("JS chatPlayer channel full, dropping message")
			}
		}
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
		r.gameOverState = nil

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
						entry := common.GameResult{}
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

		// Second arg: state to persist
		if len(call.Arguments) > 1 {
			r.gameOverState = call.Argument(1)
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
		var items []common.MenuItemDef
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
				items = append(items, common.MenuItemDef{
					Label:    itemLabel,
					Disabled: disabled,
					Handler:  goHandler,
				})
			}
		}
		r.menus = append(r.menus, common.MenuDef{Label: label, Items: items})
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
		d := common.DialogRequest{
			Title:   title,
			Body:    message,
			Buttons: buttons,
			OnClose: onClose,
		}
		if r.ShowDialogFn != nil {
			go r.ShowDialogFn(playerID, d)
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

		cmd := common.Command{
			Name:             name,
			Description:      desc,
			AdminOnly:        adminOnly,
			FirstArgIsPlayer: firstArgIsPlayer,
			Handler: func(ctx common.CommandContext, args []string) {
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
		_, err = r.vm.RunScript(name, string(src))
		if err != nil {
			panic(r.vm.NewGoError(fmt.Errorf("include %q: %w", name, err)))
		}
	})
}

func (r *JSRuntime) extractGameObject() error {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return fmt.Errorf("script must define a global 'Game' object")
	}

	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return fmt.Errorf("'Game' is not an object")
	}

	// Core game methods
	r.updateFn = extractCallable(gameObj, "update")
	r.onPlayerLeave = extractCallable(gameObj, "onPlayerLeave")
	r.onInput = extractCallable(gameObj, "onInput")
	r.renderFn = extractCallable(gameObj, "render")
	r.renderNCFn = extractCallable(gameObj, "renderNC")
	r.statusBarFn = extractCallable(gameObj, "statusBar")
	r.commandBarFn = extractCallable(gameObj, "commandBar")

	// init and start are mandatory
	r.initFn = extractCallable(gameObj, "init")
	if r.initFn == nil {
		return fmt.Errorf("Game must define an init(savedState) function")
	}
	r.startFn = extractCallable(gameObj, "start")

	// Read gameName property (string, not callable)
	if v := gameObj.Get("gameName"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		r.gameNameProp = v.String()
	}

	// Read teamRange property: {min, max}
	if v := gameObj.Get("teamRange"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		obj := v.ToObject(r.vm)
		if obj != nil {
			if mv := obj.Get("min"); mv != nil && !goja.IsUndefined(mv) {
				r.teamRangeProp.Min = int(mv.ToInteger())
			}
			if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) {
				r.teamRangeProp.Max = int(mv.ToInteger())
			}
		}
	}

	// Read splashScreen property (string)
	if v := gameObj.Get("splashScreen"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		r.splashScreenProp = v.String()
	}

	return nil
}

func extractCallable(obj *goja.Object, name string) goja.Callable {
	val := obj.Get(name)
	if val == nil || goja.IsUndefined(val) {
		return nil
	}
	fn, ok := goja.AssertFunction(val)
	if !ok {
		return nil
	}
	return fn
}

// Implement common.Game

func (r *JSRuntime) OnPlayerLeave(playerID string) {
	if r.onPlayerLeave == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerLeave")
	defer TraceJS(r.vm, "OnPlayerLeave")()
	cancel := WatchdogJS(r.vm, "OnPlayerLeave")
	defer cancel()
	_, _ = r.onPlayerLeave(goja.Undefined(), r.vm.ToValue(playerID))
}

func (r *JSRuntime) OnInput(playerID, key string) {
	if r.onInput == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnInput")
	defer TraceJS(r.vm, "OnInput")()
	cancel := WatchdogJS(r.vm, "OnInput")
	defer cancel()
	_, _ = r.onInput(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(key))
}

func (r *JSRuntime) Update(dt float64) {
	if r.updateFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Update")
	defer TraceJS(r.vm, "Update")()
	cancel := WatchdogJS(r.vm, "Update")
	defer cancel()
	_, _ = r.updateFn(goja.Undefined(), r.vm.ToValue(dt))
}

func (r *JSRuntime) Render(buf *common.ImageBuffer, playerID string, x, y, width, height int) {
	if r.renderFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Render")
	defer TraceJS(r.vm, "Render")()
	cancel := WatchdogJS(r.vm, "Render")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	_, err := r.renderFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS Render error", "error", err)
	}
}

// newJSImageBuffer creates a JS-friendly wrapper around an ImageBuffer region.
// JS games call buf.setChar(x, y, ch, fg, bg), buf.writeString(x, y, text, fg, bg),
// buf.fill(x, y, w, h, ch, fg, bg) to write directly into the buffer.
func (r *JSRuntime) newJSImageBuffer(buf *common.ImageBuffer, ox, oy, w, h int) map[string]any {
	return map[string]any{
		"width":  w,
		"height": h,
		"setChar": func(call goja.FunctionCall) goja.Value {
			x := int(call.Argument(0).ToInteger())
			y := int(call.Argument(1).ToInteger())
			ch := call.Argument(2).String()
			fg := ParseJSColor(call.Argument(3))
			bg := ParseJSColor(call.Argument(4))
			attr := ParseJSAttr(call.Argument(5))
			if len(ch) > 0 {
				r := []rune(ch)[0]
				buf.SetChar(ox+x, oy+y, r, fg, bg, attr)
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
			buf.WriteString(ox+x, oy+y, text, fg, bg, attr)
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
			buf.Fill(ox+x, oy+y, fw, fh, fillCh, fg, bg, attr)
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
func ParseJSAttr(v goja.Value) common.PixelAttr {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return common.AttrNone
	}
	return common.PixelAttr(v.ToInteger())
}

func (r *JSRuntime) RenderNC(playerID string, width, height int) *common.WidgetNode {
	if r.renderNCFn == nil {
		return nil // framework will fall back to wrapping Render() in a gameview node
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderNC")
	defer TraceJS(r.vm, "RenderNC")()
	cancel := WatchdogJS(r.vm, "RenderNC")
	defer cancel()
	val, err := r.renderNCFn(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS RenderNC error", "error", err)
		return nil
	}
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	return gojaToWidgetNode(r.vm, val)
}

// gojaToWidgetNode recursively converts a goja JS object into a WidgetNode tree.
func gojaToWidgetNode(vm *goja.Runtime, val goja.Value) *common.WidgetNode {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	obj := val.ToObject(vm)
	if obj == nil {
		return nil
	}

	node := &common.WidgetNode{}

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

func (r *JSRuntime) StatusBar(playerID string) string {
	if r.statusBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("StatusBar")
	defer TraceJS(r.vm, "StatusBar")()
	cancel := WatchdogJS(r.vm, "StatusBar")
	defer cancel()
	val, err := r.statusBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS StatusBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *JSRuntime) CommandBar(playerID string) string {
	if r.commandBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("CommandBar")
	defer TraceJS(r.vm, "CommandBar")()
	cancel := WatchdogJS(r.vm, "CommandBar")
	defer cancel()
	val, err := r.commandBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS CommandBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *JSRuntime) Commands() []common.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]common.Command, len(r.commands))
	copy(result, r.commands)
	return result
}

func (r *JSRuntime) Unload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vm.Interrupt("game unloaded")
}

func (r *JSRuntime) Menus() []common.MenuDef {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menus
}

// SetTeamsCache replaces the cached teams snapshot that JS teams() returns.
// Called by the server after loading teams or when a player reconnects.
func (r *JSRuntime) SetTeamsCache(teams []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedTeams = teams
}

// --- Lifecycle methods (part of Game interface) ---

func (r *JSRuntime) GameName() string {
	return r.gameNameProp
}

func (r *JSRuntime) TeamRange() common.TeamRange {
	return r.teamRangeProp
}

func (r *JSRuntime) SplashScreen() string {
	return r.splashScreenProp
}

// IsGameOverPending returns true if JS called gameOver() and clears the flag.
func (r *JSRuntime) IsGameOverPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.gameOverPending {
		return false
	}
	r.gameOverPending = false
	return true
}

// GameOverResults returns the results array passed to gameOver().
func (r *JSRuntime) GameOverResults() []common.GameResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameOverResults
}

// GameOverStateExport returns the state object passed as the second arg to gameOver().
func (r *JSRuntime) GameOverStateExport() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gameOverState == nil || goja.IsUndefined(r.gameOverState) || goja.IsNull(r.gameOverState) {
		return nil
	}
	return r.gameOverState.Export()
}

func (r *JSRuntime) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in game method", "method", method, "panic", rec)
	}
}
