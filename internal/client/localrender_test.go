package client

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// loadMinimalGame puts a skeletal Game object in the VM so the renderer's
// loaded-guard passes without us having to ship real game source through
// the gamesrc OSC pipeline.
func loadMinimalGame(t *testing.T) *LocalRenderer {
	t.Helper()
	lr := NewLocalRenderer()
	lr.LoadGame([]GameSrcFile{
		{Name: "stub.js", Content: `
			var Game = {
				contract: 2,
				state: { seed: "from-load" }
			};
		`},
	})
	if !lr.IsLoaded() {
		t.Fatal("stub game did not load")
	}
	return lr
}

func readGameState(t *testing.T, lr *LocalRenderer) map[string]any {
	t.Helper()
	lr.mu.Lock()
	defer lr.mu.Unlock()
	v, err := lr.vm.RunString(`JSON.stringify(Game.state)`)
	if err != nil {
		t.Fatalf("read Game.state: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(v.String()), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestSetState_ReplacesWholesale(t *testing.T) {
	lr := loadMinimalGame(t)
	lr.SetState([]byte(`{"a":1,"b":{"c":2}}`))
	got := readGameState(t, lr)
	if got["a"].(float64) != 1 || got["b"].(map[string]any)["c"].(float64) != 2 {
		t.Errorf("unexpected state after SetState: %v", got)
	}
	// Seed from load() should have been overwritten.
	if _, ok := got["seed"]; ok {
		t.Errorf("SetState should replace, but 'seed' from load() survived: %v", got)
	}
}

func TestMergeStatePatch_AddsAndUpdatesKeys(t *testing.T) {
	lr := loadMinimalGame(t)
	lr.SetState([]byte(`{"a":1,"b":2}`))
	lr.MergeStatePatch([]byte(`{"b":99,"c":"new"}`))
	got := readGameState(t, lr)
	if got["a"].(float64) != 1 {
		t.Errorf("unchanged 'a' should survive merge, got %v", got["a"])
	}
	if got["b"].(float64) != 99 {
		t.Errorf("'b' should be overwritten to 99, got %v", got["b"])
	}
	if got["c"].(string) != "new" {
		t.Errorf("'c' should be added, got %v", got["c"])
	}
}

func TestMergeStatePatch_NullDeletes(t *testing.T) {
	lr := loadMinimalGame(t)
	lr.SetState([]byte(`{"a":1,"b":2,"c":3}`))
	lr.MergeStatePatch([]byte(`{"b":null}`))
	got := readGameState(t, lr)
	if _, ok := got["b"]; ok {
		t.Errorf("'b' should be deleted by null patch, got %v", got)
	}
	if got["a"].(float64) != 1 || got["c"].(float64) != 3 {
		t.Errorf("other keys should survive, got %v", got)
	}
}

func TestMergeStatePatch_WithoutBaselineIsNoop(t *testing.T) {
	lr := loadMinimalGame(t)
	// load() seeded {seed:"from-load"}. Applying a patch before any SetState
	// baseline should still merge — the merge only needs Game.state to
	// exist as an object, which load() guaranteed.
	lr.MergeStatePatch([]byte(`{"patched":true}`))
	got := readGameState(t, lr)
	if got["patched"] != true {
		t.Errorf("patch onto load()-seeded state should apply, got %v", got)
	}
	if got["seed"].(string) != "from-load" {
		t.Errorf("untouched 'seed' should remain, got %v", got)
	}
}

// fakeClock returns a controllable time-source for the renderer.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func loadTimeStub(t *testing.T) (*LocalRenderer, *fakeClock, *float64) {
	t.Helper()
	lr := loadMinimalGame(t)
	clk := &fakeClock{now: time.Unix(1700000000, 0)}
	lr.now = clk.Now
	var lastDrift float64 = math.NaN()
	lr.driftLogger = func(d float64) { lastDrift = d }
	// expose pointer so callers can read it back
	return lr, clk, &lastDrift
}

func readGameTime(t *testing.T, lr *LocalRenderer) float64 {
	t.Helper()
	got := readGameState(t, lr)
	if v, ok := got["_gameTime"].(float64); ok {
		return v
	}
	t.Fatalf("_gameTime missing or not a number in state: %v", got)
	return 0
}

func TestGameTime_ExtrapolatesBetweenSnapshots(t *testing.T) {
	lr, clk, _ := loadTimeStub(t)
	lr.SetState([]byte(`{"_gameTime":10.0}`))

	// Advance wall-clock half a second; render should see _gameTime ~= 10.5
	clk.now = clk.now.Add(500 * time.Millisecond)
	lr.mu.Lock()
	lr.injectLocalGameTime()
	lr.mu.Unlock()
	got := readGameTime(t, lr)
	if math.Abs(got-10.5) > 1e-6 {
		t.Fatalf("expected extrapolated _gameTime ~10.5, got %v", got)
	}

	// Another quarter second of wall-clock without any server input.
	clk.now = clk.now.Add(250 * time.Millisecond)
	lr.mu.Lock()
	lr.injectLocalGameTime()
	lr.mu.Unlock()
	got = readGameTime(t, lr)
	if math.Abs(got-10.75) > 1e-6 {
		t.Fatalf("expected extrapolated _gameTime ~10.75, got %v", got)
	}
}

func TestGameTime_PatchSnapsAndReportsDrift(t *testing.T) {
	lr, clk, lastDrift := loadTimeStub(t)
	lr.SetState([]byte(`{"_gameTime":10.0}`))

	// 1 second of wall clock passes — extrapolated _gameTime = 11.0.
	clk.now = clk.now.Add(time.Second)
	// Server sends a patch claiming _gameTime is 11.5 (drift +0.5 — server
	// must have advanced faster than us, which is exactly what should be
	// reported).
	lr.MergeStatePatch([]byte(`{"_gameTime":11.5}`))

	if math.IsNaN(*lastDrift) {
		t.Fatal("expected drift logger to fire")
	}
	if math.Abs(*lastDrift-0.5) > 1e-6 {
		t.Fatalf("expected reported drift ~+0.5, got %v", *lastDrift)
	}

	// After snap, future renders extrapolate from the new anchor.
	clk.now = clk.now.Add(time.Second)
	lr.mu.Lock()
	lr.injectLocalGameTime()
	lr.mu.Unlock()
	got := readGameTime(t, lr)
	if math.Abs(got-12.5) > 1e-6 {
		t.Fatalf("expected extrapolation from new anchor 11.5+1.0=12.5, got %v", got)
	}
}

func TestGameTime_SmallDriftIsSilent(t *testing.T) {
	lr, clk, lastDrift := loadTimeStub(t)
	lr.SetState([]byte(`{"_gameTime":0.0}`))

	// Wall ticks 1.0s. Server reports 1.05 — drift 0.05s is below threshold.
	clk.now = clk.now.Add(time.Second)
	lr.MergeStatePatch([]byte(`{"_gameTime":1.05}`))
	if !math.IsNaN(*lastDrift) {
		t.Fatalf("sub-threshold drift should not fire logger, got %v", *lastDrift)
	}
}

func TestGameTime_BaselineIsSilent(t *testing.T) {
	lr, _, lastDrift := loadTimeStub(t)
	// Two baselines back-to-back must NOT report drift even if the times
	// disagree wildly — baselines reset the anchor unconditionally.
	lr.SetState([]byte(`{"_gameTime":1.0}`))
	lr.SetState([]byte(`{"_gameTime":99.0}`))
	if !math.IsNaN(*lastDrift) {
		t.Fatalf("baseline reset must not call drift logger, got %v", *lastDrift)
	}
}

func TestMergeStatePatch_DoesNotRecurse(t *testing.T) {
	// Depth-1 merge: nested objects are REPLACED, not deep-merged.
	lr := loadMinimalGame(t)
	lr.SetState([]byte(`{"players":{"alice":{"x":1,"y":2},"bob":{"x":3}}}`))
	lr.MergeStatePatch([]byte(`{"players":{"alice":{"x":5}}}`))
	got := readGameState(t, lr)
	players := got["players"].(map[string]any)
	alice := players["alice"].(map[string]any)
	if alice["x"].(float64) != 5 {
		t.Errorf("alice.x should be 5, got %v", alice["x"])
	}
	if _, hadY := alice["y"]; hadY {
		t.Error("depth-1 merge means alice.y is gone — this is the contract")
	}
	if _, hadBob := players["bob"]; hadBob {
		t.Error("depth-1 merge replaces players wholesale — bob should be gone")
	}
}
