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

	// Sound fields — optional. Graphical clients act on these via OSC; all others ignore them.
	// Messages with empty Text and a sound action are not stored in chat history.
	SoundFile string // filename to play on graphical clients (e.g. "music.ogg"); empty = no sound
	SoundLoop bool   // true = loop the sound
	SoundStop bool   // true = this is a stop-sound command (SoundFile="" means stop all)

	// MIDI fields — optional. Graphical clients synthesize these via SoundFont; others ignore.
	// Messages with empty Text and MIDI events are not stored in chat history.
	MidiEvents []MidiEvent // MIDI events to send to graphical clients
}

// MidiEventType identifies the kind of MIDI event.
type MidiEventType int

const (
	MidiNoteOn        MidiEventType = iota // Note on (with optional auto-off duration)
	MidiProgramChange                      // Instrument/program change
	MidiControlChange                      // Control change (volume, pan, etc.)
)

// MidiEvent is a MIDI event sent from a game to graphical clients for synthesis.
type MidiEvent struct {
	Type       MidiEventType `json:"t"`            // event type
	Channel    int           `json:"c"`            // MIDI channel (0-15)
	Note       int           `json:"n,omitempty"`  // note number (0-127)
	Velocity   int           `json:"v,omitempty"`  // velocity (0-127) or CC value
	DurationMs int           `json:"d,omitempty"`  // auto-NoteOff delay in ms (0 = manual)
	Program    int           `json:"p,omitempty"`  // program number (0-127)
	Controller int           `json:"ct,omitempty"` // controller number (0-127)
}

// GameAsset is a binary asset file bundled with a folder-based game.
type GameAsset struct {
	Name string // bare filename, e.g. "music.ogg"
	Data []byte // raw file bytes
}

// StateSnapshot is a per-tick marshaled view of Game.state that the server
// produces once and every local-rendering player reads. Full is the baseline
// JSON sent on a session's first broadcast; Keys holds one entry per
// top-level state key so each player can independently diff against its own
// last-sent set. Keys stay byte-stable once the tick moves on, so Models may
// hold references across frames without copying.
type StateSnapshot struct {
	Full []byte
	Keys map[string][]byte
}

// GamePhase represents the current phase of the game lifecycle.
type GamePhase int

const (
	PhaseNone     GamePhase = 0 // lobby — no game loaded
	PhaseStarting GamePhase = 1 // starting screen before game begins
	PhasePlaying  GamePhase = 2 // game is actively running
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

	// MaxPlayerNameLen caps the length of sanitized player names.
	MaxPlayerNameLen = 50
)

// GraphicsMode controls how a player's game viewport is displayed.
// Orthogonal to render location (local vs remote), which is a separate bool.
type GraphicsMode int

const (
	// ModeAscii renders using the game's text-based renderAscii() function.
	ModeAscii GraphicsMode = iota
	// ModeBlocks converts canvas output to Unicode quadrant block characters
	// (2×2 pixels per terminal cell). Requires renderCanvas.
	ModeBlocks
	// ModePixels renders the canvas at full window pixel resolution.
	// Always local (client-side). Requires enhanced client + renderCanvas.
	ModePixels
)

// Label returns a human-readable name for the mode.
func (m GraphicsMode) Label() string {
	switch m {
	case ModeAscii:
		return "Ascii"
	case ModeBlocks:
		return "Blocks"
	case ModePixels:
		return "Pixels"
	default:
		return "Unknown"
	}
}

const (

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
type GamePhaseMsg struct{ Phase GamePhase }       // broadcast when game phase changes
type StartingReadyMsg struct{ PlayerID string }   // broadcast when a player readies up
type TeamUpdatedMsg struct{}                       // broadcast when team assignments change
type GameSuspendedMsg struct{ Name string } // broadcast when a game is suspended
type GameResumedMsg struct{ Name string }   // broadcast when a suspended game is resumed
type QuitRequestMsg struct{}                // sent to a player's program to request exit
