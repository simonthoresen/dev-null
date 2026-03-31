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
	IsReply   bool   // command response to caller — render as plain text, no prefix
}

// GamePhase represents the current phase of the game lifecycle.
type GamePhase int

const (
	PhaseNone     GamePhase = 0 // lobby — no game loaded
	PhaseSplash   GamePhase = 1 // splash screen before game starts
	PhasePlaying  GamePhase = 2 // game is actively running
	PhaseGameOver GamePhase = 3 // game-over screen, waiting for acknowledgment
)

// GameResult is a single entry in the game-over results, displayed in the
// order provided by the game (first = winner).
type GameResult struct {
	Name   string // display name (e.g. player name, team name)
	Result string // freeform result text (e.g. "4200 pts", "1st", "DNF")
}

// Team is a group of players configured in the lobby before a game starts.
type Team struct {
	Name    string   // unique display name
	Color   string   // CSS hex color, e.g. "#ff5555"
	Players []string // player IDs, ordered
}

// TeamRange specifies the min/max number of teams a game supports.
// Zero means no constraint on that end.
type TeamRange struct {
	Min int
	Max int
}

// Tea messages

type TickMsg struct{ N int }            // broadcast to all programs every 100ms; N is tick counter
type PlayerJoinedMsg struct{ Player *Player }
type PlayerLeftMsg struct{ PlayerID string }
type ChatMsg struct{ Msg Message }
type GameLoadedMsg struct{ Name string }
type GameUnloadedMsg struct{}
type GamePhaseMsg struct{ Phase GamePhase } // broadcast when game phase changes
type TeamUpdatedMsg struct{}                // broadcast when team assignments change
