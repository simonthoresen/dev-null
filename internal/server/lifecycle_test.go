package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/ssh"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/state"
)

// minimalGameJS is a tiny game script that exercises the full lifecycle,
// including the suspend/resume hooks added to separate session from persistent state.
const minimalGameJS = `
var Game = {
    gameName: "test-game",
    contract: 2,

    init: function(ctx) { return { score: 0, highScore: 0 }; },

    begin: function(state, ctx) { state.score = 0; },

    update: function(state, dt, events, ctx) {
        for (var i = 0; i < events.length; i++) {
            var e = events[i];
            if (e.type === "input" && e.key === "space") {
                state.score++;
                ctx.gameOver([{name: "player", result: state.score + " pts"}]);
            }
        }
    },

    // unload: returns PERSISTENT state (high scores etc.) — called on game-over,
    // /game unload, AND after suspend() during /game suspend.
    unload: function() {
        if (Game.state.score > Game.state.highScore)
            Game.state.highScore = Game.state.score;
        return { highScore: Game.state.highScore };
    },

    // suspend: returns SESSION state (mid-game snapshot) — stored in the save file.
    suspend: function() {
        return { score: Game.state.score };
    },

    // resume: restores session state; called instead of begin() on /game resume.
    resume: function(saved) {
        if (saved && saved.score !== undefined) Game.state.score = saved.score;
    },

    renderAscii: function(state, me, cells) {
        cells.writeString(0, 0, "Score: " + state.score, "#fff", null);
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
		programs:     make(map[string]msgSender),
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
	if s.state.GetGamePhase() != domain.PhaseStarting {
		t.Fatalf("expected PhaseStarting, got %d", s.state.GetGamePhase())
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
	if s.state.GetGamePhase() != domain.PhaseStarting {
		t.Fatalf("expected PhaseStarting")
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

	// Trigger game over via input — v2 queues events; the next Update()
	// drains them and runs the handler that calls ctx.gameOver.
	game.OnInput("p1", "space")
	game.Update(0.1)

	// Check that game over was signaled.
	rt := game.(*engine.Runtime)
	if !rt.IsGameOverPending() {
		t.Fatal("expected game over to be pending")
	}

	results := rt.GameOverResults()
	if len(results) != 1 || results[0].Name != "player" {
		t.Fatalf("unexpected results: %v", results)
	}

	// Unload — unloadGame saves state returned by game.Unload() to disk.
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

	// Earn some score before suspending.
	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.OnInput("p1", "space") // queued
	game.OnInput("p1", "space") // queued
	game.Update(0.1)            // drain events → score advances to 2

	// Suspend — suspend() captures session state (score=2),
	// unload() captures persistent state (highScore=0, since gameOver not called).
	if err := s.suspendGame("save1"); err != nil {
		t.Fatalf("suspendGame: %v", err)
	}
	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Fatalf("expected PhaseNone after suspend")
	}

	// Verify suspend save file exists with session state.
	saves := s.ListSuspends()
	if len(saves) != 1 {
		t.Fatalf("expected 1 save, got %d", len(saves))
	}
	if saves[0].SaveName != "save1" {
		t.Fatalf("expected 'save1', got %q", saves[0].SaveName)
	}
	suspendSave, err := state.LoadSuspend(s.dataDir, "test-game", "save1")
	if err != nil {
		t.Fatalf("LoadSuspend: %v", err)
	}
	sessionMap, ok := suspendSave.GameState.(map[string]any)
	if !ok {
		t.Fatalf("expected session state map, got %T", suspendSave.GameState)
	}
	if fmt.Sprintf("%v", sessionMap["score"]) != "2" {
		t.Fatalf("expected session score=2, got %v", sessionMap["score"])
	}

	// Resume — load() gets persistent state, resume() gets session state (score=2).
	if err := s.resumeGame("test-game", "save1"); err != nil {
		t.Fatalf("resumeGame: %v", err)
	}
	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Fatalf("expected PhasePlaying after resume")
	}

	// Verify score was restored from session state.
	s.state.RLock()
	game = s.state.ActiveGame
	s.state.RUnlock()
	rt := game.(*engine.Runtime)
	stateVal := rt.State()
	stateMap, ok := stateVal.(map[string]any)
	if !ok {
		t.Fatalf("expected game state map, got %T", stateVal)
	}
	if fmt.Sprintf("%v", stateMap["score"]) != "2" {
		t.Fatalf("expected resumed score=2, got %v", stateMap["score"])
	}
}

// teamRequiredGameJS requires exactly 2 teams.
const teamRequiredGameJS = `
var Game = {
    gameName: "team-required",
    contract: 2,
    teamRange: { min: 2, max: 4 },
    init: function(ctx) { return {}; },
    renderAscii: function(state, me, cells) {}
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
