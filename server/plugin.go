package server

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/dop251/goja"

	"null-space/common"
)

// jsPlugin wraps a goja JS runtime and implements common.Plugin.
type jsPlugin struct {
	mu    sync.Mutex
	vm    *goja.Runtime
	state *CentralState

	commands []common.Command
	logFn    func(string)
	chatFn   func(common.Message)

	onChatMessageFn goja.Callable
	onPlayerJoinFn  goja.Callable
	onPlayerLeaveFn goja.Callable

	skin *common.SkinColors // nil if Plugin.skin is not defined
}

// LoadPlugin loads and executes a JS file from plugins/, extracts the Plugin
// object methods, and returns a common.Plugin.
func LoadPlugin(path string, state *CentralState, logFn func(string), chatFn func(common.Message)) (common.Plugin, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin file: %w", err)
	}

	p := &jsPlugin{
		vm:     goja.New(),
		state:  state,
		logFn:  logFn,
		chatFn: chatFn,
	}

	p.registerGlobals()

	if _, err := p.vm.RunScript(path, string(src)); err != nil {
		return nil, fmt.Errorf("execute plugin script: %w", err)
	}

	if err := p.extractPluginObject(); err != nil {
		return nil, fmt.Errorf("extract plugin object: %w", err)
	}

	return p, nil
}

func (p *jsPlugin) registerGlobals() {
	p.vm.Set("log", func(msg string) {
		if p.logFn != nil {
			p.logFn(msg)
		}
	})

	p.vm.Set("chat", func(msg string) {
		if p.chatFn != nil {
			p.chatFn(common.Message{Text: msg})
		}
	})

	p.vm.Set("chatPlayer", func(playerID, msg string) {
		if p.chatFn != nil {
			p.chatFn(common.Message{
				Text:      msg,
				IsPrivate: true,
				ToID:      playerID,
			})
		}
	})

	p.vm.Set("players", func() []map[string]interface{} {
		players := p.state.ListPlayers()
		result := make([]map[string]interface{}, 0, len(players))
		for _, pl := range players {
			result = append(result, map[string]interface{}{
				"id":      pl.ID,
				"name":    pl.Name,
				"isAdmin": pl.IsAdmin,
			})
		}
		return result
	})

	p.vm.Set("figlet", func(text string, font string) string {
		return Figlet(text, font)
	})

	p.vm.Set("registerCommand", func(call goja.FunctionCall) goja.Value {
		specVal := call.Argument(0)
		specObj := specVal.ToObject(p.vm)

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
			slog.Warn("plugin registerCommand: name and handler are required")
			return goja.Undefined()
		}

		cmd := common.Command{
			Name:             name,
			Description:      desc,
			AdminOnly:        adminOnly,
			FirstArgIsPlayer: firstArgIsPlayer,
			Handler: func(ctx common.CommandContext, args []string) {
				p.mu.Lock()
				defer p.mu.Unlock()

				jsArgs := make([]interface{}, len(args))
				for i, a := range args {
					jsArgs[i] = a
				}
				_, err := handler(goja.Undefined(),
					p.vm.ToValue(ctx.PlayerID),
					p.vm.ToValue(ctx.IsAdmin),
					p.vm.ToValue(jsArgs),
				)
				if err != nil {
					slog.Error("JS plugin command handler error", "name", name, "error", err)
					ctx.Reply(fmt.Sprintf("Command error: %v", err))
				}
			},
		}
		p.commands = append(p.commands, cmd)
		return goja.Undefined()
	})
}

func (p *jsPlugin) extractPluginObject() error {
	val := p.vm.Get("Plugin")
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return fmt.Errorf("script must define a global 'Plugin' object")
	}
	obj := val.ToObject(p.vm)
	if obj == nil {
		return fmt.Errorf("'Plugin' is not an object")
	}
	p.onChatMessageFn = extractCallable(obj, "onChatMessage")
	p.onPlayerJoinFn = extractCallable(obj, "onPlayerJoin")
	p.onPlayerLeaveFn = extractCallable(obj, "onPlayerLeave")

	if skinVal := obj.Get("skin"); skinVal != nil && !goja.IsUndefined(skinVal) && !goja.IsNull(skinVal) {
		if skinObj := skinVal.ToObject(p.vm); skinObj != nil {
			p.skin = &common.SkinColors{
				StatusBg: jsObjString(skinObj, "statusBg"),
				StatusFg: jsObjString(skinObj, "statusFg"),
				ChatBg:   jsObjString(skinObj, "chatBg"),
				ChatFg:   jsObjString(skinObj, "chatFg"),
				CmdBg:    jsObjString(skinObj, "cmdBg"),
				CmdFg:    jsObjString(skinObj, "cmdFg"),
				InputBg:  jsObjString(skinObj, "inputBg"),
				InputFg:  jsObjString(skinObj, "inputFg"),
			}
		}
	}

	return nil
}

func jsObjString(obj *goja.Object, key string) string {
	v := obj.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}

// --- common.Plugin implementation ---

func (p *jsPlugin) OnChatMessage(msg *common.Message) *common.Message {
	if p.onChatMessageFn == nil {
		return msg
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.recoverJS("OnChatMessage")
	defer traceJS(p.vm, "plugin.OnChatMessage")()
	cancel := watchdogJS(p.vm, "plugin.OnChatMessage")
	defer cancel()

	jsMsg := p.vm.NewObject()
	_ = jsMsg.Set("author", msg.Author)
	_ = jsMsg.Set("text", msg.Text)
	_ = jsMsg.Set("isPrivate", msg.IsPrivate)
	_ = jsMsg.Set("toID", msg.ToID)
	_ = jsMsg.Set("fromID", msg.FromID)

	result, err := p.onChatMessageFn(goja.Undefined(), jsMsg)
	if err != nil {
		slog.Error("JS plugin OnChatMessage error", "error", err)
		return msg // pass through on error
	}
	if goja.IsNull(result) || goja.IsUndefined(result) {
		return nil // message dropped
	}

	resultObj := result.ToObject(p.vm)
	if resultObj == nil {
		return msg
	}

	modified := *msg
	if v := resultObj.Get("text"); v != nil && !goja.IsUndefined(v) {
		modified.Text = v.String()
	}
	if v := resultObj.Get("author"); v != nil && !goja.IsUndefined(v) {
		modified.Author = v.String()
	}
	return &modified
}

func (p *jsPlugin) OnPlayerJoin(playerID, playerName string) {
	if p.onPlayerJoinFn == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.recoverJS("OnPlayerJoin")
	_, _ = p.onPlayerJoinFn(goja.Undefined(), p.vm.ToValue(playerID), p.vm.ToValue(playerName))
}

func (p *jsPlugin) OnPlayerLeave(playerID string) {
	if p.onPlayerLeaveFn == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.recoverJS("OnPlayerLeave")
	_, _ = p.onPlayerLeaveFn(goja.Undefined(), p.vm.ToValue(playerID))
}

func (p *jsPlugin) Commands() []common.Command {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]common.Command, len(p.commands))
	copy(result, p.commands)
	return result
}

func (p *jsPlugin) Skin() *common.SkinColors {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.skin
}

func (p *jsPlugin) Unload() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vm.Interrupt("plugin unloaded")
}

func (p *jsPlugin) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in plugin method", "method", method, "panic", rec)
	}
}
