package server

import (
	"image/color"
	"strings"
	"testing"
)

var red = color.RGBA{R: 255, A: 255}
var green = color.RGBA{G: 255, A: 255}
var blue = color.RGBA{B: 255, A: 255}
var white = color.RGBA{R: 255, G: 255, B: 255, A: 255}

func TestNewImageBuffer(t *testing.T) {
	buf := NewImageBuffer(3, 2)
	if buf.Width != 3 || buf.Height != 2 {
		t.Fatalf("expected 3x2, got %dx%d", buf.Width, buf.Height)
	}
	if len(buf.Pixels) != 6 {
		t.Fatalf("expected 6 cells, got %d", len(buf.Pixels))
	}
	for i, c := range buf.Pixels {
		if c.Char != ' ' {
			t.Errorf("cell %d: expected space, got %q", i, c.Char)
		}
		if c.Fg != nil || c.Bg != nil {
			t.Errorf("cell %d: expected nil colors", i)
		}
	}
}

func TestNewImageBufferZero(t *testing.T) {
	buf := NewImageBuffer(0, 0)
	if buf.Width != 0 || buf.Height != 0 {
		t.Fatalf("expected 0x0")
	}
	if buf.ToString() != "" {
		t.Errorf("expected empty string from 0x0 buffer")
	}
}

func TestSetChar(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.SetChar(1, 0, 'X', red, blue, AttrBold)
	c := buf.Pixels[1]
	if c.Char != 'X' || !colorEq(c.Fg, red) || !colorEq(c.Bg, blue) || c.Attr != AttrBold {
		t.Errorf("SetChar did not write correctly: %+v", c)
	}
	// Out of bounds should be silent.
	buf.SetChar(-1, 0, 'Y', nil, nil, 0)
	buf.SetChar(99, 0, 'Y', nil, nil, 0)
}

func TestFill(t *testing.T) {
	buf := NewImageBuffer(4, 3)
	buf.Fill(1, 1, 2, 2, '#', red, nil, AttrNone)
	// Check filled cells.
	for _, pos := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		c := buf.at(pos[0], pos[1])
		if c.Char != '#' || !colorEq(c.Fg, red) {
			t.Errorf("Fill at (%d,%d): got %+v", pos[0], pos[1], *c)
		}
	}
	// Check unfilled cell.
	if buf.at(0, 0).Char != ' ' {
		t.Error("cell (0,0) should be untouched")
	}
}

func TestWriteString(t *testing.T) {
	buf := NewImageBuffer(5, 1)
	n := buf.WriteString(1, 0, "Hi!", green, nil, AttrNone)
	if n != 3 {
		t.Errorf("expected 3 cols, got %d", n)
	}
	if buf.at(0, 0).Char != ' ' {
		t.Error("col 0 should be space")
	}
	if buf.at(1, 0).Char != 'H' || buf.at(2, 0).Char != 'i' || buf.at(3, 0).Char != '!' {
		t.Error("text not written correctly")
	}
	if buf.at(4, 0).Char != ' ' {
		t.Error("col 4 should be space")
	}
}

func TestWriteStringClipping(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	n := buf.WriteString(1, 0, "Hello", nil, nil, AttrNone)
	// Only 2 chars fit (cols 1 and 2).
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
	if buf.at(2, 0).Char != 'e' {
		t.Errorf("expected 'e' at col 2, got %q", buf.at(2, 0).Char)
	}
}

func TestBlit(t *testing.T) {
	dst := NewImageBuffer(5, 3)
	src := NewImageBuffer(2, 2)
	src.Fill(0, 0, 2, 2, 'X', red, blue, AttrNone)

	dst.Blit(1, 1, src)

	// Check blitted cells.
	for _, pos := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		c := dst.at(pos[0], pos[1])
		if c.Char != 'X' || !colorEq(c.Fg, red) || !colorEq(c.Bg, blue) {
			t.Errorf("Blit at (%d,%d): got %+v", pos[0], pos[1], *c)
		}
	}
	// Check non-blitted.
	if dst.at(0, 0).Char != ' ' {
		t.Error("(0,0) should be untouched")
	}
}

