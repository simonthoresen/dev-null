package client

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"io"
	"strconv"
	"strings"

	"dev-null/internal/clipboard"
	"dev-null/internal/domain"
	"dev-null/internal/render"
)

// GameSrcFile is a JS source file received from the server.
type GameSrcFile struct {
	Name    string
	Content string
}

// GameAsset is a binary asset file received from the server (audio, image).
type GameAsset struct {
	Name string // bare filename, e.g. "music.ogg"
	Data []byte // raw decompressed bytes
}

// SoundCmd is a play or stop sound instruction received from the server.
type SoundCmd struct {
	Filename string // file to play; empty on Stop means stop all
	Loop     bool
	Stop     bool // true = stop command
}

// Cell represents a single terminal cell in the parsed grid.
type Cell struct {
	Char rune
	Fg   color.RGBA
	Bg   color.RGBA
	Attr render.PixelAttr
}

// TerminalGrid holds the parsed terminal state.
type TerminalGrid struct {
	Width, Height int
	Cells         []Cell
	CursorX       int
	CursorY       int

	// Enhanced client protocol data parsed from OSC sequences.
	ViewportX   int
	ViewportY   int
	ViewportW   int
	ViewportH   int

	// Game source files for local rendering.
	GameSrcFiles []GameSrcFile // accumulated from ns;gamesrc OSC
	// Game state baseline (gzipped JSON from ns;state OSC). The client
	// replaces Game.state wholesale when this arrives.
	StateData []byte
	// Depth-1 merge patch (gzipped JSON from ns;state-patch OSC). The client
	// merges changed keys into Game.state and deletes keys whose value is null.
	StatePatch []byte
	// Render mode from ns;mode OSC ("local" or "remote").
	RenderMode string

	// Server-assigned player ID from ns;playerid OSC. The client defaults
	// this to the --player flag, but games key state by the SSH session ID
	// the server hands out — we replace the default once it arrives.
	PlayerID string

	// Asset loading (from ns;asset-manifest and ns;asset OSC).
	AssetManifestTotal int
	AssetFiles         []GameAsset

	// Sound commands from ns;sound and ns;stop-sound OSC.
	SoundCmds []SoundCmd

	// MIDI events from ns;midi OSC (decoded JSON).
	MidiEvents []domain.MidiEvent
	// SoundFont name from ns;synth OSC (e.g. "chiptune", "gm").
	SynthName string

	// Current SGR state for parsing.
	curFg   color.RGBA
	curBg   color.RGBA
	curAttr render.PixelAttr

	// Cursor visibility (DECTCEM: CSI ?25h / ?25l).
	CursorVisible bool

	// Saved cursor position (ESC 7 / ESC 8).
	savedCursorX int
	savedCursorY int

	// Scroll region (DECSTBM). Zero values mean full screen.
	scrollTop    int // 0-based inclusive top row
	scrollBottom int // 0-based inclusive bottom row (0 = use Height-1)

	// partial holds carry-over bytes from an incomplete escape sequence or
	// truncated multi-byte UTF-8 rune at the end of the previous Feed call.
	partial []byte
}

var defaultFg = color.RGBA{R: 204, G: 204, B: 204, A: 255}
var defaultBg = color.RGBA{R: 0, G: 0, B: 0, A: 255}

// NewTerminalGrid creates a grid with the given dimensions.
func NewTerminalGrid(width, height int) *TerminalGrid {
	g := &TerminalGrid{
		Width:  width,
		Height: height,
		Cells:  make([]Cell, width*height),
		curFg:  defaultFg,
		curBg:  defaultBg,
	}
	for i := range g.Cells {
		g.Cells[i] = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
	}
	return g
}

// Resize changes the grid dimensions, preserving content where possible.
func (g *TerminalGrid) Resize(width, height int) {
	cells := make([]Cell, width*height)
	for i := range cells {
		cells[i] = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
	}
	for y := 0; y < min(g.Height, height); y++ {
		for x := 0; x < min(g.Width, width); x++ {
			cells[y*width+x] = g.Cells[y*g.Width+x]
		}
	}
	g.Width = width
	g.Height = height
	g.Cells = cells
}

// At returns a pointer to the cell at (x, y), or nil if out of bounds.
func (g *TerminalGrid) At(x, y int) *Cell {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return nil
	}
	return &g.Cells[y*g.Width+x]
}

