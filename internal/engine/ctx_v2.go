package engine

import (
	"fmt"
	"log/slog"

	"github.com/dop251/goja"

	"dev-null/internal/domain"
)

// buildCtxObject constructs the server-only ctx object passed to v2 lifecycle
// hooks (init, begin, update, end). All side-effecty framework capabilities
// that v1 exposed as globals (chat, midi*, gameOver, showDialog, log,
// registerCommand, …) live here. Render hooks never receive ctx, so render
// calling any of these throws a TypeError — impurity becomes a type error
// instead of a silent divergence.
//
// The logic mirrors v1's registerGlobals entries faithfully; when v1 is
// retired (task 13) the duplication goes away along with the globals.
func (r *Runtime) buildCtxObject() *goja.Object {
	ctx := r.vm.NewObject()

	// --- logging ---
	ctx.Set("log", func(msg string) {
		if r.logFn != nil {
			r.logFn(msg)
		}
	})

	// --- chat ---
	ctx.Set("chat", func(msg string) {
		if r.chatCh == nil {
			return
		}
		select {
		case r.chatCh <- domain.Message{Text: msg}:
		default:
			slog.Warn("v2 ctx.chat channel full", "text", msg)
		}
	})
	ctx.Set("chatPlayer", func(playerID, msg string) {
		if r.chatCh == nil {
			return
		}
		select {
		case r.chatCh <- domain.Message{Text: msg, IsPrivate: true, ToID: playerID}:
		default:
			slog.Warn("v2 ctx.chatPlayer channel full")
		}
	})

	// --- sound ---
	ctx.Set("playSound", func(call goja.FunctionCall) goja.Value {
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
		}
		return goja.Undefined()
	})
	ctx.Set("stopSound", func(call goja.FunctionCall) goja.Value {
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
		}
		return goja.Undefined()
	})

	// --- MIDI ---
	ctx.Set("midiNote", func(call goja.FunctionCall) goja.Value {
		ch := int(call.Argument(0).ToInteger())
		note := int(call.Argument(1).ToInteger())
		vel := int(call.Argument(2).ToInteger())
		dur := int(call.Argument(3).ToInteger())
		r.sendMidiBroadcast(newNoteOnEvent(ch, note, vel, dur))
		return goja.Undefined()
	})
	ctx.Set("midiNotePlayer", func(call goja.FunctionCall) goja.Value {
		pid := call.Argument(0).String()
		ch := int(call.Argument(1).ToInteger())
		note := int(call.Argument(2).ToInteger())
		vel := int(call.Argument(3).ToInteger())
		dur := int(call.Argument(4).ToInteger())
		r.sendMidiPlayer(pid, newNoteOnEvent(ch, note, vel, dur))
		return goja.Undefined()
	})
	ctx.Set("midiProgram", func(call goja.FunctionCall) goja.Value {
		ch := int(call.Argument(0).ToInteger())
		prog := int(call.Argument(1).ToInteger())
		r.sendMidiBroadcast(newProgramChangeEvent(ch, prog))
		return goja.Undefined()
	})
	ctx.Set("midiProgramPlayer", func(call goja.FunctionCall) goja.Value {
		pid := call.Argument(0).String()
		ch := int(call.Argument(1).ToInteger())
		prog := int(call.Argument(2).ToInteger())
		r.sendMidiPlayer(pid, newProgramChangeEvent(ch, prog))
		return goja.Undefined()
	})
	ctx.Set("midiCC", func(call goja.FunctionCall) goja.Value {
		ch := int(call.Argument(0).ToInteger())
		ctrl := int(call.Argument(1).ToInteger())
		val := int(call.Argument(2).ToInteger())
		r.sendMidiBroadcast(newControlChangeEvent(ch, ctrl, val))
		return goja.Undefined()
	})

	// --- teams snapshot (read-only; v2 games typically read state.teams instead) ---
	ctx.Set("teams", func() []map[string]any {
		return cloneTeams(r.cachedTeams)
	})

	// --- game over ---
	ctx.Set("gameOver", func(call goja.FunctionCall) goja.Value {
		r.gameOverPending = true
		r.gameOverResults = nil
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		arg := call.Argument(0)
		if arg == nil || goja.IsUndefined(arg) || goja.IsNull(arg) {
			return goja.Undefined()
		}
		obj := arg.ToObject(r.vm)
		if obj == nil {
			return goja.Undefined()
		}
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
		return goja.Undefined()
	})

	// --- dialogs ---
	ctx.Set("showDialog", func(call goja.FunctionCall) goja.Value {
		playerID := ""
		if v := call.Argument(0); !goja.IsUndefined(v) {
			playerID = v.String()
		}
		optsVal := call.Argument(1)
		if goja.IsUndefined(optsVal) || goja.IsNull(optsVal) {
			return goja.Undefined()
		}
		opts := optsVal.ToObject(r.vm)
		dlg := domain.DialogRequest{}
		if v := opts.Get("title"); v != nil && !goja.IsUndefined(v) {
			dlg.Title = v.String()
		}
		if v := opts.Get("message"); v != nil && !goja.IsUndefined(v) {
			dlg.Body = v.String()
		}
		if v := opts.Get("buttons"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			arr := v.ToObject(r.vm)
			for _, k := range arr.Keys() {
				if el := arr.Get(k); el != nil && !goja.IsUndefined(el) {
					dlg.Buttons = append(dlg.Buttons, el.String())
				}
			}
		}
		if v := opts.Get("onClose"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			if cb, ok := goja.AssertFunction(v); ok {
				dlg.OnClose = func(button string) {
					r.mu.Lock()
					defer r.mu.Unlock()
					_, _ = cb(goja.Undefined(), r.vm.ToValue(button))
				}
			}
		}
		if r.showDialogFn != nil {
			go r.showDialogFn(playerID, dlg)
		}
		return goja.Undefined()
	})

	// --- command registration ---
	ctx.Set("registerCommand", func(call goja.FunctionCall) goja.Value {
		specVal := call.Argument(0)
		if goja.IsUndefined(specVal) || goja.IsNull(specVal) {
			return goja.Undefined()
		}
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
			slog.Warn("v2 ctx.registerCommand: name and handler required")
			return goja.Undefined()
		}
		cmd := domain.Command{
			Name:             name,
			Description:      desc,
			AdminOnly:        adminOnly,
			FirstArgIsPlayer: firstArgIsPlayer,
			Handler: func(cctx domain.CommandContext, args []string) {
				r.mu.Lock()
				defer r.mu.Unlock()
				jsArgs := make([]interface{}, len(args))
				for i, a := range args {
					jsArgs[i] = a
				}
				_, err := handler(goja.Undefined(),
					r.vm.ToValue(cctx.PlayerID),
					r.vm.ToValue(cctx.IsAdmin),
					r.vm.ToValue(jsArgs),
				)
				if err != nil {
					slog.Error("v2 JS command handler error", "name", name, "error", err)
					cctx.Reply(fmt.Sprintf("Command error: %v", err))
				}
			},
		}
		r.commands = append(r.commands, cmd)
		return goja.Undefined()
	})

	// --- time ---
	ctx.Set("now", func() int64 {
		return r.clock.Now().UnixMilli()
	})

	// --- ephemeral events (stub; ns;event pipe lands in a follow-up) ---
	ctx.Set("emit", func(call goja.FunctionCall) goja.Value {
		// Queued for broadcast when the ns;event pipe lands. For now we drop
		// silently; v2 games written against this API are forward-compatible.
		_ = call
		return goja.Undefined()
	})

	return ctx
}

// sendMidiBroadcast pushes a MIDI event to every player through chatCh.
func (r *Runtime) sendMidiBroadcast(ev domain.MidiEvent) {
	if r.chatCh == nil {
		return
	}
	select {
	case r.chatCh <- domain.Message{MidiEvents: []domain.MidiEvent{ev}}:
	default:
		slog.Warn("v2 ctx midi broadcast channel full")
	}
}

// sendMidiPlayer pushes a MIDI event to one specific player.
func (r *Runtime) sendMidiPlayer(pid string, ev domain.MidiEvent) {
	if r.chatCh == nil {
		return
	}
	select {
	case r.chatCh <- domain.Message{MidiEvents: []domain.MidiEvent{ev}, IsPrivate: true, ToID: pid}:
	default:
		slog.Warn("v2 ctx midi player channel full")
	}
}

// cloneTeams deep-copies the cached teams snapshot so JS mutation can't
// corrupt the framework's view.
func cloneTeams(src []map[string]any) []map[string]any {
	out := make([]map[string]any, len(src))
	for i, t := range src {
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
					continue
				}
			}
			cp[k] = v
		}
		out[i] = cp
	}
	return out
}