func TestBlitClipping(t *testing.T) {
	dst := NewImageBuffer(3, 3)
	src := NewImageBuffer(2, 2)
	src.Fill(0, 0, 2, 2, 'Y', nil, nil, AttrNone)

	// Blit at (2,2) — only top-left cell of src fits.
	dst.Blit(2, 2, src)
	if dst.at(2, 2).Char != 'Y' {
		t.Error("(2,2) should be Y")
	}
}

func TestRecolorRect(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.WriteString(0, 0, "ABC", red, blue, AttrBold)
	buf.RecolorRect(1, 0, 1, 1, green, white, AttrFaint)

	if buf.at(0, 0).Char != 'A' || !colorEq(buf.at(0, 0).Fg, red) {
		t.Error("col 0 should be unchanged")
	}
	c := buf.at(1, 0)
	if c.Char != 'B' {
		t.Error("char should be preserved")
	}
	if !colorEq(c.Fg, green) || !colorEq(c.Bg, white) || c.Attr != AttrFaint {
		t.Errorf("RecolorRect did not update style: %+v", *c)
	}
}

func TestPaintANSIPlainText(t *testing.T) {
	buf := NewImageBuffer(5, 1)
	buf.PaintANSI(0, 0, 5, 1, "Hello", nil, nil)
	for i, ch := range "Hello" {
		if buf.at(i, 0).Char != ch {
			t.Errorf("col %d: expected %q, got %q", i, ch, buf.at(i, 0).Char)
		}
	}
}

func TestPaintANSIMultiLine(t *testing.T) {
	buf := NewImageBuffer(3, 2)
	buf.PaintANSI(0, 0, 3, 2, "AB\nCD", nil, nil)
	if buf.at(0, 0).Char != 'A' || buf.at(1, 0).Char != 'B' {
		t.Error("row 0 wrong")
	}
	if buf.at(0, 1).Char != 'C' || buf.at(1, 1).Char != 'D' {
		t.Error("row 1 wrong")
	}
	// Remaining cell on row 0 should be filled with space.
	if buf.at(2, 0).Char != ' ' {
		t.Errorf("expected space fill at (2,0), got %q", buf.at(2, 0).Char)
	}
}

