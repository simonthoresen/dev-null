package engine

// Tests for game-loop hooks (Update, OnInput, OnPlayerLeave, End, Unload)
// and property methods (StatusBar, CommandBar, Commands, TeamRange,
// RenderStarting, RenderEnding).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

// loadHookRuntime compiles js as a single-file game and returns the Runtime.
// chatCh is buffered so JS chat() calls don't block.
func loadHookRuntime(t *testing.T, js string) *Runtime {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "game.js")
	if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	chatCh := make(chan domain.Message, 32)
	clock := &domain.MockClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	g, err := LoadGame(path, func(string) {}, chatCh, clock, dir)
	if err != nil {
		t.Fatalf("LoadGame: %v", err)
	}
	return g.(*Runtime)
}

// gameState reads Game.state from the JS VM as a map.
func gameState(rt *Runtime) map[string]any {
	s := rt.State()
	if s == nil {
		return nil
	}
	m, _ := s.(map[string]any)
	return m
}

func stateStr(rt *Runtime, key string) string {
	m := gameState(rt)
	if m == nil {
		return ""
	}
	v := m[key]
	if v == nil {
		return ""
	}
	return v.(string)
}

func stateBool(rt *Runtime, key string) bool {
	m := gameState(rt)
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestUpdate_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { called: false, lastKey: "" },
			load: function() {},
			update: function(dt) { Game.state.called = true; Game.state.lastKey = "" + dt; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.Update(0.5)
	if !stateBool(rt, "called") {
		t.Error("expected update() to be called")
	}
	if got := stateStr(rt, "lastKey"); got != "0.5" {
		t.Errorf("expected lastKey='0.5', got %q", got)
	}
}

func TestUpdate_NoHook_IsNoop(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	// Should not panic even when updateFn is nil.
	rt.Update(0.1)
}

// ─── OnInput ─────────────────────────────────────────────────────────────────

func TestOnInput_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { lastInput: "" },
			load: function() {},
			onInput: function(pid, key) { Game.state.lastInput = pid + ":" + key; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.OnInput("player1", "enter")
	if got := stateStr(rt, "lastInput"); got != "player1:enter" {
		t.Errorf("expected 'player1:enter', got %q", got)
	}
}

func TestOnInput_NoHook_IsNoop(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	rt.OnInput("p1", "x") // must not panic
}

// ─── OnPlayerLeave ───────────────────────────────────────────────────────────

func TestOnPlayerLeave_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { leftPlayer: "" },
			load: function() {},
			onPlayerLeave: function(pid) { Game.state.leftPlayer = pid; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.OnPlayerLeave("alice")
	if got := stateStr(rt, "leftPlayer"); got != "alice" {
		t.Errorf("expected 'alice', got %q", got)
	}
}

func TestOnPlayerLeave_NoHook_IsNoop(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	rt.OnPlayerLeave("p1") // must not panic
}

// ─── End ─────────────────────────────────────────────────────────────────────

func TestEnd_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { ended: false },
			load: function() {},
			end: function() { Game.state.ended = true; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.End()
	if !stateBool(rt, "ended") {
		t.Error("expected end() to be called")
	}
}

func TestEnd_NoHook_IsNoop(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	rt.End() // must not panic
}

// ─── Unload ──────────────────────────────────────────────────────────────────

func TestUnload_ReturnsState(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			unload: function() { return { score: 99, label: "hi" }; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	result := rt.Unload()
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map from Unload, got %T: %v", result, result)
	}
	if score, _ := m["score"].(int64); score != 99 {
		t.Errorf("expected score=99, got %v", m["score"])
	}
	if label, _ := m["label"].(string); label != "hi" {
		t.Errorf("expected label='hi', got %v", m["label"])
	}
}

func TestUnload_NoHook_ReturnsNil(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	result := rt.Unload()
	if result != nil {
		t.Errorf("expected nil from Unload when no hook, got %v", result)
	}
}

func TestUnload_NullReturn_ReturnsNil(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			unload: function() { return null; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	result := rt.Unload()
	if result != nil {
		t.Errorf("expected nil for null return, got %v", result)
	}
}

// ─── Begin ───────────────────────────────────────────────────────────────────

func TestBegin_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { begun: false },
			load: function() {},
			begin: function() { Game.state.begun = true; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.Begin()
	if !stateBool(rt, "begun") {
		t.Error("expected begin() to be called")
	}
}

// ─── StatusBar / CommandBar ───────────────────────────────────────────────────

func TestStatusBar_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			statusBar: function(pid) { return "status:" + pid; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	got := rt.StatusBar("player1")
	if got != "status:player1" {
		t.Errorf("expected 'status:player1', got %q", got)
	}
}

func TestStatusBar_NoHook_ReturnsEmpty(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	if got := rt.StatusBar("p1"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCommandBar_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			commandBar: function(pid) { return "cmd:" + pid; },
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	got := rt.CommandBar("player1")
	if got != "cmd:player1" {
		t.Errorf("expected 'cmd:player1', got %q", got)
	}
}

func TestCommandBar_NoHook_ReturnsEmpty(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	if got := rt.CommandBar("p1"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func TestCommands_RegisteredViaJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {
				registerCommand({
					name: "test-cmd",
					description: "a test command",
					handler: function(ctx, args) { ctx.reply("done"); }
				});
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	cmds := rt.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Name != "test-cmd" {
		t.Errorf("expected name='test-cmd', got %q", cmds[0].Name)
	}
	if cmds[0].Description != "a test command" {
		t.Errorf("expected description='a test command', got %q", cmds[0].Description)
	}
}

func TestCommands_Empty(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	if cmds := rt.Commands(); len(cmds) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cmds))
	}
}

// ─── TeamRange ───────────────────────────────────────────────────────────────

func TestTeamRange_FromJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			teamRange: { min: 2, max: 6 },
			load: function() {},
			renderAscii: function() {},
		};
	`)
	tr := rt.TeamRange()
	if tr.Min != 2 || tr.Max != 6 {
		t.Errorf("expected {2,6}, got {%d,%d}", tr.Min, tr.Max)
	}
}

func TestTeamRange_Default(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	tr := rt.TeamRange()
	if tr.Min != 0 || tr.Max != 0 {
		t.Errorf("expected {0,0}, got {%d,%d}", tr.Min, tr.Max)
	}
}

// ─── RenderStarting / RenderEnding ───────────────────────────────────────────

func TestRenderStarting_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { startingCalled: false },
			load: function() {},
			renderGameStart: function(buf, pid, ox, oy, w, h) {
				Game.state.startingCalled = true;
				return true;
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	buf := render.NewImageBuffer(20, 5)
	result := rt.RenderStarting(buf, "p1", 0, 0, 20, 5)
	if !result {
		t.Error("expected RenderStarting to return true")
	}
	if !stateBool(rt, "startingCalled") {
		t.Error("expected renderGameStart() to be called")
	}
}

func TestRenderStarting_NoHook_ReturnsFalse(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	buf := render.NewImageBuffer(20, 5)
	if rt.RenderStarting(buf, "p1", 0, 0, 20, 5) {
		t.Error("expected false when no renderGameStart hook")
	}
}

func TestRenderEnding_CallsJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { endingCalled: false },
			load: function() {},
			renderGameEnd: function(buf, pid, ox, oy, w, h, results) {
				Game.state.endingCalled = true;
				return true;
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	buf := render.NewImageBuffer(20, 5)
	results := []domain.GameResult{{Name: "alice", Result: "10 pts"}}
	result := rt.RenderEnding(buf, "p1", 0, 0, 20, 5, results)
	if !result {
		t.Error("expected RenderEnding to return true")
	}
	if !stateBool(rt, "endingCalled") {
		t.Error("expected renderGameEnd() to be called")
	}
}

func TestRenderEnding_NoHook_ReturnsFalse(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	rt.Load(nil)
	buf := render.NewImageBuffer(20, 5)
	if rt.RenderEnding(buf, "p1", 0, 0, 20, 5, nil) {
		t.Error("expected false when no renderGameEnd hook")
	}
}

// ─── SetTeamsCache / State ────────────────────────────────────────────────────

func TestSetTeamsCache(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { teamCount: 0 },
			load: function() {},
			update: function(dt) {
				var ts = teams();
				Game.state.teamCount = ts.length;
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	rt.SetTeamsCache([]map[string]any{
		{"id": 0, "name": "red"},
		{"id": 1, "name": "blue"},
	})
	rt.Update(0.1)
	m := gameState(rt)
	if count, _ := m["teamCount"].(int64); count != 2 {
		t.Errorf("expected teamCount=2, got %v", m["teamCount"])
	}
}

// ─── GameName ─────────────────────────────────────────────────────────────────

func TestGameName_FromJS(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { gameName: "my-game", load: function() {}, render: function() {} };
	`)
	if got := rt.GameName(); got != "my-game" {
		t.Errorf("expected 'my-game', got %q", got)
	}
}

