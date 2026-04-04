package render

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestBlitShadow(t *testing.T) {
	buf := NewImageBuffer(10, 8)
	// Fill with some visible content.
	buf.Fill(0, 0, 10, 8, '.', nil, nil, AttrNone)

	shadowFg := color.RGBA{R: 85, G: 85, B: 85, A: 255}
	shadowBg := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	// Box at (2,1) size 4x3.
	BlitShadow(buf, 2, 1, 4, 3, shadowFg, shadowBg)

	// Right strip: column 6 (2+4), rows 2-3 (1+1 to 1+3-1).
	for _, dy := range []int{1, 2} {
		r := 1 + dy
		c := 2 + 4
		cell := buf.at(c, r)
		if cell == nil {
			t.Fatalf("expected cell at (%d,%d)", c, r)
		}
		if cell.Bg != shadowBg {
			t.Errorf("right strip (%d,%d): expected shadow bg", c, r)
		}
	}

	// Bottom strip: row 4 (1+3), columns 3-6 (2+1 to 2+4).
	bottomRow := 1 + 3
	for dx := 1; dx <= 4; dx++ {
		c := 2 + dx
		cell := buf.at(c, bottomRow)
		if cell == nil {
			t.Fatalf("expected cell at (%d,%d)", c, bottomRow)
		}
		if cell.Bg != shadowBg {
			t.Errorf("bottom strip (%d,%d): expected shadow bg", c, bottomRow)
		}
	}
}

func TestBlitShadowOutOfBounds(t *testing.T) {
	buf := NewImageBuffer(5, 5)
	// Shadow extends beyond buffer — should not panic.
	BlitShadow(buf, 3, 3, 4, 4, nil, nil)
}

func TestRuneOf(t *testing.T) {
	tests := []struct {
		input string
		want  rune
	}{
		{"A", 'A'},
		{"hello", 'h'},
		{"", ' '},
		{"日本語", '日'},
	}
	for _, tt := range tests {
		got := RuneOf(tt.input)
		if got != tt.want {
			t.Errorf("RuneOf(%q) = %c, want %c", tt.input, got, tt.want)
		}
	}
}

func TestNewImageBufferDimensions(t *testing.T) {
	buf := NewImageBuffer(10, 5)
	if buf.Width != 10 {
		t.Fatalf("expected width 10, got %d", buf.Width)
	}
	if buf.Height != 5 {
		t.Fatalf("expected height 5, got %d", buf.Height)
	}
	if len(buf.Pixels) != 50 {
		t.Fatalf("expected 50 pixels, got %d", len(buf.Pixels))
	}
}

func TestSetCharAndAt(t *testing.T) {
	buf := NewImageBuffer(10, 5)
	fg := color.RGBA{R: 255, A: 255}
	bg := color.RGBA{B: 255, A: 255}
	buf.SetChar(3, 2, 'X', fg, bg, AttrBold)

	cell := buf.at(3, 2)
	if cell == nil {
		t.Fatal("expected cell")
	}
	if cell.Char != 'X' {
		t.Fatalf("expected 'X', got %c", cell.Char)
	}
	if cell.Fg != fg {
		t.Fatal("wrong fg")
	}
	if cell.Bg != bg {
		t.Fatal("wrong bg")
	}
	if cell.Attr != AttrBold {
		t.Fatal("wrong attr")
	}
}

func TestSetCharOutOfBounds(t *testing.T) {
	buf := NewImageBuffer(5, 5)
	// Should not panic.
	buf.SetChar(-1, 0, 'X', nil, nil, 0)
	buf.SetChar(0, -1, 'X', nil, nil, 0)
	buf.SetChar(5, 0, 'X', nil, nil, 0)
	buf.SetChar(0, 5, 'X', nil, nil, 0)
}

func TestWriteStringBasic(t *testing.T) {
	buf := NewImageBuffer(10, 5)
	fg := color.RGBA{G: 255, A: 255}
	buf.WriteString(1, 2, "Hi", fg, nil, AttrNone)

	if buf.at(1, 2).Char != 'H' {
		t.Fatalf("expected 'H', got %c", buf.at(1, 2).Char)
	}
	if buf.at(2, 2).Char != 'i' {
		t.Fatalf("expected 'i', got %c", buf.at(2, 2).Char)
	}
}

func TestFillRect(t *testing.T) {
	buf := NewImageBuffer(10, 5)
	bg := color.RGBA{R: 128, A: 255}
	buf.Fill(2, 1, 3, 2, '#', nil, bg, AttrNone)

	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 3; dx++ {
			cell := buf.at(2+dx, 1+dy)
			if cell.Char != '#' {
				t.Errorf("(%d,%d) expected '#', got %c", 2+dx, 1+dy, cell.Char)
			}
			if cell.Bg != bg {
				t.Errorf("(%d,%d) wrong bg", 2+dx, 1+dy)
			}
		}
	}
}

func TestRecolorRectPreservesChar(t *testing.T) {
	buf := NewImageBuffer(10, 5)
	buf.SetChar(1, 1, 'A', nil, nil, AttrNone)

	newFg := color.RGBA{R: 255, A: 255}
	newBg := color.RGBA{B: 255, A: 255}
	buf.RecolorRect(0, 0, 5, 5, newFg, newBg, AttrBold)

	cell := buf.at(1, 1)
	if cell.Char != 'A' {
		t.Fatal("recolor should not change char")
	}
	if cell.Fg != newFg {
		t.Fatal("recolor should change fg")
	}
	if cell.Bg != newBg {
		t.Fatal("recolor should change bg")
	}
}

func TestToStringRoundTrip(t *testing.T) {
	buf := NewImageBuffer(5, 2)
	buf.WriteString(0, 0, "Hello", color.RGBA{R: 255, A: 255}, nil, AttrNone)
	buf.WriteString(0, 1, "World", nil, color.RGBA{B: 255, A: 255}, AttrNone)

	s := buf.ToString(colorprofile.TrueColor)
	if len(s) == 0 {
		t.Fatal("expected non-empty string")
	}
}
