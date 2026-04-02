package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/ssh"

	"null-space/internal/domain"
	"null-space/internal/engine"
	"null-space/internal/state"
)

// minimalGameJS is a tiny game script that exercises the full lifecycle.
const minimalGameJS = `
var Game = {
    gameName: "test-game",
    state: { score: 0 },
    init: function(saved) {
        if (saved && saved.score) Game.state.score = saved.score;
    },
    start: function() {},
    update: function(dt) {},
    onInput: function(pid, key) {
        if (key === "space") {
            Game.state.score++;
            gameOver([{name: "player", result: Game.state.score + " pts"}], {score: Game.state.score});
        }
    },
    render: function(buf, pid, ox, oy, w, h) {
        buf.writeString(ox, oy, "Score: " + Game.state.score, "#fff", null);
    }
};
`

func writeTestGame(t *testing.T, dir string) string {
	t.Helper()
	gamesDir := filepath.Join(dir, "games")
	if err := os.MkdirAll(gamesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gamePath := filepath.Join(gamesDir, "test-game.js")
	if err := os.WriteFile(gamePath, []byte(minimalGameJS), 0o644); err != nil {
		t.Fatal(err)
	}
	return gamePath
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return &Server{
		state:        state.New(""),
		registry:     newCommandRegistry(),
		dataDir:      dir,
		clock:        &domain.MockClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		programs:     make(map[string]*tea.Program),
		sessions:     make(map[string]ssh.Session),
		tickInterval: 100 * time.Millisecond,
	}
}


func TestLoadAndUnloadGame(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()

	// Set up a player and team so the game can load.
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)

	gamePath := writeTestGame(t, s.dataDir)

	// Load game.
	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame failed: %v", err)
	}

	// Verify state transitions.
	if s.state.GetGamePhase() != domain.PhaseSplash {
		t.Fatalf("expected PhaseSplash, got %d", s.state.GetGamePhase())
	}
	s.state.RLock()
	name := s.state.GameName
	game := s.state.ActiveGame
	s.state.RUnlock()
	if name != "test-game" {
		t.Fatalf("expected 'test-game', got %q", name)
	}
	if game == nil {
		t.Fatal("expected active game")
	}

	// Unload game.
	s.unloadGame()
	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Fatalf("expected PhaseNone, got %d", s.state.GetGamePhase())
	}
	s.state.RLock()
	if s.state.ActiveGame != nil {
		t.Fatal("expected nil active game")
	}
	s.state.RUnlock()
}

func TestGameLifecycle(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()

	// Set up a player and team.
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)

	gamePath := writeTestGame(t, s.dataDir)

	// Load → Splash.
	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame: %v", err)
	}
	if s.state.GetGamePhase() != domain.PhaseSplash {
		t.Fatalf("expected PhaseSplash")
	}

	// Start game (admin acknowledges splash).
	s.StartGame()

	// Wait briefly for the splash goroutine to transition.
	time.Sleep(200 * time.Millisecond)

	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Fatalf("expected PhasePlaying, got %d", s.state.GetGamePhase())
	}

	// Simulate a game tick with Update().
	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.Update(0.1)

	// Trigger game over via input.
	game.OnInput("p1", "space")

	// Check that game over was signaled.
	rt := game.(*engine.JSRuntime)
	if !rt.IsGameOverPending() {
		t.Fatal("expected game over to be pending")
	}

	results := rt.GameOverResults()
	if len(results) != 1 || results[0].Name != "player" {
		t.Fatalf("unexpected results: %v", results)
	}

	// State should have been passed to gameOver.
	if rt.GameOverStateExport() == nil {
		t.Fatal("expected game-over state export")
	}

	// Unload.
	s.unloadGame()
	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Fatalf("expected PhaseNone after unload")
	}
}

func TestSuspendResume(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()

	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)

	gamePath := writeTestGame(t, s.dataDir)

	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame: %v", err)
	}
	s.StartGame()
	time.Sleep(200 * time.Millisecond)

	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Fatalf("expected PhasePlaying")
	}

	// Suspend.
	if err := s.suspendGame("save1"); err != nil {
		t.Fatalf("suspendGame: %v", err)
	}
	if s.state.GetGamePhase() != domain.PhaseSuspended {
		t.Fatalf("expected PhaseSuspended")
	}

	// Verify save file exists.
	saves := s.ListSuspends()
	if len(saves) != 1 {
		t.Fatalf("expected 1 save, got %d", len(saves))
	}
	if saves[0].SaveName != "save1" {
		t.Fatalf("expected 'save1', got %q", saves[0].SaveName)
	}

	// Warm resume (runtime still alive).
	if err := s.resumeGame("test-game", "save1"); err != nil {
		t.Fatalf("resumeGame (warm): %v", err)
	}
	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Fatalf("expected PhasePlaying after warm resume")
	}
}

// teamRequiredGameJS requires exactly 2 teams.
const teamRequiredGameJS = `
var Game = {
    gameName: "team-required",
    teamRange: { min: 2, max: 4 },
    state: {},
    init: function(saved) {},
    render: function(buf, pid, ox, oy, w, h) {}
};
`

func TestTeamValidationOnLoad(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()

	// Write a game that requires 2-4 teams.
	gamesDir := filepath.Join(s.dataDir, "games")
	if err := os.MkdirAll(gamesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gamePath := filepath.Join(gamesDir, "team-required.js")
	if err := os.WriteFile(gamePath, []byte(teamRequiredGameJS), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only 1 team — should fail (min is 2).
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)

	err := s.loadGame(gamePath)
	if err == nil {
		t.Fatal("expected error loading game with insufficient teams")
	}
}
