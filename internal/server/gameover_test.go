package server

import (
	"strings"
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

// checkGameOver posts the ranked results to chat as a system message and
// unloads the game, returning everyone to the lobby (PhaseNone). There
// is no ending phase or acknowledgement step.
func TestCheckGameOverPostsChatAndUnloads(t *testing.T) {
	s := setupPlayingGame(t)

	s.state.RLock()
	game := s.state.ActiveGame
	s.state.RUnlock()
	game.OnInput("p1", "space") // queues an event; Update drains and runs it
	game.Update(0.01)
	s.checkGameOver()

	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Fatalf("expected PhaseNone after game-over, got %d", s.state.GetGamePhase())
	}
	if s.state.ActiveGame != nil {
		t.Fatal("expected ActiveGame to be unloaded")
	}

	// The chat broadcast happens synchronously from checkGameOver →
	// broadcastChat, which both posts to chatCh and adds to state history.
	// State history is the authoritative record (chatCh is a non-blocking
	// notification channel that may drop under load), so assert on that.
	history := s.state.GetChatHistory()
	var found bool
	for _, m := range history {
		if strings.Contains(m.Text, "Game Over") && strings.Contains(m.Text, "player") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected game-over message in chat history, got %d messages", len(history))
	}
}

func TestCheckGameOverNoOpWhenNotPlaying(t *testing.T) {
	s := newTestServer(t)
	s.registerBuiltins()
	s.checkGameOver()
	if s.state.GetGamePhase() != domain.PhaseNone {
		t.Errorf("expected PhaseNone, got %d", s.state.GetGamePhase())
	}
}

func TestCheckGameOverNoOpWhenNotPending(t *testing.T) {
	s := setupPlayingGame(t)
	s.checkGameOver()
	if s.state.GetGamePhase() != domain.PhasePlaying {
		t.Errorf("expected PhasePlaying, got %d", s.state.GetGamePhase())
	}
}

func TestFormatGameOverChat(t *testing.T) {
	text := formatGameOverChat("voyage", []domain.GameResult{
		{Name: "alice", Result: "12 pts"},
		{Name: "bob", Result: "3 pts"},
	})
	lines := strings.Split(text, "\n")
	if lines[0] != "Game Over: voyage" {
		t.Errorf("header = %q, want %q", lines[0], "Game Over: voyage")
	}
	if !strings.Contains(lines[1], "1.") || !strings.Contains(lines[1], "alice") {
		t.Errorf("first result line = %q", lines[1])
	}
	if !strings.Contains(lines[2], "2.") || !strings.Contains(lines[2], "bob") {
		t.Errorf("second result line = %q", lines[2])
	}
}

func TestFormatGameOverChatNoResults(t *testing.T) {
	text := formatGameOverChat("", nil)
	if text != "Game Over" {
		t.Errorf("got %q, want %q", text, "Game Over")
	}
}
