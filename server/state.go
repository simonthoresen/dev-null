package server

import (
	"null-space/common"
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

	ActiveApp common.App
	AppName   string

	Plugins     []common.Plugin
	PluginNames []string // parallel to Plugins; name = filename stem

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

// AddPlugin appends a plugin. Caller must ensure name is unique.
func (s *CentralState) AddPlugin(name string, p common.Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Plugins = append(s.Plugins, p)
	s.PluginNames = append(s.PluginNames, name)
}

// RemovePlugin removes the named plugin and returns it (nil if not found).
func (s *CentralState) RemovePlugin(name string) common.Plugin {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, n := range s.PluginNames {
		if strings.EqualFold(n, name) {
			p := s.Plugins[i]
			s.Plugins = append(s.Plugins[:i], s.Plugins[i+1:]...)
			s.PluginNames = append(s.PluginNames[:i], s.PluginNames[i+1:]...)
			return p
		}
	}
	return nil
}

// GetPlugins returns copies of the current plugin slice and name slice.
func (s *CentralState) GetPlugins() ([]common.Plugin, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ps := make([]common.Plugin, len(s.Plugins))
	ns := make([]string, len(s.PluginNames))
	copy(ps, s.Plugins)
	copy(ns, s.PluginNames)
	return ps, ns
}

// RLock/RUnlock for external readers (e.g. main.go reading Net).
func (s *CentralState) RLock()   { s.mu.RLock() }
func (s *CentralState) RUnlock() { s.mu.RUnlock() }
