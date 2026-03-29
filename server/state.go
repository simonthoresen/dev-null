package server

import (
	"strings"
	"sync"
	"time"

	"null-space/common"
)

type CentralState struct {
	sync.RWMutex
	ActiveGame  common.Game
	Players     map[string]*common.Player
	ChatHistory []string
	StartTime   time.Time
}

func newCentralState(game common.Game) *CentralState {
	return &CentralState{
		ActiveGame:  game,
		Players:     make(map[string]*common.Player),
		ChatHistory: make([]string, 0, 50),
		StartTime:   time.Now(),
	}
}

func (s *CentralState) AddPlayer(player *common.Player) {
	s.Lock()
	defer s.Unlock()
	s.Players[player.ID] = player
}

func (s *CentralState) RemovePlayer(playerID string) {
	s.Lock()
	defer s.Unlock()
	delete(s.Players, playerID)
}

func (s *CentralState) GetPlayer(playerID string) *common.Player {
	s.RLock()
	defer s.RUnlock()
	return s.Players[playerID]
}

func (s *CentralState) SetPlayerAdmin(playerID string, isAdmin bool) {
	s.Lock()
	defer s.Unlock()
	if player, ok := s.Players[playerID]; ok {
		player.IsAdmin = isAdmin
	}
}

func (s *CentralState) PlayerCount() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.Players)
}

func (s *CentralState) ListPlayers() []*common.Player {
	s.RLock()
	defer s.RUnlock()
	players := make([]*common.Player, 0, len(s.Players))
	for _, player := range s.Players {
		players = append(players, player)
	}
	return players
}

func (s *CentralState) PlayerByName(name string) *common.Player {
	s.RLock()
	defer s.RUnlock()
	for _, player := range s.Players {
		if strings.EqualFold(player.Name, name) {
			return player
		}
	}
	return nil
}

func (s *CentralState) AppendChat(line string) {
	s.Lock()
	defer s.Unlock()
	s.ChatHistory = append(s.ChatHistory, line)
	if len(s.ChatHistory) > 50 {
		s.ChatHistory = append([]string(nil), s.ChatHistory[len(s.ChatHistory)-50:]...)
	}
}

func (s *CentralState) ChatLines() []string {
	s.RLock()
	defer s.RUnlock()
	return append([]string(nil), s.ChatHistory...)
}