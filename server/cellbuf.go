package server

import (
	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"
)

// ─── Pixel attributes ─────────────────────────────────────────────────────────

// PixelAttr is a bitmask for text attributes (bold, faint, underline, etc.).
type PixelAttr uint8

const (
	AttrNone      PixelAttr = 0
	AttrBold      PixelAttr = 1 << 0
	AttrFaint     PixelAttr = 1 << 1
	AttrItalic    PixelAttr = 1 << 2
	AttrUnderline PixelAttr = 1 << 3
	AttrReverse   PixelAttr = 1 << 4
)

// ─── Pixel ───────────────────────────────────────────────────────────────────

// Pixel represents a single character cell with its styling.
type Pixel struct {
	Char rune        // visible character (' ' = empty, 0 = wide-char continuation)
	Fg   color.Color // nil = default terminal foreground
	Bg   color.Color // nil = default terminal background
	Attr PixelAttr
}

// ─── ImageBuffer ──────────────────────────────────────────────────────────────

// ImageBuffer is a 2D grid of styled character cells. Components write into it
// directly, and ToString() serializes it to an ANSI string at the end.
type ImageBuffer struct {
	Width, Height int
	Pixels         []Pixel // row-major: Pixels[y*Width + x]
}

// NewImageBuffer creates a buffer filled with spaces and nil colors.
func NewImageBuffer(w, h int) *ImageBuffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	cells := make([]Pixel, w*h)
	for i := range cells {
		cells[i].Char = ' '
	}
	return &ImageBuffer{Width: w, Height: h, Pixels: cells}
}

// inBounds returns true if (x, y) is within the buffer.
func (b *ImageBuffer) inBounds(x, y int) bool {
	return x >= 0 && x < b.Width && y >= 0 && y < b.Height
}

// at returns a pointer to the cell at (x, y), or nil if out of bounds.
func (b *ImageBuffer) at(x, y int) *Pixel {
	if !b.inBounds(x, y) {
		return nil
	}
	return &b.Pixels[y*b.Width+x]
}

// SetChar sets a single cell at (x, y). Out-of-bounds writes are silently ignored.
func (b *ImageBuffer) SetChar(x, y int, ch rune, fg, bg color.Color, attr PixelAttr) {
	if c := b.at(x, y); c != nil {
		c.Char = ch
		c.Fg = fg
		c.Bg = bg
		c.Attr = attr
	}
}

// Fill fills a rectangle with the given character and style.
func (b *ImageBuffer) Fill(x, y, w, h int, ch rune, fg, bg color.Color, attr PixelAttr) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			if c := b.at(col, row); c != nil {
				c.Char = ch
				c.Fg = fg
				c.Bg = bg
				c.Attr = attr
			}
		}
	}
}

// WriteString writes plain text (no ANSI codes) at (x, y) with the given style.
// Stops at the right edge of the buffer or end of string. Returns columns consumed.
func (b *ImageBuffer) WriteString(x, y int, s string, fg, bg color.Color, attr PixelAttr) int {
	col := x
	for _, r := range s {
		if !b.inBounds(col, y) {
			break
		}
		b.SetChar(col, y, r, fg, bg, attr)
		col++
	}
	return col - x
}

// Blit copies all cells from src onto b at position (x, y).
// Only copies cells that fall within b's bounds.
func (b *ImageBuffer) Blit(x, y int, src *ImageBuffer) {
	for sy := 0; sy < src.Height; sy++ {
		dy := y + sy
		if dy < 0 || dy >= b.Height {
			continue
		}
		for sx := 0; sx < src.Width; sx++ {
			dx := x + sx
			if dx < 0 || dx >= b.Width {
				continue
			}
			b.Pixels[dy*b.Width+dx] = src.Pixels[sy*src.Width+sx]
		}
	}
}

// RecolorRect changes the fg, bg, and attr of cells in the given rectangle
// without changing the character. Useful for drop shadows.
func (b *ImageBuffer) RecolorRect(x, y, w, h int, fg, bg color.Color, attr PixelAttr) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			if c := b.at(col, row); c != nil {
				c.Fg = fg
				c.Bg = bg
				c.Attr = attr
			}
		}
	}
}

// blitShadow renders an L-shaped drop shadow around a box in the buffer.
// Right strip: 1 cell wide. Bottom strip: boxW cells wide, offset by 1.
func blitShadow(buf *ImageBuffer, boxCol, boxRow, boxW, boxH int, shadowFg, shadowBg color.Color) {
	// Right strip (skip first row to offset).
	for dy := 1; dy < boxH; dy++ {
		r := boxRow + dy
		c := boxCol + boxW
		if cell := buf.at(c, r); cell != nil {
			cell.Fg = shadowFg
			cell.Bg = shadowBg
			cell.Attr = AttrNone
		}
	}
	// Bottom strip (skip first column to offset).
	bottomRow := boxRow + boxH
	for dx := 1; dx <= boxW; dx++ {
		c := boxCol + dx
		if cell := buf.at(c, bottomRow); cell != nil {
			cell.Fg = shadowFg
			cell.Bg = shadowBg
			cell.Attr = AttrNone
		}
	}
}

