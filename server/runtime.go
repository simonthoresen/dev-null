package server

import (
	"fmt"
	"log/slog"
	"os"
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

// jsRuntime wraps a goja JS runtime and implements common.Game.
type jsRuntime struct {
	mu    sync.Mutex
	vm    *goja.Runtime
	state *CentralState

	commands []common.Command
	logFn    func(string)
	chatFn   func(common.Message)

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
}

// LoadGame loads and executes a JS file from games/, extracts the Game object
// methods, and returns a common.Game. Init() is NOT called here — the server
// calls it at the splash→playing transition when GamePlayerIDs are set.
func LoadGame(path string, state *CentralState, logFn func(string), chatFn func(common.Message)) (common.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	rt := &jsRuntime{
		vm:     goja.New(),
		state:  state,
		logFn:  logFn,
		chatFn: chatFn,
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
		if r.chatFn != nil {
			r.chatFn(common.Message{Text: msg})
		}
	})

	r.vm.Set("chatPlayer", func(playerID, msg string) {
		if r.chatFn != nil {
			r.chatFn(common.Message{
				Text:      msg,
				IsPrivate: true,
				ToID:      playerID,
			})
		}
	})

	r.vm.Set("teams", func() []map[string]any {
		// During a game, return the game teams snapshot.
		var teams []common.Team
		if r.state.GetGamePhase() != common.PhaseNone {
			teams = r.state.GetGameTeams()
		} else {
			teams = r.state.GetTeams()
		}
		result := make([]map[string]any, 0, len(teams))
		for _, t := range teams {
			playerList := make([]any, 0, len(t.Players))
			for _, pid := range t.Players {
				entry := map[string]any{"id": pid}
				if p := r.state.GetPlayer(pid); p != nil {
					entry["name"] = p.Name
				} else {
					entry["name"] = pid // disconnected — use ID as fallback
				}
				playerList = append(playerList, entry)
			}
			result = append(result, map[string]any{
				"name":    t.Name,
				"color":   t.Color,
				"players": playerList,
			})
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
