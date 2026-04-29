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
			contract: 2,
			init: function(ctx) { return {}; }
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
			contract: 2,
			init: function(ctx) { return {}; }
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
		var Game = { contract: 2, init: function(ctx) { return {}; } };
	`), 0o644)

	chatCh := make(chan domain.Message, 8)
	_, err := LoadGame(mainJS, func(string) {}, chatCh, domain.RealClock{}, dir)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}
