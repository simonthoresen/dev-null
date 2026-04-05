package render

import (
	"image"
	"image/color"
	"testing"
)

func TestQuadrantRunesTable(t *testing.T) {
	// Verify all 16 entries are distinct non-zero runes (except index 0 = space).
	seen := make(map[rune]bool)
	for i, r := range quadrantRunes {
		if seen[r] {
			t.Errorf("duplicate rune at index %d: %c (U+%04X)", i, r, r)
		}
		seen[r] = true
	}
	if quadrantRunes[0] != ' ' {
		t.Errorf("index 0 should be space, got %c", quadrantRunes[0])
	}
	if quadrantRunes[15] != '█' {
		t.Errorf("index 15 should be full block, got %c", quadrantRunes[15])
	}
}

func TestLuminance(t *testing.T) {
	if l := luminance(255, 255, 255); l != 255 {
		t.Errorf("white luminance = %d, want 255", l)
	}
	if l := luminance(0, 0, 0); l != 0 {
		t.Errorf("black luminance = %d, want 0", l)
	}
	// Green should be brighter than red, red brighter than blue.
	lr := luminance(255, 0, 0)
	lg := luminance(0, 255, 0)
	lb := luminance(0, 0, 255)
	if lg <= lr || lr <= lb {
		t.Errorf("expected green > red > blue luminance, got G=%d R=%d B=%d", lg, lr, lb)
	}
}

// makeImage creates a 2-pixel-wide, 2-pixel-tall RGBA image from 4 colors.
func makeImage(ul, ur, ll, lr color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, ul)
	img.SetRGBA(1, 0, ur)
	img.SetRGBA(0, 1, ll)
	img.SetRGBA(1, 1, lr)
	return img
}

func TestImageToQuadrants_AllBlack(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	img := makeImage(black, black, black, black)
	buf := NewImageBuffer(1, 1)
	ImageToQuadrants(img, buf, 0, 0, 1, 1)
	// All same color → could be space or full block (both valid since fg==bg).
	p := buf.Pixels[0]
	if p.Char != ' ' && p.Char != '█' {
		t.Errorf("all-black: expected space or full block, got %c (U+%04X)", p.Char, p.Char)
	}
}

func TestImageToQuadrants_AllWhite(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	img := makeImage(white, white, white, white)
	buf := NewImageBuffer(1, 1)
	ImageToQuadrants(img, buf, 0, 0, 1, 1)
	p := buf.Pixels[0]
	if p.Char != ' ' && p.Char != '█' {
		t.Errorf("all-white: expected space or full block, got %c (U+%04X)", p.Char, p.Char)
	}
}

func TestImageToQuadrants_UpperLeft(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	img := makeImage(white, black, black, black)
	buf := NewImageBuffer(1, 1)
	ImageToQuadrants(img, buf, 0, 0, 1, 1)
	p := buf.Pixels[0]
	// Upper-left lit: mask=0b1000=8 → ▘, or inverted mask=0b0111=7 → ▟ (depends on luminance swap).
	if p.Char != '▘' && p.Char != '▟' {
		t.Errorf("upper-left white: expected ▘ or ▟, got %c (U+%04X)", p.Char, p.Char)
	}
}

func TestImageToQuadrants_LowerHalf(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	img := makeImage(black, black, white, white)
	buf := NewImageBuffer(1, 1)
	ImageToQuadrants(img, buf, 0, 0, 1, 1)
	p := buf.Pixels[0]
	// Lower half lit: ▄ (mask 0011) or ▀ (inverted, mask 1100).
	if p.Char != '▄' && p.Char != '▀' {
		t.Errorf("lower-half white: expected ▄ or ▀, got %c (U+%04X)", p.Char, p.Char)
	}
}

func TestImageToQuadrants_MultiCell(t *testing.T) {
	// 4x4 image → 2x2 cells.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// Fill top-left 2x2 block red, rest blue.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if x < 2 && y < 2 {
				img.SetRGBA(x, y, red)
			} else {
				img.SetRGBA(x, y, blue)
			}
		}
	}
	buf := NewImageBuffer(2, 2)
	ImageToQuadrants(img, buf, 0, 0, 2, 2)
	// Cell (0,0) should be all-red (uniform), cell (1,1) all-blue (uniform).
	p00 := buf.Pixels[0]
	p11 := buf.Pixels[3]
	// Uniform cells: space or full block.
	if p00.Char != ' ' && p00.Char != '█' {
		t.Errorf("cell (0,0) all-red: expected uniform, got %c", p00.Char)
	}
	if p11.Char != ' ' && p11.Char != '█' {
		t.Errorf("cell (1,1) all-blue: expected uniform, got %c", p11.Char)
	}
}

func TestImageToQuadrants_FgDarkerThanBg(t *testing.T) {
	// Verify that the fg color is always the darker group (filled positions = dark pixels).
	// This keeps ANSI-stripped golden output readable: edge chars like ▗ appear for
	// sparse dark marks rather than their mostly-filled inverses.
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	img := makeImage(white, black, black, black)
	buf := NewImageBuffer(1, 1)
	ImageToQuadrants(img, buf, 0, 0, 1, 1)
	p := buf.Pixels[0]
	if p.Fg == nil {
		t.Fatal("fg should not be nil")
	}
	if p.Bg == nil {
		t.Fatal("bg should not be nil")
	}
	fr, fg, fb, _ := p.Fg.RGBA()
	br, bg, bb, _ := p.Bg.RGBA()
	fgLum := (19595*fr + 38470*fg + 7471*fb + 1<<15) >> 16
	bgLum := (19595*br + 38470*bg + 7471*bb + 1<<15) >> 16
	if fgLum > bgLum {
		t.Errorf("fg should be darker than bg: fg lum=%d, bg lum=%d", fgLum, bgLum)
	}
}

func TestImageToQuadrants_OddSize(t *testing.T) {
	// Image smaller than expected (3x3 for 2x2 cells = 4x4 expected).
	// Should not panic; pixels outside bounds treated as black.
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	buf := NewImageBuffer(2, 2)
	ImageToQuadrants(img, buf, 0, 0, 2, 2)
	// Should not panic — that's the main assertion.
}
