package state

import (
	"dev-null/internal/domain"
	"sort"
	"strings"
)

// TeamColors is the palette of available team colors.
var TeamColors = []string{
	"#ff5555", // red
	"#5555ff", // blue
	"#55ff55", // green
	"#ffff55", // yellow
	"#55ffff", // cyan
	"#ff55ff", // magenta
	"#ffaa00", // orange
	"#aa55ff", // purple
}

func (s *CentralState) GetTeams() []domain.Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	teams := make([]domain.Team, len(s.Teams))
	for i, t := range s.Teams {
		teams[i] = domain.Team{
			Name:    t.Name,
			Color:   t.Color,
			Players: append([]string(nil), t.Players...),
		}
	}
	return teams
}

// nextAvailableColor returns the first team color not already used.
func (s *CentralState) nextAvailableColor() string {
	used := make(map[string]bool)
	for _, t := range s.Teams {
		used[t.Color] = true
	}
	for _, c := range TeamColors {
		if !used[c] {
			return c
		}
	}
	// all taken — just recycle the first one
	return TeamColors[0]
}

// RemovePlayerFromTeams removes a player from all teams and cleans up empty teams.
func (s *CentralState) RemovePlayerFromTeams(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removePlayerFromTeamsLocked(playerID)
}

func (s *CentralState) removePlayerFromTeamsLocked(playerID string) {
	for i := range s.Teams {
		for j, id := range s.Teams[i].Players {
			if id == playerID {
				s.Teams[i].Players = append(s.Teams[i].Players[:j], s.Teams[i].Players[j+1:]...)
				break
			}
		}
	}
	// remove empty teams
	filtered := s.Teams[:0]
	for _, t := range s.Teams {
		if len(t.Players) > 0 {
			filtered = append(filtered, t)
		}
	}
	s.Teams = filtered
}

// MovePlayerToTeam moves a player to the team at the given index.
// If teamIndex == len(Teams), a new solo team is created at the end.
// If teamIndex < 0, the player becomes unassigned (removed from all teams).
func (s *CentralState) MovePlayerToTeam(playerID string, teamIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removePlayerFromTeamsLocked(playerID)
	if teamIndex < 0 {
		return // unassigned
	}
	if teamIndex >= len(s.Teams) {
		// create new solo team at the end
		p := s.Players[playerID]
		name := playerID
		if p != nil {
			name = "Team " + p.Name
		}
		s.Teams = append(s.Teams, domain.Team{
			Name:    name,
			Color:   s.nextAvailableColor(),
			Players: []string{playerID},
		})
		return
	}
	s.Teams[teamIndex].Players = append(s.Teams[teamIndex].Players, playerID)
}

// RenameTeam renames the team at the given index. Returns false if name is taken.
func (s *CentralState) RenameTeam(teamIndex int, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if teamIndex < 0 || teamIndex >= len(s.Teams) {
		return false
	}
	for i, t := range s.Teams {
		if i != teamIndex && strings.EqualFold(t.Name, name) {
			return false // name taken
		}
	}
	s.Teams[teamIndex].Name = name
	return true
}

// NextTeamColor cycles to the next available color for the team at the given index.
// direction: +1 = forward, -1 = backward.
func (s *CentralState) NextTeamColor(teamIndex, direction int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if teamIndex < 0 || teamIndex >= len(s.Teams) {
		return
	}
	used := make(map[string]bool)
	for i, t := range s.Teams {
		if i != teamIndex {
			used[t.Color] = true
		}
	}
	current := s.Teams[teamIndex].Color
	idx := 0
	for i, c := range TeamColors {
		if c == current {
			idx = i
			break
		}
	}
	for range TeamColors {
		idx = (idx + direction + len(TeamColors)) % len(TeamColors)
		if !used[TeamColors[idx]] {
			s.Teams[teamIndex].Color = TeamColors[idx]
			return
		}
	}
}

// PlayerTeamIndex returns the index of the team the player belongs to, or -1.
func (s *CentralState) PlayerTeamIndex(playerID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, t := range s.Teams {
		for _, id := range t.Players {
			if id == playerID {
				return i
			}
		}
	}
	return -1
}

// IsFirstInTeam returns true if playerID is the first player in their team.
func (s *CentralState) IsFirstInTeam(playerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.Teams {
		if len(t.Players) > 0 && t.Players[0] == playerID {
			return true
		}
	}
	return false
}

// TeamCount returns the number of non-empty teams.
func (s *CentralState) TeamCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Teams)
}

// IsSoleMemberOfTeam returns true if the player is the only member of their team.
func (s *CentralState) IsSoleMemberOfTeam(playerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.Teams {
		for _, id := range t.Players {
			if id == playerID {
				return len(t.Players) == 1
			}
		}
	}
	return false
}

// UnassignedPlayers returns player IDs not belonging to any team.
func (s *CentralState) UnassignedPlayers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	assigned := make(map[string]bool)
	for _, t := range s.Teams {
		for _, id := range t.Players {
			assigned[id] = true
		}
	}
	var result []string
	for id := range s.Players {
		if !assigned[id] {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		pi, pj := s.Players[result[i]], s.Players[result[j]]
		if pi != nil && pj != nil {
			return pi.Name < pj.Name
		}
		return result[i] < result[j]
	})
	return result
}
