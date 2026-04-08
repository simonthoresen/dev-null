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
	CharmapJSON []byte // raw JSON from ns;charmap OSC
	AtlasData   []byte // gzipped PNG from ns;atlas OSC
	FrameData   []byte // gzipped PNG canvas frame from ns;frame OSC
	ViewportX   int
	ViewportY   int
	ViewportW   int
	ViewportH   int

	// Game source files for local rendering.
	GameSrcFiles []GameSrcFile // accumulated from ns;gamesrc OSC
	// Game state for local rendering (gzipped JSON from ns;state OSC).
	StateData []byte
	// Render mode from ns;mode OSC ("local" or "remote").
	RenderMode string

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

// Clear resets all cells to blank with default colors.
func (g *TerminalGrid) Clear() {
	for i := range g.Cells {
		g.Cells[i] = Cell{Char: ' ', Fg: defaultFg, Bg: defaultBg}
	}
	g.CursorX = 0
	g.CursorY = 0
}

// Feed processes raw ANSI output from the server.
func (g *TerminalGrid) Feed(data []byte) {
	i := 0
	for i < len(data) {
		b := data[i]

		if b == '\x1b' {
			// Escape sequence.
			i++
			if i >= len(data) {
				break
			}

			switch data[i] {
			case '[': // CSI sequence
				i++
				i = g.parseCSI(data, i)
			case ']': // OSC sequence
				i++
				i = g.parseOSC(data, i)
			default:
				i++
			}
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

		// Regular character — decode UTF-8.
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
// Returns the index past the end of the sequence.
func (g *TerminalGrid) parseCSI(data []byte, start int) int {
	i := start
	// Collect parameter bytes (digits and semicolons).
	paramStart := i
	for i < len(data) && ((data[i] >= '0' && data[i] <= '9') || data[i] == ';' || data[i] == '?') {
		i++
	}
	if i >= len(data) {
		return i
	}

	params := string(data[paramStart:i])
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
	case 'J': // Erase in display
		n := 0
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		if n == 2 || n == 3 {
			g.Clear()
		}
	case 'K': // Erase in line
		n := 0
		if params != "" {
			n, _ = strconv.Atoi(params)
		}
		if n == 0 {
			// Clear from cursor to end of line.
			for x := g.CursorX; x < g.Width; x++ {
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
		// Unterminated — skip to end.
		return len(data)
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
		case "charmap":
			if decoded, err := decodeBase64Str(data); err == nil {
				g.CharmapJSON = decoded
			}
		case "atlas":
			if decoded, err := decodeBase64Str(data); err == nil {
				g.AtlasData = decoded
			}
		case "frame":
			if decoded, err := decodeBase64Str(data); err == nil {
				g.FrameData = decoded
			}
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
		case "mode":
			g.RenderMode = data
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