// ToImageBuffer converts the grid to a render.ImageBuffer for DrawImageBuffer.
func (g *TerminalGrid) ToImageBuffer() *render.ImageBuffer {
	buf := &render.ImageBuffer{
		Width:  g.Width,
		Height: g.Height,
		Pixels: make([]render.Pixel, g.Width*g.Height),
	}
	for i, cell := range g.Cells {
		buf.Pixels[i] = render.Pixel{
			Char: cell.Char,
			Fg:   cell.Fg,
			Bg:   cell.Bg,
			Attr: cell.Attr,
		}
	}
	return buf
}

// scrollRegionBottom returns the effective bottom row of the scroll region.
func (g *TerminalGrid) scrollRegionBottom() int {
	if g.scrollBottom > 0 {
		return g.scrollBottom
	}
	return g.Height - 1
}

// scrollUp shifts lines up within the scroll region, inserting blank lines at the bottom.
func (g *TerminalGrid) scrollUp(n int) {
	top := g.scrollTop
	bot := g.scrollRegionBottom()
	for count := 0; count < n; count++ {
		for y := top; y < bot; y++ {
			for x := 0; x < g.Width; x++ {
				if dst := g.At(x, y); dst != nil {
					if src := g.At(x, y+1); src != nil {
						*dst = *src
					}
				}
			}
		}
		// Clear the bottom line.
		for x := 0; x < g.Width; x++ {
			if cell := g.At(x, bot); cell != nil {
				*cell = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
			}
		}
	}
}

// scrollDown shifts lines down within the scroll region, inserting blank lines at the top.
func (g *TerminalGrid) scrollDown(n int) {
	top := g.scrollTop
	bot := g.scrollRegionBottom()
	for count := 0; count < n; count++ {
		for y := bot; y > top; y-- {
			for x := 0; x < g.Width; x++ {
				if dst := g.At(x, y); dst != nil {
					if src := g.At(x, y-1); src != nil {
						*dst = *src
					}
				}
			}
		}
		// Clear the top line.
		for x := 0; x < g.Width; x++ {
			if cell := g.At(x, top); cell != nil {
				*cell = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
			}
		}
	}
}

// insertLines inserts n blank lines at the cursor row, shifting existing lines down.
func (g *TerminalGrid) insertLines(n int) {
	bot := g.scrollRegionBottom()
	for count := 0; count < n; count++ {
		for y := bot; y > g.CursorY; y-- {
			for x := 0; x < g.Width; x++ {
				if dst := g.At(x, y); dst != nil {
					if src := g.At(x, y-1); src != nil {
						*dst = *src
					}
				}
			}
		}
		for x := 0; x < g.Width; x++ {
			if cell := g.At(x, g.CursorY); cell != nil {
				*cell = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
			}
		}
	}
}

// deleteLines deletes n lines at the cursor row, shifting lines up from below.
func (g *TerminalGrid) deleteLines(n int) {
	bot := g.scrollRegionBottom()
	for count := 0; count < n; count++ {
		for y := g.CursorY; y < bot; y++ {
			for x := 0; x < g.Width; x++ {
				if dst := g.At(x, y); dst != nil {
					if src := g.At(x, y+1); src != nil {
						*dst = *src
					}
				}
			}
		}
		for x := 0; x < g.Width; x++ {
			if cell := g.At(x, bot); cell != nil {
				*cell = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
			}
		}
	}
}

// Clear resets all cells to blank with default colors.
func (g *TerminalGrid) Clear() {
	for i := range g.Cells {
		g.Cells[i] = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
	}
	g.CursorX = 0
	g.CursorY = 0
	g.partial = nil
}

