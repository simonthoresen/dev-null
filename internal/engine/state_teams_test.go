package engine

import (
	"testing"
)

// State() should overlay the framework's teams cache onto the exported
// Game.state so clients syncing state see teams without the game having
// to write it.
func TestState_OverlaysTeamsCache(t *testing.T) {
	rt := loadHookRuntime(t, `
		Game = {
			gameName: "tf",
			teamRange: { min: 1, max: 4 },
			state: { score: 7 },
			contract: 2,
			init: function(ctx) { return Game.state; }
		};
	`)
	rt.SetTeamsCache([]map[string]any{
		{"name": "Red", "color": "#FF0000", "players": []any{}},
		{"name": "Blue", "color": "#0000FF", "players": []any{}},
	})

	got, ok := rt.State().(map[string]any)
	if !ok {
		t.Fatalf("State() should return a map, got %T", rt.State())
	}
	if got["score"] != int64(7) && got["score"] != float64(7) {
		t.Errorf("game-authored 'score' should pass through: %v", got["score"])
	}
	teams, ok := got["teams"].([]map[string]any)
	if !ok {
		t.Fatalf("teams should be present as []map[string]any, got %T", got["teams"])
	}
	if len(teams) != 2 || teams[0]["name"] != "Red" {
		t.Errorf("teams overlay malformed: %v", teams)
	}
}

// No teams cache → the state is returned untouched. This keeps the pre-game
// (PhaseStarting, no teams yet) path clean: render hooks that look for
// state.teams don't see a stale empty array, they see undefined and can
// splash "loading".
func TestState_NoTeamsCache_LeavesStateAlone(t *testing.T) {
	rt := loadHookRuntime(t, `
		Game = {
			gameName: "tf",
			teamRange: { min: 1, max: 2 },
			state: { score: 1 },
			contract: 2,
			init: function(ctx) { return Game.state; }
		};
	`)
	// No SetTeamsCache call.
	got, _ := rt.State().(map[string]any)
	if _, ok := got["teams"]; ok {
		t.Errorf("teams should not appear when cache is nil: %v", got)
	}
}

// Overlay must not mutate the live JS Game.state. If the game's update
// reads state.teams during a server tick, it should see whatever the JS
// holds there (undefined for v1 games), not the injected cache — because
// v1 games are expected to use the teams() global, and we don't want to
// clash with a key the game might author.
func TestState_OverlayDoesNotTouchLiveJSState(t *testing.T) {
	rt := loadHookRuntime(t, `
		Game = {
			gameName: "tf",
			teamRange: { min: 1, max: 4 },
			state: { score: 1, marker: "untouched" },
			contract: 2,
			init: function(ctx) { return Game.state; }
		};
	`)
	rt.SetTeamsCache([]map[string]any{{"name": "Red", "color": "#F00", "players": []any{}}})

	// First export: includes teams.
	_ = rt.State()

	// Reach into the VM and see if Game.state.teams leaked in.
	rt.mu.Lock()
	v, err := rt.vm.RunString(`typeof Game.state.teams`)
	rt.mu.Unlock()
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if v.String() != "undefined" {
		t.Errorf("live JS Game.state.teams should stay undefined, got %q", v.String())
	}
}