func TestGameName_EmptyWhenNotDefined(t *testing.T) {
	// GameName() returns "" when no gameName property is set in JS.
	// The caller (chrome view) falls back to the filename stem.
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	if got := rt.GameName(); got != "" {
		t.Errorf("expected empty GameName when not defined in JS, got %q", got)
	}
}

// ─── HasCanvasMode ────────────────────────────────────────────────────────────

func TestHasCanvasMode_True(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			renderCanvas: function(ctx, pid, w, h) {},
			renderAscii: function() {},
		};
	`)
	if !rt.HasCanvasMode() {
		t.Error("expected HasCanvasMode=true")
	}
}

func TestHasCanvasMode_False(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = { load: function() {}, renderAscii: function() {} };
	`)
	if rt.HasCanvasMode() {
		t.Error("expected HasCanvasMode=false")
	}
}

// ─── IsGameOverPending / GameOverResults ─────────────────────────────────────

func TestIsGameOverPending_AfterGameOver(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			load: function() {},
			onInput: function(pid, key) {
				if (key === "q") gameOver([{name: pid, result: "done"}]);
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(nil)
	if rt.IsGameOverPending() {
		t.Error("expected IsGameOverPending=false before input")
	}
	rt.OnInput("alice", "q")
	if !rt.IsGameOverPending() {
		t.Error("expected IsGameOverPending=true after gameOver()")
	}
	results := rt.GameOverResults()
	if len(results) != 1 || results[0].Name != "alice" || results[0].Result != "done" {
		t.Errorf("unexpected results: %v", results)
	}
}

// ─── Load with savedState ────────────────────────────────────────────────────

func TestLoad_PassesSavedState(t *testing.T) {
	rt := loadHookRuntime(t, `
		var Game = {
			state: { score: 0 },
			load: function(saved) {
				if (saved && saved.score) Game.state.score = saved.score;
			},
			renderAscii: function() {},
		};
	`)
	rt.Load(map[string]any{"score": int64(77)})
	m := gameState(rt)
	if score, _ := m["score"].(int64); score != 77 {
		t.Errorf("expected score=77, got %v", m["score"])
	}
}
