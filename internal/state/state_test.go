package state

import (
	"testing"

	"dev-null/internal/domain"
)

func TestAddRemovePlayer(t *testing.T) {
	s := New("")
	p := &domain.Player{ID: "p1", Name: "alice"}
	s.AddPlayer(p)

	if got := s.GetPlayer("p1"); got == nil {
		t.Fatal("expected player to be found")
	}
	if got := s.PlayerCount(); got != 1 {
		t.Fatalf("expected 1 player, got %d", got)
	}

	s.RemovePlayer("p1")
	if got := s.GetPlayer("p1"); got != nil {
		t.Fatal("expected player to be nil after removal")
	}
}

func TestPlayerByName(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "Alice"})

	// Case-insensitive lookup.
	if p := s.PlayerByName("alice"); p == nil {
		t.Fatal("expected case-insensitive match")
	}
	if p := s.PlayerByName("ALICE"); p == nil {
		t.Fatal("expected case-insensitive match (upper)")
	}
	if p := s.PlayerByName("bob"); p != nil {
		t.Fatal("expected nil for unknown name")
	}
}

func TestChatHistory(t *testing.T) {
	s := New("")
	for i := range MaxChatHistory + 10 {
		s.AddChat(domain.Message{Text: string(rune('A' + i%26))})
	}
	history := s.GetChatHistory()
	if len(history) != MaxChatHistory {
		t.Fatalf("expected %d messages, got %d", MaxChatHistory, len(history))
	}
}

func TestSetGamePhase(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})

	// Transition to starting creates the ready map.
	s.SetGamePhase(domain.PhaseStarting)
	if s.GetGamePhase() != domain.PhaseStarting {
		t.Fatal("expected PhaseStarting")
	}
	s.RLock()
	if s.StartingReady == nil {
		t.Fatal("expected StartingReady to be initialized")
	}
	s.RUnlock()

	// Transition to none clears everything.
	s.SetGamePhase(domain.PhaseNone)
	s.RLock()
	if s.StartingReady != nil {
		t.Fatal("expected StartingReady to be nil")
	}
	if s.GameTeams != nil {
		t.Fatal("expected GameTeams to be nil")
	}
	s.RUnlock()
}

func TestSetPlayerAdmin(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})

	s.SetPlayerAdmin("p1", true)
	if p := s.GetPlayer("p1"); !p.IsAdmin {
		t.Fatal("expected admin flag to be set")
	}

	s.SetPlayerAdmin("p1", false)
	if p := s.GetPlayer("p1"); p.IsAdmin {
		t.Fatal("expected admin flag to be cleared")
	}
}

func TestTeamManagement(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.AddPlayer(&domain.Player{ID: "p2", Name: "bob"})

	// Create a team for alice (index = 0, which is past len(Teams)=0).
	s.MovePlayerToTeam("p1", 0)
	teams := s.GetTeams()
	if len(teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(teams))
	}
	if teams[0].Name != "Team alice" {
		t.Fatalf("expected 'Team alice', got %q", teams[0].Name)
	}
	if len(teams[0].Players) != 1 || teams[0].Players[0] != "p1" {
		t.Fatalf("unexpected players: %v", teams[0].Players)
	}

	// Bob joins alice's team.
	s.MovePlayerToTeam("p2", 0)
	teams = s.GetTeams()
	if len(teams[0].Players) != 2 {
		t.Fatalf("expected 2 players, got %d", len(teams[0].Players))
	}

	// Bob creates a new team.
	s.MovePlayerToTeam("p2", 1)
	teams = s.GetTeams()
	if len(teams) != 2 {
		t.Fatalf("expected 2 teams, got %d", len(teams))
	}

	// Unassign alice (-1).
	s.MovePlayerToTeam("p1", -1)
	teams = s.GetTeams()
	// alice's team should be removed (was empty).
	if len(teams) != 1 {
		t.Fatalf("expected 1 team after unassign, got %d", len(teams))
	}

	unassigned := s.UnassignedPlayers()
	if len(unassigned) != 1 || unassigned[0] != "p1" {
		t.Fatalf("expected alice unassigned, got %v", unassigned)
	}
}

func TestRenameTeam(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.AddPlayer(&domain.Player{ID: "p2", Name: "bob"})
	s.MovePlayerToTeam("p1", 0)
	s.MovePlayerToTeam("p2", 1)

	if !s.RenameTeam(0, "Awesome") {
		t.Fatal("expected rename to succeed")
	}
	teams := s.GetTeams()
	if teams[0].Name != "Awesome" {
		t.Fatalf("expected 'Awesome', got %q", teams[0].Name)
	}

	// Duplicate name should fail.
	if s.RenameTeam(1, "awesome") {
		t.Fatal("expected rename to fail for duplicate name")
	}
}

func TestNextTeamColor(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.MovePlayerToTeam("p1", 0)

	original := s.GetTeams()[0].Color
	s.NextTeamColor(0, 1)
	newColor := s.GetTeams()[0].Color
	if newColor == original {
		t.Fatal("expected color to change")
	}
}

func TestPlayerTeamIndex(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.AddPlayer(&domain.Player{ID: "p2", Name: "bob"})

	// Unassigned → -1.
	if idx := s.PlayerTeamIndex("p1"); idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}

	s.MovePlayerToTeam("p1", 0)
	s.MovePlayerToTeam("p2", 1)
	if idx := s.PlayerTeamIndex("p1"); idx != 0 {
		t.Fatalf("expected 0, got %d", idx)
	}
	if idx := s.PlayerTeamIndex("p2"); idx != 1 {
		t.Fatalf("expected 1, got %d", idx)
	}
}

func TestGameTeamOperations(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})

	// Set up game teams.
	s.Lock()
	s.GameTeams = []domain.Team{
		{Name: "Red", Color: "#ff0000", Players: []string{"p1"}},
	}
	s.Unlock()

	if !s.IsGamePlayer("p1") {
		t.Fatal("expected p1 to be a game player")
	}
	if s.IsGamePlayer("p2") {
		t.Fatal("expected p2 to not be a game player")
	}

	// Test ReplaceGamePlayerID.
	s.Lock()
	s.ReplaceGamePlayerID("p1", "p1-new")
	s.Unlock()

	gameTeams := s.GetGameTeams()
	if gameTeams[0].Players[0] != "p1-new" {
		t.Fatalf("expected p1-new, got %q", gameTeams[0].Players[0])
	}
}

func TestIsSoleMemberOfTeam(t *testing.T) {
	s := New("")
	s.AddPlayer(&domain.Player{ID: "p1", Name: "alice"})
	s.AddPlayer(&domain.Player{ID: "p2", Name: "bob"})
	s.MovePlayerToTeam("p1", 0)

	if !s.IsSoleMemberOfTeam("p1") {
		t.Fatal("expected alice to be sole member")
	}

	s.MovePlayerToTeam("p2", 0)
	if s.IsSoleMemberOfTeam("p1") {
		t.Fatal("expected alice to not be sole member")
	}
}
