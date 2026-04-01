package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"null-space/common"
)

// jsCallTimeout is how long a JS method can run before being interrupted.
const jsCallTimeout = 2 * time.Second

// traceJS logs entry/exit of a JS method. Returns a function to call on exit.
func traceJS(_ *goja.Runtime, method string) func() {
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

// watchdogJS starts a goroutine that interrupts the VM after timeout.
// Call the returned cancel func when the JS call completes.
func watchdogJS(vm *goja.Runtime, method string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(jsCallTimeout):
			slog.Error("JS call timed out, interrupting VM", "method", method, "timeout", jsCallTimeout)
			vm.Interrupt("timeout: " + method)
		}
	}()
	return func() { close(done) }
}

// LOCK ORDERING INVARIANT
//
// The system has two primary mutexes:
//   1. CentralState.mu   — protects shared game state (state.go)
//   2. jsRuntime.mu       — protects the goja JS VM (this file)
//
// Permitted lock order: jsRuntime.mu → (nothing external)
//
// jsRuntime must NEVER hold or acquire CentralState.mu. To enforce this
// structurally, jsRuntime has no reference to CentralState. All data flows:
//   - Teams data: cached snapshot set by server via SetTeamsCache()
//   - Chat output: buffered channel drained by a server goroutine
//
// Callers (server.go, chrome.go) must release state.mu BEFORE calling
// any jsRuntime Game method (Init, Start, View, OnInput, etc.).

// jsRuntime wraps a goja JS runtime and implements common.Game.
type jsRuntime struct {
	mu      sync.Mutex
	vm      *goja.Runtime
	baseDir string // directory containing the game file (for include() resolution)

	commands    []common.Command
	cachedTeams []map[string]any   // snapshot set by server; read by JS teams()
	logFn       func(string)
	chatCh      chan common.Message // drained by server goroutine; closed on unload

	// game object methods (nil if not defined)
	onPlayerLeave goja.Callable
	onInput       goja.Callable
	viewFn        goja.Callable
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
	showDialogFn func(playerID string, d common.DialogRequest) // injected by server
}

// LoadGame loads and executes a JS file from games/, extracts the Game object
// methods, and returns a common.Game. Init() is NOT called here — the server
// calls it at the splash→playing transition when GamePlayerIDs are set.
func LoadGame(path string, logFn func(string), chatCh chan common.Message) (common.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	rt := &jsRuntime{
		vm:      goja.New(),
		baseDir: filepath.Dir(path),
		logFn:   logFn,
		chatCh:  chatCh,
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

func (r *jsRuntime) Init(savedState any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Init")
	defer traceJS(r.vm, "Init")()
	cancel := watchdogJS(r.vm, "Init")
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

func (r *jsRuntime) Start() {
	if r.startFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Start")
	defer traceJS(r.vm, "Start")()
	cancel := watchdogJS(r.vm, "Start")
	defer cancel()
	_, _ = r.startFn(goja.Undefined())
}

func (r *jsRuntime) registerGlobals() {
	r.vm.Set("log", func(msg string) {
		if r.logFn != nil {
			r.logFn(msg)
		}
	})

	r.vm.Set("chat", func(msg string) {
		if r.chatCh != nil {
			select {
			case r.chatCh <- common.Message{Text: msg}:
			default:
				slog.Warn("JS chat channel full, dropping message", "text", msg)
			}
		}
	})

	r.vm.Set("chatPlayer", func(playerID, msg string) {
		if r.chatCh != nil {
			select {
			case r.chatCh <- common.Message{Text: msg, IsPrivate: true, ToID: playerID}:
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

func (r *jsRuntime) extractGameObject() error {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return fmt.Errorf("script must define a global 'Game' object")
	}

	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return fmt.Errorf("'Game' is not an object")
	}

	// Core game methods
	r.onPlayerLeave = extractCallable(gameObj, "onPlayerLeave")
	r.onInput = extractCallable(gameObj, "onInput")
	r.viewFn = extractCallable(gameObj, "view")
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

func (r *jsRuntime) OnPlayerLeave(playerID string) {
	if r.onPlayerLeave == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerLeave")
	defer traceJS(r.vm, "OnPlayerLeave")()
	cancel := watchdogJS(r.vm, "OnPlayerLeave")
	defer cancel()
	_, _ = r.onPlayerLeave(goja.Undefined(), r.vm.ToValue(playerID))
}

func (r *jsRuntime) OnInput(playerID, key string) {
	if r.onInput == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnInput")
	defer traceJS(r.vm, "OnInput")()
	cancel := watchdogJS(r.vm, "OnInput")
	defer cancel()
	_, _ = r.onInput(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(key))
}

func (r *jsRuntime) View(playerID string, width, height int) string {
	if r.viewFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("View")
	defer traceJS(r.vm, "View")()
	cancel := watchdogJS(r.vm, "View")
	defer cancel()
	val, err := r.viewFn(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS View error", "error", err)
		return ""
	}
	return val.String()
}

func (r *jsRuntime) StatusBar(playerID string) string {
	if r.statusBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("StatusBar")
	defer traceJS(r.vm, "StatusBar")()
	cancel := watchdogJS(r.vm, "StatusBar")
	defer cancel()
	val, err := r.statusBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS StatusBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *jsRuntime) CommandBar(playerID string) string {
	if r.commandBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("CommandBar")
	defer traceJS(r.vm, "CommandBar")()
	cancel := watchdogJS(r.vm, "CommandBar")
	defer cancel()
	val, err := r.commandBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS CommandBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *jsRuntime) Commands() []common.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]common.Command, len(r.commands))
	copy(result, r.commands)
	return result
}

func (r *jsRuntime) Unload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vm.Interrupt("game unloaded")
}

func (r *jsRuntime) Menus() []common.MenuDef {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menus
}

// SetTeamsCache replaces the cached teams snapshot that JS teams() returns.
// Called by the server after loading teams or when a player reconnects.
func (r *jsRuntime) SetTeamsCache(teams []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedTeams = teams
}

// --- Lifecycle methods (part of Game interface) ---

func (r *jsRuntime) GameName() string {
	return r.gameNameProp
}

func (r *jsRuntime) TeamRange() common.TeamRange {
	return r.teamRangeProp
}

func (r *jsRuntime) SplashScreen() string {
	return r.splashScreenProp
}

// IsGameOverPending returns true if JS called gameOver() and clears the flag.
func (r *jsRuntime) IsGameOverPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.gameOverPending {
		return false
	}
	r.gameOverPending = false
	return true
}

// GameOverResults returns the results array passed to gameOver().
func (r *jsRuntime) GameOverResults() []common.GameResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameOverResults
}

// GameOverStateExport returns the state object passed as the second arg to gameOver().
func (r *jsRuntime) GameOverStateExport() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gameOverState == nil || goja.IsUndefined(r.gameOverState) || goja.IsNull(r.gameOverState) {
		return nil
	}
	return r.gameOverState.Export()
}

func (r *jsRuntime) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in game method", "method", method, "panic", rec)
	}
}
