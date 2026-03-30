package server

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/dop251/goja"

	"null-space/common"
)

// jsRuntime wraps a goja JS runtime and implements common.App.
type jsRuntime struct {
	mu    sync.Mutex
	vm    *goja.Runtime
	state *CentralState

	commands []common.Command
	logFn    func(string)
	chatFn   func(common.Message)

	// game object methods (nil if not defined)
	onPlayerJoin  goja.Callable
	onPlayerLeave goja.Callable
	onInput       goja.Callable
	viewFn        goja.Callable
	statusBarFn   goja.Callable
	commandBarFn  goja.Callable
}

// LoadApp loads and executes a JS file from apps/, extracts the Game object
// methods, and returns a common.App.
func LoadApp(path string, state *CentralState, logFn func(string), chatFn func(common.Message)) (common.App, error) {
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

	r.vm.Set("players", func() []map[string]interface{} {
		players := r.state.ListPlayers()
		result := make([]map[string]interface{}, 0, len(players))
		for _, p := range players {
			result = append(result, map[string]interface{}{
				"id":      p.ID,
				"name":    p.Name,
				"isAdmin": p.IsAdmin,
			})
		}
		return result
	})

	r.vm.Set("registerCommand", func(spec map[string]interface{}) {
		name, _ := spec["name"].(string)
		desc, _ := spec["description"].(string)
		adminOnly, _ := spec["adminOnly"].(bool)
		firstArgIsPlayer, _ := spec["firstArgIsPlayer"].(bool)
		handler, _ := spec["handler"].(goja.Callable)

		if name == "" || handler == nil {
			slog.Warn("JS registerCommand: name and handler are required")
			return
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

	r.onPlayerJoin = extractCallable(gameObj, "onPlayerJoin")
	r.onPlayerLeave = extractCallable(gameObj, "onPlayerLeave")
	r.onInput = extractCallable(gameObj, "onInput")
	r.viewFn = extractCallable(gameObj, "view")
	r.statusBarFn = extractCallable(gameObj, "statusBar")
	r.commandBarFn = extractCallable(gameObj, "commandBar")

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

func (r *jsRuntime) OnPlayerJoin(playerID, playerName string) {
	if r.onPlayerJoin == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerJoin")
	_, _ = r.onPlayerJoin(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(playerName))
}

func (r *jsRuntime) OnPlayerLeave(playerID string) {
	if r.onPlayerLeave == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerLeave")
	_, _ = r.onPlayerLeave(goja.Undefined(), r.vm.ToValue(playerID))
}

func (r *jsRuntime) OnInput(playerID, key string) {
	if r.onInput == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnInput")
	_, _ = r.onInput(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(key))
}

func (r *jsRuntime) View(playerID string, width, height int) string {
	if r.viewFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("View")
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

func (r *jsRuntime) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in game method", "method", method, "panic", rec)
	}
}
