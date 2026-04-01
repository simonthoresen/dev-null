package server

import (
	"null-space/common"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxChatHistory = 50

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type networkInfo struct {
	LocalPort  int
	LANIP      string
	PublicIP   string // UPnP or detected
	PinggyURL  string // e.g. "tcp.eu.pinggy.io:12345"
	UPnPMapped bool
}

type CentralState struct {
	mu sync.RWMutex

	AdminPassword string
	StartTime     time.Time
	TickN         int // increments every 100ms; SpinnerFrame = TickN/10

	ActiveGame common.Game
	GameName   string
	GamePhase  common.GamePhase

	// GameTeams is a snapshot of the teams at game load time.
	// Separate from lobby Teams so the lobby stays editable during a game.
	GameTeams []common.Team

	// GameDisconnected maps player name → game player ID for players who
	// disconnected mid-game. Used to rejoin them on reconnect.
	GameDisconnected map[string]string

	// GameOverReady tracks which players have acknowledged the game-over screen.
	GameOverReady   map[string]bool
	GameOverResults []common.GameResult // ranked results from gameOver()

	// Teams configured in the lobby before a game starts.
	Teams []common.Team

	Players     map[string]*common.Player
	ChatHistory []common.Message

	Net networkInfo
}

func newState(password string) *CentralState {
	return &CentralState{
		AdminPassword: password,
		StartTime:     time.Now(),
		Players:       make(map[string]*common.Player),
	}
}

func (s *CentralState) SpinnerChar() rune {
	frame := (s.TickN / 10) % len(spinnerFrames)
	return spinnerFrames[frame]
}

func (s *CentralState) AddChat(msg common.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ChatHistory = append(s.ChatHistory, msg)
	if len(s.ChatHistory) > maxChatHistory {
		s.ChatHistory = s.ChatHistory[len(s.ChatHistory)-maxChatHistory:]
	}
}

func (s *CentralState) GetChatHistory() []common.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]common.Message, len(s.ChatHistory))
	copy(result, s.ChatHistory)
	return result
}

func (s *CentralState) AddPlayer(player *common.Player) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players[player.ID] = player
}

func (s *CentralState) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Players, playerID)
}

func (s *CentralState) GetPlayer(playerID string) *common.Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Players[playerID]
}

func (s *CentralState) SetPlayerAdmin(playerID string, isAdmin bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if player, ok := s.Players[playerID]; ok {
		player.IsAdmin = isAdmin
	}
}

func (s *CentralState) PlayerByName(name string) *common.Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.Players {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return nil
}

func (s *CentralState) PlayerNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.Players))
	for _, p := range s.Players {
		names = append(names, p.Name)
	}
	return names
}

func (s *CentralState) ListPlayers() []*common.Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	players := make([]*common.Player, 0, len(s.Players))
	for _, p := range s.Players {
		players = append(players, p)
	}
	return players
}

func (s *CentralState) PlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Players)
}

// --- Game phase helpers ---

func (s *CentralState) SetGamePhase(phase common.GamePhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GamePhase = phase
	if phase == common.PhaseGameOver {
		s.GameOverReady = make(map[string]bool)
	} else {
		s.GameOverReady = nil
		s.GameOverResults = nil
	}
	if phase == common.PhaseNone {
		s.GameTeams = nil
		s.GameDisconnected = nil
	}
}

func (s *CentralState) GetGamePhase() common.GamePhase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GamePhase
}

func (s *CentralState) MarkPlayerReady(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GameOverReady != nil {
		s.GameOverReady[playerID] = true
	}
}

// AllPlayersReady returns true if every connected game player has acknowledged.
func (s *CentralState) AllPlayersReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.GameOverReady == nil {
		return false
	}
	for _, t := range s.GameTeams {
		for _, id := range t.Players {
			// Only check players who are still connected.
			if s.Players[id] != nil && !s.GameOverReady[id] {
				return false
			}
		}
	}
	return true
}

// IsGamePlayer returns true if the player is in any game team.
func (s *CentralState) IsGamePlayer(playerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.GameTeams {
		for _, id := range t.Players {
			if id == playerID {
				return true
			}
		}
	}
	return false
}

// GetGameTeams returns a deep copy of the game teams snapshot.
func (s *CentralState) GetGameTeams() []common.Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	teams := make([]common.Team, len(s.GameTeams))
	for i, t := range s.GameTeams {
		teams[i] = common.Team{
			Name:    t.Name,
			Color:   t.Color,
			Players: append([]string(nil), t.Players...),
		}
	}
	return teams
}

// replaceGamePlayerIDLocked swaps an old player ID for a new one in game teams.
// Caller must hold s.mu.
func (s *CentralState) replaceGamePlayerIDLocked(oldID, newID string) {
	for i := range s.GameTeams {
		for j, id := range s.GameTeams[i].Players {
			if id == oldID {
				s.GameTeams[i].Players[j] = newID
				return
			}
		}
	}
}


// --- Team helpers ---

// teamColors is the palette of available team colors.
var teamColors = []string{
	"#ff5555", // red
	"#5555ff", // blue
	"#55ff55", // green
	"#ffff55", // yellow
	"#55ffff", // cyan
	"#ff55ff", // magenta
	"#ffaa00", // orange
	"#aa55ff", // purple
}

func (s *CentralState) GetTeams() []common.Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	teams := make([]common.Team, len(s.Teams))
	for i, t := range s.Teams {
		teams[i] = common.Team{
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
	for _, c := range teamColors {
		if !used[c] {
			return c
		}
	}
	// all taken — just recycle the first one
	return teamColors[0]
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
		s.Teams = append(s.Teams, common.Team{
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
	for i, c := range teamColors {
		if c == current {
			idx = i
			break
		}
	}
	for range teamColors {
		idx = (idx + direction + len(teamColors)) % len(teamColors)
		if !used[teamColors[idx]] {
			s.Teams[teamIndex].Color = teamColors[idx]
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

// RLock/RUnlock for external readers (e.g. main.go reading Net).
func (s *CentralState) RLock()   { s.mu.RLock() }
func (s *CentralState) RUnlock() { s.mu.RUnlock() }
