package server

import (
	"testing"
	"time"

	"dev-null/internal/domain"
)

// setupPlayingGame loads the minimal test game and advances it to PhasePlaying.
func setupPlayingGame(t *testing.T) *Server {
	t.Helper()
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
		t.Fatalf("expected PhasePlaying, got %d", s.state.GetGamePhase())
	}
	return s
}

// ─── checkGameOver ────────────────────────────────────────────────────────────

func TestCheckGameOver_TransitionsToEnding(t *testing.T) {
	s := setupPlayingGame(t)

	// Trigger game over in JS via onInput("space").
	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.OnInput("p1", "space")

	// NOTE: do NOT call rt.IsGameOverPending() here — it is a consume-once
	// method that resets the flag, which would cause checkGameOver to miss it.
	s.checkGameOver()

	if s.state.GetGamePhase() != domain.PhaseEnding {
		t.Fatalf("expected PhaseEnding, got %d", s.state.GetGamePhase())
	}

	s.state.RLock()
	results := s.state.GameOverResults
	s.state.RUnlock()
	if len(results) != 1 || results[0].Name != "player" {
		t.Fatalf("unexpected GameOverResults: %v", results)
	}
}

func TestCheckGameOver_NoOp_WhenNotPlaying(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()
	// No game loaded; checkGameOver should be a no-op.
	s.checkGameOver()
	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Errorf("expected PhaseNone, got %d", s.state.GetGamePhase())
	}
}

func TestCheckGameOver_NoOp_WhenGameOverNotPending(t *testing.T) {
	s := setupPlayingGame(t)
	// Don't trigger game over; checkGameOver must stay in PhasePlaying.
	s.checkGameOver()
	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Errorf("expected PhasePlaying, got %d", s.state.GetGamePhase())
	}
}

func TestCheckGameOver_SetsGameOverTimer(t *testing.T) {
	s := setupPlayingGame(t)

	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.OnInput("p1", "space") // triggers gameOver() in JS
	s.checkGameOver()

	if s.gameOverTimer == nil {
		t.Fatal("expected gameOverTimer to be initialised after checkGameOver")
	}
}

// ─── AcknowledgeGameOver ─────────────────────────────────────────────────────

func TestAcknowledgeGameOver_ClosesTimerWhenAllReady(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)
	gamePath := writeTestGame(t, s.dataDir)
	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame: %v", err)
	}
	// Set PhaseEnding and timer directly (avoids background goroutine cleanup race).
	s.state.SetGamePhase(domain.PhaseEnding)
	s.gameOverTimer = make(chan struct{})

	// Only one player (p1) — one acknowledgement should close the timer.
	s.AcknowledgeGameOver("p1")

	select {
	case <-s.gameOverTimer:
		// OK — timer was closed.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected gameOverTimer to be closed after all players acknowledged")
	}
}

func TestAcknowledgeGameOver_NoOp_WhenTimerAlreadyClosed(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.MovePlayerToTeam("p1", 0)
	gamePath := writeTestGame(t, s.dataDir)
	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame: %v", err)
	}
	s.state.SetGamePhase(domain.PhaseEnding)
	s.gameOverTimer = make(chan struct{})

	// Acknowledge twice — second call must not panic even though timer is already closed.
	s.AcknowledgeGameOver("p1")
	s.AcknowledgeGameOver("p1")
}

func TestAcknowledgeGameOver_WaitsForAllPlayers(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()
	// Two players in the same team.
	s.state.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.state.AddPlayer(&domain.Player{ID: "p2", Name: "bob"})
	s.state.MovePlayerToTeam("p1", 0)
	s.state.MovePlayerToTeam("p2", 0)

	// Load the game so that GameTeams is populated (AllPlayersReady checks it).
	gamePath := writeTestGame(t, s.dataDir)
	if err := s.loadGame(gamePath); err != nil {
		t.Fatalf("loadGame: %v", err)
	}

	// Set PhaseEnding and a timer channel directly — this avoids launching the
	// 15-second background goroutine that checkGameOver starts, which would
	// call unloadGame after the test exits (causing temp-dir cleanup races on Windows).
	s.state.SetGamePhase(domain.PhaseEnding)
	s.gameOverTimer = make(chan struct{})

	// First player acknowledges — timer must remain open.
	s.AcknowledgeGameOver("p1")
	select {
	case <-s.gameOverTimer:
		t.Fatal("timer closed prematurely after only one of two players acknowledged")
	default:
		// Correct: still open.
	}

	// Second player acknowledges — timer must close now.
	s.AcknowledgeGameOver("p2")
	select {
	case <-s.gameOverTimer:
		// OK.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected timer to close after all players acknowledged")
	}
}

// ─── gameOverTimeout ─────────────────────────────────────────────────────────

func TestGameOverTimeout_UnloadsWhenPhaseEnding(t *testing.T) {
	s := setupPlayingGame(t)

	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.OnInput("p1", "space")
	s.checkGameOver()

	// Override the timer with a pre-closed channel so gameOverTimeout fires immediately.
	done := make(chan struct{})
	close(done)
	s.gameOverTimer = done

	// Run synchronously (the timer fires immediately because the channel is closed).
	s.gameOverTimeout()

	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Errorf("expected PhaseNone after timeout unload, got %d", s.state.GetGamePhase())
	}
}

func TestGameOverTimeout_DoesNotUnload_WhenPhaseNotEnding(t *testing.T) {
	s := newTestServer(t)
	// Phase is PhaseNone (no game loaded).
	done := make(chan struct{})
	close(done)
	s.gameOverTimer = done

	// Should be a no-op since phase != PhaseEnding.
	s.gameOverTimeout()

	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Errorf("unexpected phase change: %d", s.state.GetGamePhase())
	}
}