// Feed processes raw ANSI output from the server. It is safe to call
// repeatedly with streaming data — incomplete escape sequences and truncated
// multi-byte UTF-8 runes at the end of a chunk are buffered and prepended to
// the next call, so a sequence split across two SSH reads is handled correctly.
func (g *TerminalGrid) Feed(data []byte) {
	// Prepend any carry-over bytes from a previous incomplete sequence.
	if len(g.partial) > 0 {
		data = append(g.partial, data...)
		g.partial = nil
	}

	i := 0
	for i < len(data) {
		b := data[i]

		if b == '\x1b' {
			escStart := i
			i++
			if i >= len(data) {
				// Lone ESC at end — save it; may be the start of a sequence.
				g.partial = append(g.partial, data[escStart:]...)
				return
			}

			switch data[i] {
			case '[': // CSI sequence
				i++
				newI := g.parseCSI(data, i)
				if newI < 0 {
					// Incomplete CSI — carry over from the opening ESC.
					g.partial = append(g.partial, data[escStart:]...)
					return
				}
				i = newI
			case ']': // OSC sequence
				i++
				newI := g.parseOSC(data, i)
				if newI < 0 {
					// Incomplete OSC — carry over from the opening ESC.
					g.partial = append(g.partial, data[escStart:]...)
					return
				}
				i = newI
			case 'M': // RI — Reverse Index: cursor up one line, scroll down if at top
				i++
				if g.CursorY > g.scrollTop {
					g.CursorY--
				} else {
					g.scrollDown(1)
				}
			case '7': // DECSC — Save Cursor
				i++
				g.savedCursorX = g.CursorX
				g.savedCursorY = g.CursorY
			case '8': // DECRC — Restore Cursor
				i++
				g.CursorX = g.savedCursorX
				g.CursorY = g.savedCursorY
			case 'H': // HTS — Horizontal Tab Set (not cursor position)
				i++ // ignore — we don't track custom tab stops
			default:
				i++ // unknown two-byte escape — skip
			}
			continue
		}

		if b == '\b' {
			if g.CursorX > 0 {
				g.CursorX--
			}
			i++
			continue
		}

		if b == '\n' {
			g.CursorX = 0
			g.CursorY++
			i++
			continue
		}

		if b == '\r' {
			g.CursorX = 0
			i++
			continue
		}

		if b == '\t' {
			// Advance to next 8-column tab stop, clamped to last column.
			next := ((g.CursorX/8) + 1) * 8
			if next >= g.Width {
				next = g.Width - 1
			}
			g.CursorX = next
			i++
			continue
		}

		// Regular character — check for a truncated multi-byte UTF-8 sequence
		// before attempting to decode, so we don't corrupt the continuation bytes.
		if b >= 0x80 {
			var needed int
			switch {
			case b&0xE0 == 0xC0:
				needed = 2
			case b&0xF0 == 0xE0:
				needed = 3
			case b&0xF8 == 0xF0:
				needed = 4
			}
			if needed > 0 && i+needed > len(data) {
				// Truncated multi-byte sequence at end of chunk — carry over.
				g.partial = append(g.partial, data[i:]...)
				return
			}
		}

		r, size := decodeUTF8(data[i:])
		if size == 0 {
			i++
			continue
		}
		i += size

		if cell := g.At(g.CursorX, g.CursorY); cell != nil {
			cell.Char = r
			cell.Fg = g.curFg
			cell.Bg = g.curBg
			cell.Attr = g.curAttr
		}
		g.CursorX++
	}
}

