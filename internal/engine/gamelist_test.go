package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListGamesIncludesFolderGames(t *testing.T) {
	dir := t.TempDir()

	// Create a flat game
	os.WriteFile(filepath.Join(dir, "flat.js"), []byte("var Game = {};"), 0o644)

	// Create a folder-based game
	os.MkdirAll(filepath.Join(dir, "folder"), 0o755)
	os.WriteFile(filepath.Join(dir, "folder", "main.js"), []byte("var Game = {};"), 0o644)

	// Create a folder without main.js (should NOT appear)
	os.MkdirAll(filepath.Join(dir, "noentry"), 0o755)
	os.WriteFile(filepath.Join(dir, "noentry", "helper.js"), []byte("// not a game"), 0o644)

	// Create .cache dir (should be excluded)
	os.MkdirAll(filepath.Join(dir, ".cache"), 0o755)
	os.WriteFile(filepath.Join(dir, ".cache", "cached.js"), []byte("var Game = {};"), 0o644)

	games := ListGames(dir)
	if len(games) != 2 {
		t.Fatalf("expected 2 games, got %d: %v", len(games), games)
	}
	if games[0] != "flat" {
		t.Errorf("expected games[0]=%q, got %q", "flat", games[0])
	}
	if games[1] != "folder" {
		t.Errorf("expected games[1]=%q, got %q", "folder", games[1])
	}
}

func TestResolveGamePath(t *testing.T) {
	dir := t.TempDir()

	// Create both types
	os.WriteFile(filepath.Join(dir, "flat.js"), []byte(""), 0o644)
	os.MkdirAll(filepath.Join(dir, "folder"), 0o755)
	os.WriteFile(filepath.Join(dir, "folder", "main.js"), []byte(""), 0o644)

	// Flat game
	got := ResolveGamePath(dir, "flat")
	want := filepath.Join(dir, "flat.js")
	if got != want {
		t.Errorf("flat: got %q, want %q", got, want)
	}

	// Folder game
	got = ResolveGamePath(dir, "folder")
	want = filepath.Join(dir, "folder", "main.js")
	if got != want {
		t.Errorf("folder: got %q, want %q", got, want)
	}
}
