package domain

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

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
	Author       string // empty = system message
	Text         string
	IsPrivate    bool
	ToID         string // recipient player ID (if private)
	FromID       string // sender player ID (if private)
	IsReply      bool   // command response to caller — render as plain text, no prefix
	IsFromPlugin bool   // message originated from a plugin — plugins skip these to prevent loops
}

// GamePhase represents the current phase of the game lifecycle.
type GamePhase int

const (
	PhaseNone      GamePhase = 0 // lobby — no game loaded
	PhaseSplash    GamePhase = 1 // splash screen before game starts
	PhasePlaying   GamePhase = 2 // game is actively running
	PhaseGameOver  GamePhase = 3 // game-over screen, waiting for acknowledgment
	PhaseSuspended GamePhase = 4 // game suspended — runtime may be alive or restored from disk
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

// WidgetNode is a declarative UI node returned by Game.ViewNC().
// Games build a tree of these to describe their viewport layout using
// real NC-style controls rendered by the framework.
type WidgetNode struct {
	Type     string        // "gameview", "panel", "label", "hsplit", "vsplit", "divider", "table", "button", "textinput", "checkbox", "textview"
	Title    string        // panel title (for "panel")
	Text     string        // content text (for "label", "button", "checkbox")
	Align    string        // "left" (default), "center", "right" (for "label")
	Weight   float64       // flex weight in split layouts (0 = use fixed size)
	Width    int           // fixed width (for split children, 0 = use weight)
	Height   int           // fixed height (for split children, 0 = use weight)
	Rows     [][]string    // table rows (for "table")
	Children []*WidgetNode // child nodes (for split/panel containers)

	// Interactive control fields
	Action      string // action ID sent via OnInput when control is activated (for "button", "textinput", "checkbox")
	IsFocusable bool   // participates in Tab cycling (for "gameview")
	TabIndex    int    // focus order; lower values first (0 = default)
	Checked     bool   // current checked state (for "checkbox")
	Value       string // current value (for "textinput")
	Lines       []string // content lines (for "textview")
}

// Hash returns a content hash of this node and all descendants.
// gameview nodes return 0 (always cache-miss) because their content
// comes from JS View() which can change unpredictably.
func (n *WidgetNode) Hash() uint64 {
	if n == nil {
		return 0
	}
	// gameview nodes (and unknown types that fall back to gameview) are always dirty.
	if n.Type == "gameview" || n.Type == "" {
		return 0
	}
	// Interactive nodes are uncacheable — their rendered output depends on
	// framework state (focus highlight, cursor position) not just content.
	if n.Action != "" || n.IsFocusable {
		return 0
	}

	h := fnv.New64a()
	h.Write([]byte(n.Type))
	h.Write([]byte{0})
	h.Write([]byte(n.Title))
	h.Write([]byte{0})
	h.Write([]byte(n.Text))
	h.Write([]byte{0})
	h.Write([]byte(n.Align))
	h.Write([]byte{0})
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(n.Weight))
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:4], uint32(n.Width))
	h.Write(buf[:4])
	binary.LittleEndian.PutUint32(buf[:4], uint32(n.Height))
	h.Write(buf[:4])
	// Table rows.
	for _, row := range n.Rows {
		for _, cell := range row {
			h.Write([]byte(cell))
			h.Write([]byte{0})
		}
		h.Write([]byte{1}) // row separator
	}
	// Children: mix in each child's hash. A child hash of 0 (gameview)
	// makes the parent hash non-deterministic via the position encoding,
	// but we handle that at the cache lookup level — any subtree containing
	// a gameview will propagate 0 upward.
	for _, child := range n.Children {
		ch := child.Hash()
		if ch == 0 {
			return 0 // gameview descendant → entire subtree is uncacheable
		}
		binary.LittleEndian.PutUint64(buf[:], ch)
		h.Write(buf[:])
	}
	return h.Sum64()
}

// Shared constants used across packages.
const (
	// Time format constants — use these instead of inline format strings.
	TimeFormatDateTime = "2006-01-02 15:04:05" // status bars, chat logs
	TimeFormatFileSafe = "2006-01-02_15-04-05" // save file names
	TimeFormatShort    = "Jan 2 15:04"         // compact timestamps (menus, lists)

	// MaxChatDisplayLines is the maximum number of chat lines kept in the
	// per-player display buffer (separate from the server's MaxChatHistory).
	MaxChatDisplayLines = 200

	// Canvas scale bounds for /canvas scale <n>.
	MinCanvasScale = 1
	MaxCanvasScale = 32

	// MaxPlayerNameLen caps the length of sanitized player names.
	MaxPlayerNameLen = 50

	// MaxConnections is the default cap on concurrent SSH sessions.
	MaxConnections = 100
)

// Tea messages

type TickMsg struct{ N int }               // broadcast to all programs every tick; N is tick counter
type PlayerJoinedMsg struct{ Player *Player }
type PlayerLeftMsg struct{ PlayerID string }
type ChatMsg struct{ Msg Message }
type GameLoadedMsg struct{ Name string }
type GameUnloadedMsg struct{}
type GamePhaseMsg struct{ Phase GamePhase } // broadcast when game phase changes
type TeamUpdatedMsg struct{}                // broadcast when team assignments change
type GameSuspendedMsg struct{ Name string } // broadcast when a game is suspended
type GameResumedMsg struct{ Name string }   // broadcast when a suspended game is resumed
