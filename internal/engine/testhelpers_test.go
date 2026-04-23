package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev-null/internal/domain"
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