// ─── ANSI Parsing ────────────────────────────────────────────────────────────

// PaintANSI parses a string containing ANSI escape codes and paints the
// characters into the buffer at (x, y), clipped to (w, h). This is used for
// game output and textinput.Model.View() output.
func (b *ImageBuffer) PaintANSI(x, y, w, h int, s string, defaultFg, defaultBg color.Color) {
	fg := defaultFg
	bg := defaultBg
	var attr PixelAttr

	col := 0 // column offset within the w×h region
	row := 0 // row offset within the w×h region

	i := 0
	for i < len(s) {
		if row >= h {
			break
		}

		// Newline: advance to next row.
		if s[i] == '\n' {
			// Fill remaining columns on this row with spaces.
			for col < w {
				b.SetChar(x+col, y+row, ' ', defaultFg, defaultBg, AttrNone)
				col++
			}
			row++
			col = 0
			i++
			continue
		}

		// CSI escape sequence: \x1b[ ... final_byte
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isCSIFinal(s[j]) {
				j++
			}
			if j < len(s) {
				j++ // include final byte
			}
			seq := s[i:j]
			if len(seq) >= 3 && seq[len(seq)-1] == 'm' {
				// SGR sequence: parse color/attribute parameters.
				parseSGR(seq, &fg, &bg, &attr, defaultFg, defaultBg)
			}
			// Skip non-SGR sequences (cursor movement, etc.).
			i = j
			continue
		}

		// Visible character.
		r, size := utf8.DecodeRuneInString(s[i:])
		if col < w {
			b.SetChar(x+col, y+row, r, fg, bg, attr)
			col++
		}
		i += size
	}
}

// isCSIFinal returns true if b is a CSI final byte (0x40–0x7E).
func isCSIFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

// parseSGR parses an SGR escape sequence ("\x1b[...m") and updates the
// running fg, bg, and attr state.
func parseSGR(seq string, fg, bg *color.Color, attr *PixelAttr, defaultFg, defaultBg color.Color) {
	// Strip "\x1b[" prefix and "m" suffix.
	inner := seq[2 : len(seq)-1]
	if inner == "" {
		// "\x1b[m" = reset.
		*fg = defaultFg
		*bg = defaultBg
		*attr = AttrNone
		return
	}

	params := strings.Split(inner, ";")
	for i := 0; i < len(params); i++ {
		p := parseIntParam(params[i])
		switch {
		case p == 0:
			*fg = defaultFg
			*bg = defaultBg
			*attr = AttrNone
		case p == 1:
			*attr |= AttrBold
		case p == 2:
			*attr |= AttrFaint
		case p == 3:
			*attr |= AttrItalic
		case p == 4:
			*attr |= AttrUnderline
		case p == 7:
			*attr |= AttrReverse
		case p == 22:
			*attr &^= AttrBold | AttrFaint
		case p == 23:
			*attr &^= AttrItalic
		case p == 24:
			*attr &^= AttrUnderline
		case p == 27:
			*attr &^= AttrReverse

		// Standard foreground colors (30–37).
		case p >= 30 && p <= 37:
			*fg = ansi16Color(p - 30)
		// Bright foreground colors (90–97).
		case p >= 90 && p <= 97:
			*fg = ansi16Color(p - 90 + 8)
		// Default foreground.
		case p == 39:
			*fg = defaultFg

		// Standard background colors (40–47).
		case p >= 40 && p <= 47:
			*bg = ansi16Color(p - 40)
		// Bright background colors (100–107).
		case p >= 100 && p <= 107:
			*bg = ansi16Color(p - 100 + 8)
		// Default background.
		case p == 49:
			*bg = defaultBg

		// Extended foreground: 38;5;N or 38;2;R;G;B
		case p == 38:
			if i+1 < len(params) {
				mode := parseIntParam(params[i+1])
				if mode == 5 && i+2 < len(params) {
					*fg = ansi256Color(parseIntParam(params[i+2]))
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					r := parseIntParam(params[i+2])
					g := parseIntParam(params[i+3])
					b := parseIntParam(params[i+4])
					*fg = color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
					i += 4
				}
			}

		// Extended background: 48;5;N or 48;2;R;G;B
		case p == 48:
			if i+1 < len(params) {
				mode := parseIntParam(params[i+1])
				if mode == 5 && i+2 < len(params) {
					*bg = ansi256Color(parseIntParam(params[i+2]))
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					r := parseIntParam(params[i+2])
					g := parseIntParam(params[i+3])
					b := parseIntParam(params[i+4])
					*bg = color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
					i += 4
				}
			}
		}
	}
}

