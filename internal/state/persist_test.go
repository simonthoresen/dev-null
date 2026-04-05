package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev-null/internal/domain"
)

func TestSaveAndLoadGameState(t *testing.T) {
	dir := t.TempDir()
	state := map[string]any{"highScore": 42.0}

	if err := SaveGameState(dir, "testgame", state); err != nil {
		t.Fatalf("SaveGameState: %v", err)
	}

	loaded, err := LoadGameState(dir, "testgame")
	if err != nil {
		t.Fatalf("LoadGameState: %v", err)
	}
	m, ok := loaded.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", loaded)
	}
	if m["highScore"] != 42.0 {
		t.Fatalf("expected 42, got %v", m["highScore"])
	}
}

func TestLoadGameStateNonexistent(t *testing.T) {
	dir := t.TempDir()
	loaded, err := LoadGameState(dir, "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil state, got %v", loaded)
	}
}

func TestSaveGameStateNilIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := SaveGameState(dir, "testgame", nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// No file should have been created.
	path := filepath.Join(dir, "state", "testgame.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected no file for nil state")
	}
}

func TestSuspendSaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	save := &SuspendSave{
		GameName:  "mygame",
		SaveName:  "save1",
		SavedAt:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Teams:     []domain.Team{{Name: "Red", Color: "#ff0000", Players: []string{"p1"}}},
		GameState: map[string]any{"level": 3.0},
	}

	if err := SaveSuspend(dir, save); err != nil {
		t.Fatalf("SaveSuspend: %v", err)
	}

	loaded, err := LoadSuspend(dir, "mygame", "save1")
	if err != nil {
		t.Fatalf("LoadSuspend: %v", err)
	}
	if loaded.GameName != "mygame" {
		t.Fatalf("expected 'mygame', got %q", loaded.GameName)
	}
	if len(loaded.Teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(loaded.Teams))
	}
	if loaded.Teams[0].Name != "Red" {
		t.Fatalf("expected 'Red', got %q", loaded.Teams[0].Name)
	}

	// Delete.
	if err := DeleteSuspend(dir, "mygame", "save1"); err != nil {
		t.Fatalf("DeleteSuspend: %v", err)
	}
	_, err = LoadSuspend(dir, "mygame", "save1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListSuspends(t *testing.T) {
	dir := t.TempDir()

	// Save two suspends for different games.
	save1 := &SuspendSave{
		GameName: "game1",
		SaveName: "early",
		SavedAt:  time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Teams:    []domain.Team{{Name: "A", Players: []string{"p1"}}},
	}
	save2 := &SuspendSave{
		GameName: "game2",
		SaveName: "later",
		SavedAt:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Teams:    []domain.Team{{Name: "B", Players: []string{"p2"}}, {Name: "C", Players: []string{"p3"}}},
	}
	SaveSuspend(dir, save1)
	SaveSuspend(dir, save2)

	// List all.
	all := ListSuspends(dir, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 saves, got %d", len(all))
	}
	// Should be sorted by time descending (most recent first).
	if all[0].SaveName != "later" {
		t.Fatalf("expected 'later' first, got %q", all[0].SaveName)
	}
	if all[1].SaveName != "early" {
		t.Fatalf("expected 'early' second, got %q", all[1].SaveName)
	}
	if all[1].TeamCount != 1 {
		t.Fatalf("expected 1 team for 'early', got %d", all[1].TeamCount)
	}

	// List filtered by game name.
	filtered := ListSuspends(dir, "game1")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 save for game1, got %d", len(filtered))
	}
	if filtered[0].GameName != "game1" {
		t.Fatalf("expected 'game1', got %q", filtered[0].GameName)
	}
}

func TestListSuspendNames(t *testing.T) {
	dir := t.TempDir()
	save := &SuspendSave{
		GameName: "mygame",
		SaveName: "save1",
		SavedAt:  time.Now(),
		Teams:    []domain.Team{{Name: "A", Players: []string{"p1"}}},
	}
	SaveSuspend(dir, save)

	names := ListSuspendNames(dir)
	if len(names) != 1 || names[0] != "mygame/save1" {
		t.Fatalf("expected ['mygame/save1'], got %v", names)
	}
}

func TestListSuspendsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	all := ListSuspends(dir, "")
	if len(all) != 0 {
		t.Fatalf("expected 0 saves, got %d", len(all))
	}
}
