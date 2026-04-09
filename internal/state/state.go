package state

import (
	"dev-null/internal/domain"
	"strings"
	"sync"
	"time"
)

const MaxChatHistory = 50


// NetworkInfo holds detected network configuration.
type NetworkInfo struct {
	LocalPort  int
	LANIP      string
	PublicIP   string // UPnP or detected
	PinggyURL  string // e.g. "tcp.eu.pinggy.io:12345"
	UPnPMapped bool
}

// CentralState is the shared game state protected by a read-write mutex.
type CentralState struct {
	mu sync.RWMutex

	AdminPassword string
	StartTime     time.Time
	TickN      int     // increments every tick
	ElapsedSec float64 // total seconds elapsed since server start (TickN * tickInterval)

	ActiveGame domain.Game
	GameName   string
	GamePhase  domain.GamePhase

	// GameTeams is a snapshot of the teams at game load time.
	// Separate from lobby Teams so the lobby stays editable during a game.
	GameTeams []domain.Team

	// GameDisconnected maps player name → game player ID for players who
	// disconnected mid-game. Used to rejoin them on reconnect.
	GameDisconnected map[string]string

	// GameOverReady tracks which players have acknowledged the game-over screen.
	GameOverReady   map[string]bool
	GameOverResults []domain.GameResult // ranked results from gameOver()

	// Teams configured in the lobby before a game starts.
	Teams []domain.Team

	Players     map[string]*domain.Player
	ChatHistory []domain.Message

	Net NetworkInfo

	// CanvasScale is the pixels-per-cell scaling factor for canvas rendering.
	// 0 = canvas rendering disabled. Typical values: 4, 8, 16.
	CanvasScale int
}

// New creates a new CentralState with the given admin password.
func New(password string) *CentralState {
	return &CentralState{
		AdminPassword: password,
		StartTime:     time.Now(),
		Players:       make(map[string]*domain.Player),
		CanvasScale:   8,
	}
}

// Lock acquires the write lock.
func (s *CentralState) Lock() { s.mu.Lock() }

// Unlock releases the write lock.
func (s *CentralState) Unlock() { s.mu.Unlock() }

// RLock acquires the read lock.
func (s *CentralState) RLock() { s.mu.RLock() }

// RUnlock releases the read lock.
func (s *CentralState) RUnlock() { s.mu.RUnlock() }

func (s *CentralState) AddChat(msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ChatHistory = append(s.ChatHistory, msg)
	if len(s.ChatHistory) > MaxChatHistory {
		s.ChatHistory = s.ChatHistory[len(s.ChatHistory)-MaxChatHistory:]
	}
}

func (s *CentralState) GetChatHistory() []domain.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.Message, len(s.ChatHistory))
	copy(result, s.ChatHistory)
	return result
}

func (s *CentralState) AddPlayer(player *domain.Player) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players[player.ID] = player
}

func (s *CentralState) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Players, playerID)
}

func (s *CentralState) GetPlayer(playerID string) *domain.Player {
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

func (s *CentralState) PlayerByName(name string) *domain.Player {
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

func (s *CentralState) ListPlayers() []*domain.Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	players := make([]*domain.Player, 0, len(s.Players))
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

func (s *CentralState) SetGamePhase(phase domain.GamePhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GamePhase = phase
	if phase == domain.PhaseEnding {
		s.GameOverReady = make(map[string]bool)
	} else {
		s.GameOverReady = nil
		s.GameOverResults = nil
	}
	if phase == domain.PhaseNone {
		s.GameTeams = nil
		s.GameDisconnected = nil
	}
}

func (s *CentralState) GetGamePhase() domain.GamePhase {
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
func (s *CentralState) GetGameTeams() []domain.Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	teams := make([]domain.Team, len(s.GameTeams))
	for i, t := range s.GameTeams {
		teams[i] = domain.Team{
			Name:    t.Name,
			Color:   t.Color,
			Players: append([]string(nil), t.Players...),
		}
	}
	return teams
}

// ReplaceGamePlayerID swaps an old player ID for a new one in game teams.
// Caller must hold the write lock.
func (s *CentralState) ReplaceGamePlayerID(oldID, newID string) {
	for i := range s.GameTeams {
		for j, id := range s.GameTeams[i].Players {
			if id == oldID {
				s.GameTeams[i].Players[j] = newID
				return
			}
		}
	}
}
