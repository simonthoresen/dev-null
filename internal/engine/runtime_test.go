package engine

import (
	"os"
	"path/filepath"
	"testing"

	"dev-null/internal/domain"
)

func TestIncludeSingleFile(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "helper.js"), []byte(`
		function greet(name) { return "Hello, " + name; }
	`), 0o644)

	mainJS := filepath.Join(dir, "main.js")
	os.WriteFile(mainJS, []byte(`
		include("helper.js");
		var Game = {
			gameName: greet("World"),
			load: function(s) {}
		};
	`), 0o644)

	chatCh := make(chan domain.Message, 8)
	game, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, dir)
	if err != nil {
		t.Fatalf("LoadGame: %v", err)
	}
	if game.GameName() != "Hello, World" {
		t.Errorf("got gameName=%q, want %q", game.GameName(), "Hello, World")
	}
}

func TestIncludeIdempotent(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "counter.js"), []byte(`
		var counter = (typeof counter !== 'undefined') ? counter + 1 : 1;
	`), 0o644)

	mainJS := filepath.Join(dir, "main.js")
	os.WriteFile(mainJS, []byte(`
		include("counter.js");
		include("counter.js");
		var Game = {
			gameName: "count-" + counter,
			load: function(s) {}
		};
	`), 0o644)

	chatCh := make(chan domain.Message, 8)
	game, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, dir)
	if err != nil {
		t.Fatalf("LoadGame: %v", err)
	}
	if game.GameName() != "count-1" {
		t.Errorf("got gameName=%q, want %q", game.GameName(), "count-1")
	}
}

func TestIncludeRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()

	mainJS := filepath.Join(dir, "main.js")
	os.WriteFile(mainJS, []byte(`
		include("../etc/passwd");
		var Game = { load: function(s) {} };
	`), 0o644)

	chatCh := make(chan domain.Message, 8)
	_, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, dir)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestNethackGameLoads(t *testing.T) {
	mainJS := filepath.Join("../../dist/games/nethack", "main.js")
	if _, err := os.Stat(mainJS); err != nil {
		t.Skip("nethack game not found at", mainJS)
	}

	chatCh := make(chan domain.Message, 64)
	game, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, "../../dist")
	if err != nil {
		t.Fatalf("LoadGame nethack: %v", err)
	}
	if game.GameName() != "NetHack" {
		t.Errorf("got gameName=%q, want %q", game.GameName(), "NetHack")
	}
}

func TestHoldemGameLoads(t *testing.T) {
	mainJS := filepath.Join("../../dist/games/holdem", "main.js")
	if _, err := os.Stat(mainJS); err != nil {
		t.Skip("holdem game not found at", mainJS)
	}

	chatCh := make(chan domain.Message, 64)
	game, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, "../../dist")
	if err != nil {
		t.Fatalf("LoadGame holdem: %v", err)
	}
	if game.GameName() != "Texas Hold'em" {
		t.Errorf("got gameName=%q, want %q", game.GameName(), "Texas Hold'em")
	}
}

// TestVoyageRendersCanvas loads voyage.js and renders one canvas frame.
// This is a smoke test for the 3D rasterizer bindings: if the JS side
// hits an undefined method (e.g. fillTriangle3DLit) the call panics and
// is converted to an error, which would surface here. It also catches
// regressions in voyage's sphere-mesh generator and lighting math.
func TestVoyageRendersCanvas(t *testing.T) {
	mainJS := filepath.Join("../../dist/games", "voyage.js")
	if _, err := os.Stat(mainJS); err != nil {
		t.Skip("voyage game not found at", mainJS)
	}
	chatCh := make(chan domain.Message, 64)
	game, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, "../../dist")
	if err != nil {
		t.Fatalf("LoadGame voyage: %v", err)
	}
	game.Load(nil)
	game.Begin()
	game.Update(0.016)
	img := game.RenderCanvasImage("alice", 200, 200)
	if img == nil {
		t.Fatal("voyage RenderCanvasImage returned nil")
	}
	// Expect at least some non-background pixels (the scene has stars+planets).
	touched := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i-3] > 8 || img.Pix[i-2] > 8 || img.Pix[i-1] > 8 {
			touched++
		}
	}
	if touched == 0 {
		t.Error("voyage rendered an empty canvas")
	}
}
