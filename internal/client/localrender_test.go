package client

import (
	"encoding/json"
	"testing"
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