// parseIntParam parses a decimal integer from an SGR parameter string.
func parseIntParam(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// ─── ANSI Color Tables ───────────────────────────────────────────────────────

// ansi16Color returns the color.RGBA for standard ANSI colors 0–15.
func ansi16Color(idx int) color.RGBA {
	if idx < 0 || idx > 15 {
		return color.RGBA{A: 255}
	}
	return ansi16Table[idx]
}

var ansi16Table = [16]color.RGBA{
	{0, 0, 0, 255},       // 0: black
	{170, 0, 0, 255},     // 1: red
	{0, 170, 0, 255},     // 2: green
	{170, 85, 0, 255},    // 3: yellow/brown
	{0, 0, 170, 255},     // 4: blue
	{170, 0, 170, 255},   // 5: magenta
	{0, 170, 170, 255},   // 6: cyan
	{170, 170, 170, 255}, // 7: white
	{85, 85, 85, 255},    // 8: bright black
	{255, 85, 85, 255},   // 9: bright red
	{85, 255, 85, 255},   // 10: bright green
	{255, 255, 85, 255},  // 11: bright yellow
	{85, 85, 255, 255},   // 12: bright blue
	{255, 85, 255, 255},  // 13: bright magenta
	{85, 255, 255, 255},  // 14: bright cyan
	{255, 255, 255, 255}, // 15: bright white
}

// ansi256Color returns the color.RGBA for an ANSI 256-color index.
func ansi256Color(idx int) color.RGBA {
	if idx < 0 || idx > 255 {
		return color.RGBA{A: 255}
	}
	if idx < 16 {
		return ansi16Table[idx]
	}
	if idx < 232 {
		// 6×6×6 color cube: indices 16–231.
		idx -= 16
		b := idx % 6
		idx /= 6
		g := idx % 6
		r := idx / 6
		return color.RGBA{
			R: uint8(cubeVal(r)),
			G: uint8(cubeVal(g)),
			B: uint8(cubeVal(b)),
			A: 255,
		}
	}
	// Grayscale ramp: indices 232–255.
	v := uint8(8 + (idx-232)*10)
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

func cubeVal(i int) int {
	if i == 0 {
		return 0
	}
	return 55 + i*40
}

// ─── Serialization ───────────────────────────────────────────────────────────

// ToString serializes the buffer to a string with ANSI escape codes.
// Uses RLE-style optimization: only emits SGR codes when the style changes
// from the previous cell (or at the start of each row).
func (b *ImageBuffer) ToString() string {
	if b.Width == 0 || b.Height == 0 {
		return ""
	}

	// Pre-allocate a generous builder.
	var sb strings.Builder
	sb.Grow(b.Width * b.Height * 3) // rough estimate

	var curFg, curBg color.Color
	var curAttr PixelAttr
	first := true

	for y := 0; y < b.Height; y++ {
		if y > 0 {
			sb.WriteByte('\n')
		}
		// Reset at each row start for clean state.
		sb.WriteString("\x1b[0m")
		curFg = nil
		curBg = nil
		curAttr = AttrNone
		first = true

		for x := 0; x < b.Width; x++ {
			c := &b.Pixels[y*b.Width+x]
			if c.Char == 0 {
				continue // wide-char continuation
			}

			// Emit SGR if style changed.
			if first || !colorEq(c.Fg, curFg) || !colorEq(c.Bg, curBg) || c.Attr != curAttr {
				sgr := buildSGR(c.Fg, c.Bg, c.Attr)
				if sgr != "" {
					sb.WriteString(sgr)
				}
				curFg = c.Fg
				curBg = c.Bg
				curAttr = c.Attr
				first = false
			}

			sb.WriteRune(c.Char)
		}
	}

	// Final reset so the terminal returns to default.
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// colorEq compares two color.Color values for equality.
// Two nil colors are equal; a nil and non-nil are not.
func colorEq(a, b color.Color) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// buildSGR builds an SGR escape sequence for the given style.
// Returns "" if all values are default (nil colors, no attrs).
func buildSGR(fg, bg color.Color, attr PixelAttr) string {
	var parts []string

	// Always start with reset to avoid inheriting previous state,
	// then add back what we need.
	parts = append(parts, "0")

	if attr&AttrBold != 0 {
		parts = append(parts, "1")
	}
	if attr&AttrFaint != 0 {
		parts = append(parts, "2")
	}
	if attr&AttrItalic != 0 {
		parts = append(parts, "3")
	}
	if attr&AttrUnderline != 0 {
		parts = append(parts, "4")
	}
	if attr&AttrReverse != 0 {
		parts = append(parts, "7")
	}

	if fg != nil {
		r, g, b, _ := fg.RGBA()
		parts = append(parts, fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8))
	}
	if bg != nil {
		r, g, b, _ := bg.RGBA()
		parts = append(parts, fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8))
	}

	return "\x1b[" + strings.Join(parts, ";") + "m"
}