func TestPaintANSIColors(t *testing.T) {
	buf := NewImageBuffer(5, 1)
	// Red foreground, then text.
	buf.PaintANSI(0, 0, 5, 1, "\x1b[31mHi", nil, nil)
	c := buf.at(0, 0)
	if c.Char != 'H' {
		t.Errorf("expected 'H', got %q", c.Char)
	}
	// Should be ANSI red (index 1).
	if c.Fg == nil {
		t.Fatal("expected non-nil Fg")
	}
	r, g, b, _ := c.Fg.RGBA()
	if r>>8 != 170 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("expected ANSI red, got (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestPaintANSITrueColor(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	// 24-bit foreground: \x1b[38;2;100;200;50m
	buf.PaintANSI(0, 0, 3, 1, "\x1b[38;2;100;200;50mX", nil, nil)
	c := buf.at(0, 0)
	if c.Char != 'X' {
		t.Fatalf("expected 'X', got %q", c.Char)
	}
	r, g, b, _ := c.Fg.RGBA()
	if uint8(r>>8) != 100 || uint8(g>>8) != 200 || uint8(b>>8) != 50 {
		t.Errorf("expected (100,200,50), got (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestPaintANSIBold(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.PaintANSI(0, 0, 3, 1, "\x1b[1mB", nil, nil)
	if buf.at(0, 0).Attr&AttrBold == 0 {
		t.Error("expected bold attribute")
	}
}

func TestPaintANSIReset(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.PaintANSI(0, 0, 3, 1, "\x1b[31mR\x1b[0mN", red, blue)
	// 'R' should have ANSI red fg.
	if buf.at(0, 0).Fg == nil {
		t.Error("expected non-nil fg on 'R'")
	}
	// 'N' should have default fg (red, as passed to PaintANSI).
	if !colorEq(buf.at(1, 0).Fg, red) {
		t.Error("expected default fg after reset")
	}
}

func TestPaintANSIClipping(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.PaintANSI(0, 0, 2, 1, "ABCDEF", nil, nil)
	if buf.at(0, 0).Char != 'A' || buf.at(1, 0).Char != 'B' {
		t.Error("first two chars wrong")
	}
	// Col 2 should be untouched (space from NewImageBuffer).
	if buf.at(2, 0).Char != ' ' {
		t.Errorf("col 2 should be untouched space, got %q", buf.at(2, 0).Char)
	}
}

func TestPaintANSIRowClipping(t *testing.T) {
	buf := NewImageBuffer(3, 1)
	buf.PaintANSI(0, 0, 3, 1, "AB\nCD\nEF", nil, nil)
	// Only the first row should be painted.
	if buf.at(0, 0).Char != 'A' || buf.at(1, 0).Char != 'B' {
		t.Error("row 0 wrong")
	}
}

func TestToStringPlain(t *testing.T) {
	buf := NewImageBuffer(3, 2)
	buf.WriteString(0, 0, "ABC", nil, nil, AttrNone)
	buf.WriteString(0, 1, "DEF", nil, nil, AttrNone)
	s := buf.ToString()
	// Should contain the characters on two lines.
	lines := strings.Split(s, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// Strip ANSI to check chars.
	if cellbufStripANSI(lines[0]) != "ABC" {
		t.Errorf("row 0: expected 'ABC', got %q", cellbufStripANSI(lines[0]))
	}
	if cellbufStripANSI(lines[1]) != "DEF" {
		t.Errorf("row 1: expected 'DEF', got %q", cellbufStripANSI(lines[1]))
	}
}

func TestToStringRLE(t *testing.T) {
	buf := NewImageBuffer(4, 1)
	buf.SetChar(0, 0, 'A', red, nil, AttrNone)
	buf.SetChar(1, 0, 'B', red, nil, AttrNone)
	buf.SetChar(2, 0, 'C', green, nil, AttrNone)
	buf.SetChar(3, 0, 'D', green, nil, AttrNone)

	s := buf.ToString()
	// There should be exactly 3 SGR sequences: reset at row start, one for red, one for green.
	// Plus the final reset.
	count := strings.Count(s, "\x1b[")
	// Row reset + red SGR + green SGR + final reset = 4
	if count != 4 {
		t.Errorf("expected 4 SGR sequences, got %d in %q", count, s)
	}
}

func TestColorEq(t *testing.T) {
	if !colorEq(nil, nil) {
		t.Error("nil == nil should be true")
	}
	if colorEq(nil, red) {
		t.Error("nil != red")
	}
	if colorEq(red, nil) {
		t.Error("red != nil")
	}
	if !colorEq(red, color.RGBA{R: 255, A: 255}) {
		t.Error("same red should be equal")
	}
	if colorEq(red, green) {
		t.Error("red != green")
	}
}

func TestAnsi256Color(t *testing.T) {
	// Index 1 = red.
	c := ansi256Color(1)
	if c != ansi16Table[1] {
		t.Errorf("index 1: expected %v, got %v", ansi16Table[1], c)
	}
	// Grayscale index 232 = darkest gray.
	c = ansi256Color(232)
	if c.R != 8 || c.G != 8 || c.B != 8 {
		t.Errorf("index 232: expected (8,8,8), got (%d,%d,%d)", c.R, c.G, c.B)
	}
}

// cellbufStripANSI removes all ANSI escape sequences from a string.
func cellbufStripANSI(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isCSIFinal(s[j]) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}
