package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev-null/internal/domain"
)

// TestVoyageEntitiesAndPhaseTransitions covers what the older
// TestVoyageRendersCanvas (in runtime_test.go) doesn't: voyage v2's entity
// model. It seeds two real teams, asserts that bots fill the slate up to
// MIN_TEAMS=5, that everyone starts in orbit, and that running the
// simulation long enough produces at least one orbit→travel transition.
// Spectator-mode rendering is also exercised because resolveMe returns
// entityIdx=-1 for non-team players.
func TestVoyageEntitiesAndPhaseTransitions(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, "dist", "games", "voyage.js"))
	if err != nil {
		t.Fatalf("read voyage.js: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "voyage.js")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	chatCh := make(chan domain.Message, 32)
	clock := &domain.MockClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	g, err := LoadGame(path, func(string) {}, chatCh, clock, dir)
	if err != nil {
		t.Fatalf("LoadGame: %v", err)
	}
	rt := g.(*Runtime)

	// Two real teams; voyage's MIN_TEAMS=5 means three bots get added.
	rt.SetTeamsCache([]map[string]any{
		{"name": "Acme",  "color": "#ff8800", "players": []any{map[string]any{"id": "p1", "name": "alice"}}},
		{"name": "Stark", "color": "#0088ff", "players": []any{map[string]any{"id": "p2", "name": "bob"}}},
	})

	rt.Load(nil)
	rt.Begin()
	state := gameState(rt)
	ents, ok := state["entities"].([]any)
	if !ok {
		t.Fatalf("entities missing or wrong shape: %v", state)
	}
	if len(ents) < 5 {
		t.Fatalf("voyage should ensure ≥5 entities (2 teams + ≥3 bots), got %d", len(ents))
	}

	// Each entity must have a mode descriptor (orbit on first tick).
	for i, e := range ents {
		em := e.(map[string]any)
		mode, ok := em["mode"].(map[string]any)
		if !ok {
			t.Fatalf("entity %d missing mode: %v", i, em)
		}
		if mode["kind"] != "orbit" {
			t.Errorf("entity %d should start in orbit, got %v", i, mode["kind"])
		}
	}

	// Step the engine forward enough to provoke at least one orbit→travel
	// transition (orbit duration is ~14s at the configured speed).
	for tick := 0; tick < 200; tick++ {
		rt.Update(0.1)
	}
	state = gameState(rt)
	ents = state["entities"].([]any)
	sawTravel := false
	for _, e := range ents {
		mode := e.(map[string]any)["mode"].(map[string]any)
		if mode["kind"] == "travel" {
			sawTravel = true
			break
		}
	}
	if !sawTravel {
		t.Error("after 20s of simulation, no entity ever entered travel mode")
	}

	// renderCanvas must complete without throwing for both a team player
	// and a spectator.
	if data := rt.RenderCanvas("p1", 80, 60); data == nil {
		t.Error("RenderCanvas returned nil for team player")
	}
	if data := rt.RenderCanvas("ghost", 80, 60); data == nil {
		t.Error("RenderCanvas returned nil for spectator")
	}

	// statusBar should give a sensible string in both modes.
	if got := rt.StatusBar("p1"); got == "" {
		t.Error("statusBar empty for team player")
	}
	if got := rt.StatusBar("ghost"); got == "" {
		t.Error("statusBar empty for spectator")
	}
}
