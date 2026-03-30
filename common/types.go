package common

// Player is a connected SSH client.
type Player struct {
	ID         string
	Name       string
	IsAdmin    bool
	TermWidth  int
	TermHeight int
}

// Message is a chat entry. IsPrivate=true means only sender, recipient, and server console see it.
type Message struct {
	Author    string // empty = system message
	Text      string
	IsPrivate bool
	ToID      string // recipient player ID (if private)
	FromID    string // sender player ID (if private)
}

// Tea messages

type TickMsg struct{ N int }            // broadcast to all programs every 100ms; N is tick counter
type PlayerJoinedMsg struct{ Player *Player }
type PlayerLeftMsg struct{ PlayerID string }
type ChatMsg struct{ Msg Message }
type GameLoadedMsg struct{ Name string }
type GameUnloadedMsg struct{}