// parseCSI parses a CSI (Control Sequence Introducer) sequence starting after "\x1b[".
// Returns the index past the end of the sequence, or -1 if the data ended
// before the command byte was found (incomplete sequence).
func (g *TerminalGrid) parseCSI(data []byte, start int) int {
	i := start
	// Collect parameter bytes (0x30-0x3F: digits, semicolons, private-mode prefixes like ?<=).
	paramStart := i
	for i < len(data) && data[i] >= 0x30 && data[i] <= 0x3F {
		i++
	}
	if i >= len(data) {
		return -1 // incomplete: command byte not yet received
	}
	params := string(data[paramStart:i])

	// Consume intermediate bytes (0x20-0x2F: space, !, ", #, $, %, &, ', (, ), *, +, ,, -, ., /).
	for i < len(data) && data[i] >= 0x20 && data[i] <= 0x2F {
		i++
	}
	if i >= len(data) {
		return -1 // incomplete: command byte not yet received
	}

	cmd := data[i]
	i++

	switch cmd {
	case 'm': // SGR — Select Graphic Rendition
		g.parseSGR(params)
	case 'H', 'f': // Cursor position
		row, col := 1, 1
		parts := strings.Split(params, ";")
		if len(parts) >= 1 && parts[0] != "" {
			row, _ = strconv.Atoi(parts[0])
		}
		if len(parts) >= 2 && parts[1] != "" {
			col, _ = strconv.Atoi(parts[1])
		}
		g.CursorY = row - 1
		g.CursorX = col - 1
	case 'J': // ED — Erase in Display
		n := 0
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		switch n {
		case 0: // Erase below (cursor to end of screen).
			for x := g.CursorX; x < g.Width; x++ {
				if cell := g.At(x, g.CursorY); cell != nil {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
			for y := g.CursorY + 1; y < g.Height; y++ {
				for x := 0; x < g.Width; x++ {
					if cell := g.At(x, y); cell != nil {
						*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
					}
				}
			}
		case 1: // Erase above (start of screen to cursor).
			for y := 0; y < g.CursorY; y++ {
				for x := 0; x < g.Width; x++ {
					if cell := g.At(x, y); cell != nil {
						*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
					}
				}
			}
			for x := 0; x <= g.CursorX; x++ {
				if cell := g.At(x, g.CursorY); cell != nil {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
		case 2, 3: // Erase entire screen.
			g.Clear()
		}
	case 'K': // EL — Erase in Line
		n := 0
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		switch n {
		case 0: // Clear from cursor to end of line.
			for x := g.CursorX; x < g.Width; x++ {
				if cell := g.At(x, g.CursorY); cell != nil {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
		case 1: // Clear from start of line to cursor.
			for x := 0; x <= g.CursorX; x++ {
				if cell := g.At(x, g.CursorY); cell != nil {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
		case 2: // Clear entire line.
			for x := 0; x < g.Width; x++ {
				if cell := g.At(x, g.CursorY); cell != nil {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
		}
	case 'A': // Cursor up
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.CursorY -= n
		if g.CursorY < 0 {
			g.CursorY = 0
		}
	case 'B': // Cursor down
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.CursorY += n
	case 'C': // Cursor forward
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.CursorX += n
	case 'D': // Cursor backward
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.CursorX -= n
		if g.CursorX < 0 {
			g.CursorX = 0
		}
	case 'G': // CHA — Cursor Horizontal Absolute
		col := 1
		if params != "" {
			col, _ = strconv.Atoi(params)
		}
		g.CursorX = col - 1
	case 'd': // VPA — Vertical Position Absolute
		row := 1
		if params != "" {
			row, _ = strconv.Atoi(params)
		}
		g.CursorY = row - 1
	case 'X': // ECH — Erase Character: erase N chars at cursor, cursor does NOT advance
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		for x := g.CursorX; x < g.CursorX+n && x < g.Width; x++ {
			if cell := g.At(x, g.CursorY); cell != nil {
				*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
			}
		}
	case 'b': // REP — Repeat preceding character
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		prevX := g.CursorX - 1
		if prevX >= 0 {
			if prev := g.At(prevX, g.CursorY); prev != nil {
				for j := 0; j < n; j++ {
					if cell := g.At(g.CursorX, g.CursorY); cell != nil {
						cell.Char = prev.Char
						cell.Fg = g.curFg
						cell.Bg = g.curBg
						cell.Attr = g.curAttr
					}
					g.CursorX++
				}
			}
		}
	case 'L': // IL — Insert Lines
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.insertLines(n)
	case 'M': // DL — Delete Lines
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.deleteLines(n)
	case 'S': // SU — Scroll Up
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		g.scrollUp(n)
	case 'T': // SD — Scroll Down
		// Only handle numeric params (not mouse tracking responses which have ';').
		if !strings.Contains(params, ";") {
			n := 1
			if params != "" {
				n, _ = strconv.Atoi(params)
			}
			g.scrollDown(n)
		}
	case 'P': // DCH — Delete Character
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		y := g.CursorY
		for x := g.CursorX; x < g.Width; x++ {
			src := x + n
			if cell := g.At(x, y); cell != nil {
				if srcCell := g.At(src, y); srcCell != nil {
					*cell = *srcCell
				} else {
					*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
				}
			}
		}
	case '@': // ICH — Insert Character
		n := 1
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		y := g.CursorY
		// Shift characters right from end of line.
		for x := g.Width - 1; x >= g.CursorX+n; x-- {
			if cell := g.At(x, y); cell != nil {
				if srcCell := g.At(x-n, y); srcCell != nil {
					*cell = *srcCell
				}
			}
		}
		// Clear inserted positions.
		for x := g.CursorX; x < g.CursorX+n && x < g.Width; x++ {
			if cell := g.At(x, y); cell != nil {
				*cell = Cell{Char: ' ', Fg: g.curFg, Bg: g.curBg}
			}
		}
	case 'r': // DECSTBM — Set Scrolling Region
		top, bottom := 1, g.Height
		parts := strings.Split(params, ";")
		if len(parts) >= 1 && parts[0] != "" {
			top, _ = strconv.Atoi(parts[0])
		}
		if len(parts) >= 2 && parts[1] != "" {
			bottom, _ = strconv.Atoi(parts[1])
		}
		g.scrollTop = top - 1
		g.scrollBottom = bottom - 1
		g.CursorX = 0
		g.CursorY = 0
	case 'h': // SM — Set Mode (handle private modes with ? prefix)
		if strings.HasPrefix(params, "?") {
			for _, p := range strings.Split(params[1:], ";") {
				if n, _ := strconv.Atoi(p); n == 25 {
					g.CursorVisible = true
				}
			}
		}
	case 'l': // RM — Reset Mode (handle private modes with ? prefix)
		if strings.HasPrefix(params, "?") {
			for _, p := range strings.Split(params[1:], ";") {
				if n, _ := strconv.Atoi(p); n == 25 {
					g.CursorVisible = false
				}
			}
		}
	}

	return i
}

// parseSGR processes SGR parameters.
func (g *TerminalGrid) parseSGR(params string) {
	if params == "" || params == "0" {
		g.curFg = defaultFg
		g.curBg = defaultBg
		g.curAttr = render.AttrNone
		return
	}

	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		switch {
		case n == 0:
			g.curFg = defaultFg
			g.curBg = defaultBg
			g.curAttr = render.AttrNone
		case n == 1:
			g.curAttr |= render.AttrBold
		case n == 2:
			g.curAttr |= render.AttrFaint
		case n == 3:
			g.curAttr |= render.AttrItalic
		case n == 4:
			g.curAttr |= render.AttrUnderline
		case n == 7:
			g.curAttr |= render.AttrReverse
		case n == 22:
			g.curAttr &^= render.AttrBold | render.AttrFaint
		case n == 23:
			g.curAttr &^= render.AttrItalic
		case n == 24:
			g.curAttr &^= render.AttrUnderline
		case n == 27:
			g.curAttr &^= render.AttrReverse
		case n == 38: // Foreground: 38;2;R;G;B
			if i+4 < len(parts) && parts[i+1] == "2" {
				r, _ := strconv.Atoi(parts[i+2])
				gg, _ := strconv.Atoi(parts[i+3])
				b, _ := strconv.Atoi(parts[i+4])
				g.curFg = color.RGBA{R: uint8(r), G: uint8(gg), B: uint8(b), A: 255}
				i += 4
			}
		case n == 48: // Background: 48;2;R;G;B
			if i+4 < len(parts) && parts[i+1] == "2" {
				r, _ := strconv.Atoi(parts[i+2])
				gg, _ := strconv.Atoi(parts[i+3])
				b, _ := strconv.Atoi(parts[i+4])
				g.curBg = color.RGBA{R: uint8(r), G: uint8(gg), B: uint8(b), A: 255}
				i += 4
			}
		case n == 39: // Default foreground
			g.curFg = defaultFg
		case n == 49: // Default background
			g.curBg = defaultBg
		}
	}
}

// parseOSC parses an OSC sequence starting after "\x1b]".
// Format: \x1b]<payload>\x07 or \x1b]<payload>\x1b\\
func (g *TerminalGrid) parseOSC(data []byte, start int) int {
	i := start
	// Find the ST (string terminator): BEL (\x07) or ESC+backslash (\x1b\\).
	end := -1
	for j := i; j < len(data); j++ {
		if data[j] == '\x07' {
			end = j
			break
		}
		if data[j] == '\x1b' && j+1 < len(data) && data[j+1] == '\\' {
			end = j
			break
		}
	}
	if end < 0 {
		// Unterminated — sequence incomplete; caller will carry over the bytes.
		return -1
	}

	payload := string(data[i:end])
	g.handleOSC(payload)

	// Advance past the terminator.
	if data[end] == '\x07' {
		return end + 1
	}
	return end + 2 // ESC + backslash
}

// handleOSC processes a dev-null OSC payload.
func (g *TerminalGrid) handleOSC(payload string) {
	// Standard OSC 52 clipboard: "52;c;<base64>"
	if strings.HasPrefix(payload, "52;c;") {
		if decoded, err := base64.StdEncoding.DecodeString(payload[5:]); err == nil {
			clipboard.Copy(string(decoded)) //nolint:errcheck
		}
		return
	}

	if !strings.HasPrefix(payload, "ns;") {
		return // not a dev-null OSC
	}

	rest := payload[3:]
	if idx := strings.Index(rest, ";"); idx >= 0 {
		kind := rest[:idx]
		data := rest[idx+1:]
		switch kind {
		case "gamesrc":
			// Format: ns;gamesrc;<filename>;<base64 gzipped JS>
			if sepIdx := strings.Index(data, ";"); sepIdx >= 0 {
				filename := data[:sepIdx]
				if decoded, err := decodeBase64Str(data[sepIdx+1:]); err == nil {
					g.GameSrcFiles = append(g.GameSrcFiles, GameSrcFile{Name: filename, Content: decompressString(decoded)})
				}
			}
		case "state":
			if decoded, err := decodeBase64Str(data); err == nil {
				g.StateData = decoded
			}
		case "state-patch":
			if decoded, err := decodeBase64Str(data); err == nil {
				g.StatePatch = decoded
			}
		case "mode":
			g.RenderMode = data
		case "playerid":
			g.PlayerID = data
		case "viewport":
			parts := strings.Split(data, ",")
			if len(parts) == 4 {
				g.ViewportX, _ = strconv.Atoi(parts[0])
				g.ViewportY, _ = strconv.Atoi(parts[1])
				g.ViewportW, _ = strconv.Atoi(parts[2])
				g.ViewportH, _ = strconv.Atoi(parts[3])
			}
		case "asset-manifest":
			if n, err := strconv.Atoi(data); err == nil {
				g.AssetManifestTotal = n
			}
		case "asset":
			// Format: ns;asset;<name>;<base64 gzipped data>
			if sepIdx := strings.Index(data, ";"); sepIdx >= 0 {
				name := data[:sepIdx]
				if decoded, err := decodeBase64Str(data[sepIdx+1:]); err == nil {
					if raw := decompressBytes(decoded); raw != nil {
						g.AssetFiles = append(g.AssetFiles, GameAsset{Name: name, Data: raw})
					}
				}
			}
		case "sound":
			// Format: ns;sound;<filename>;<options>  e.g. music.ogg;loop=1
			if sepIdx := strings.Index(data, ";"); sepIdx >= 0 {
				filename := data[:sepIdx]
				opts := data[sepIdx+1:]
				loop := strings.Contains(opts, "loop=1")
				g.SoundCmds = append(g.SoundCmds, SoundCmd{Filename: filename, Loop: loop})
			}
		case "stop-sound":
			g.SoundCmds = append(g.SoundCmds, SoundCmd{Filename: data, Stop: true})
		case "midi":
			if decoded, err := decodeBase64Str(data); err == nil {
				var events []domain.MidiEvent
				if json.Unmarshal(decoded, &events) == nil {
					g.MidiEvents = append(g.MidiEvents, events...)
				}
			}
		case "synth":
			g.SynthName = data
		}
	}
}

// decodeBase64Str decodes a standard base64 string.
func decodeBase64Str(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// decompressString decompresses gzipped data to a string.
func decompressString(data []byte) string {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return ""
	}
	return string(raw)
}

// decodeUTF8 decodes the first UTF-8 rune from data.
func decodeUTF8(data []byte) (rune, int) {
	if len(data) == 0 {
		return 0, 0
	}
	b := data[0]
	if b < 0x80 {
		return rune(b), 1
	}
	var size int
	var r rune
	switch {
	case b&0xE0 == 0xC0:
		size = 2
		r = rune(b & 0x1F)
	case b&0xF0 == 0xE0:
		size = 3
		r = rune(b & 0x0F)
	case b&0xF8 == 0xF0:
		size = 4
		r = rune(b & 0x07)
	default:
		return 0xFFFD, 1
	}
	if len(data) < size {
		return 0xFFFD, 1
	}
	for i := 1; i < size; i++ {
		if data[i]&0xC0 != 0x80 {
			return 0xFFFD, 1
		}
		r = r<<6 | rune(data[i]&0x3F)
	}
	return r, size
}
