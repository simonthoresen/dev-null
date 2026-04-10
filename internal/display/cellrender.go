// Package display provides pluggable display backends (terminal TUI vs Ebitengine GUI)
// for rendering ImageBuffer-based UI to different output targets.
package display

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/bitmapfont/v4"

	"dev-null/internal/datadir"
	"dev-null/internal/render"
)

// CellW and CellH are the pixel dimensions of a single terminal cell.
// These are set by InitFont based on the loaded font's metrics.
var (
	CellW = 10
	CellH = 20
)

// DefaultFontFace returns the built-in bitmap font face for terminal rendering.
// Used by the client's remote ANSI renderer where pixel-perfect retro look is desired.
func DefaultFontFace() text.Face {
	return text.NewGoXFace(bitmapfont.Face)
}

// GUIFontFace loads a monospace TTF font suitable for the GUI backend.
// Search order: bundled fonts dir (install dir or dev dist/fonts/), then
// Windows system fonts, then bitmap fallback.
// Cell dimensions (CellW, CellH) are updated to match the loaded font.
func GUIFontFace(size float64) text.Face {
	if size <= 0 {
		size = 16
	}

	// Build candidate paths: bundled font takes priority over system fonts.
	var candidates []string
	installDir := datadir.InstallDir()
	candidates = append(candidates, filepath.Join(installDir, "fonts", "CascadiaMono.ttf"))
	// Dev fallback: when running via "go run" from repo root, fonts live in dist/fonts/.
	if installDir == "." {
		candidates = append(candidates, filepath.Join("dist", "fonts", "CascadiaMono.ttf"))
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\Windows\Fonts\CascadiaMono.ttf`,
			`C:\Windows\Fonts\consola.ttf`,
		)
	}

	for _, path := range candidates {
		if face := loadTTF(path, size); face != nil {
			updateCellSize(face)
			return face
		}
	}

	// Fallback: bitmap font with default cell size.
	CellW = 10
	CellH = 20
	return DefaultFontFace()
}

// loadTTF loads a TrueType font from the given path at the specified size.
func loadTTF(path string, size float64) text.Face {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	src, err := text.NewGoTextFaceSource(f)
	if err != nil {
		return nil
	}

	return &text.GoTextFace{
		Source: src,
		Size:   size,
	}
}

// updateCellSize measures the font's metrics and sets CellW/CellH accordingly.
func updateCellSize(face text.Face) {
	m := face.Metrics()

	// Measure advance width of several characters including box-drawing
	// glyphs that tend to be wider than Latin letters.
	maxW := 0.0
	for _, ch := range "M║═█╔╗╚╝─│W@" {
		if w := text.Advance(string(ch), face); w > maxW {
			maxW = w
		}
	}
	CellW = int(math.Ceil(maxW))
	CellH = int(math.Ceil(m.HAscent + m.HDescent + m.HLineGap))
	if CellW < 1 {
		CellW = 1
	}
	if CellH < 1 {
		CellH = 1
	}
}

// --- Shared DPI / layout logic (used by EbitenBackend and client.Game) ---

var dpiScale float64

// DPIScale returns the monitor's device scale factor (cached on first call).
func DPIScale() float64 {
	if dpiScale == 0 {
		dpiScale = ebiten.Monitor().DeviceScaleFactor()
		if dpiScale < 1 {
			dpiScale = 1
		}
	}
	return dpiScale
}

// InitGUIFont loads the DPI-scaled GUI font and updates CellW/CellH.
// Call once at startup before creating any Game.
func InitGUIFont() text.Face {
	return GUIFontFace(16 * DPIScale())
}

// WindowCols returns the number of cell columns for a logical window width.
func WindowCols(logicalWidth int) int {
	cols := int(float64(logicalWidth)*DPIScale()) / CellW
	if cols < 1 {
		return 1
	}
	return cols
}

// WindowRows returns the number of cell rows for a logical window height.
func WindowRows(logicalHeight int) int {
	rows := int(float64(logicalHeight)*DPIScale()) / CellH
	if rows < 1 {
		return 1
	}
	return rows
}

// GameLayout returns the game screen size in physical pixels for LayoutF.
func GameLayout(outsideWidth, outsideHeight float64) (float64, float64) {
	s := DPIScale()
	return outsideWidth * s, outsideHeight * s
}

// GameLayoutInt returns the game screen size in physical pixels for Layout.
func GameLayoutInt(outsideWidth, outsideHeight int) (int, int) {
	s := DPIScale()
	return int(float64(outsideWidth) * s), int(float64(outsideHeight) * s)
}

// sharedPixel is a 1x1 white image reused for all background fills.
// Colored via ColorScale to avoid per-cell image allocation.
var sharedPixel *ebiten.Image

func init() {
	sharedPixel = ebiten.NewImage(1, 1)
	sharedPixel.Fill(color.White)
}

// blockCharQuadrantMask returns a 4-bit quadrant fill mask for Unicode block
// element characters (U+2580–U+259F).
// Bit layout: bit3=upper-left, bit2=upper-right, bit1=lower-left, bit0=lower-right.
// Returns -1 for characters not rendered as pixel fills.
func blockCharQuadrantMask(r rune) int {
	switch r {
	case 0x2580:
		return 0b1100 // ▀ upper half
	case 0x2584:
		return 0b0011 // ▄ lower half
	case 0x2588:
		return 0b1111 // █ full block
	case 0x258C:
		return 0b1010 // ▌ left half
	case 0x2590:
		return 0b0101 // ▐ right half
	case 0x2596:
		return 0b0010 // ▖ lower-left
	case 0x2597:
		return 0b0001 // ▗ lower-right
	case 0x2598:
		return 0b1000 // ▘ upper-left
	case 0x2599:
		return 0b1011 // ▙ upper-left + lower-left + lower-right
	case 0x259A:
		return 0b1001 // ▚ upper-left + lower-right
	case 0x259B:
		return 0b1110 // ▛ upper-left + upper-right + lower-left
	case 0x259C:
		return 0b1101 // ▜ upper-left + upper-right + lower-right
	case 0x259D:
		return 0b0100 // ▝ upper-right
	case 0x259E:
		return 0b0110 // ▞ upper-right + lower-left
	case 0x259F:
		return 0b0111 // ▟ upper-right + lower-left + lower-right
	}
	return -1
}

// drawBlockChar renders a block element character as Ebitengine pixel fills.
// mask is the 4-bit quadrant mask from blockCharQuadrantMask.
func drawBlockChar(screen *ebiten.Image, px, py, cw, ch int, mask int, fg color.RGBA) {
	hw := cw / 2
	hh := ch / 2
	type quad struct{ x, y, w, h int }
	quads := [4]quad{
		{px, py, hw, hh},                    // bit3: upper-left
		{px + hw, py, cw - hw, hh},          // bit2: upper-right
		{px, py + hh, hw, ch - hh},          // bit1: lower-left
		{px + hw, py + hh, cw - hw, ch - hh}, // bit0: lower-right
	}
	for i, q := range quads {
		if mask&(1<<(3-i)) != 0 {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(float64(q.w), float64(q.h))
			op.GeoM.Translate(float64(q.x), float64(q.y))
			op.ColorScale.ScaleWithColor(fg)
			screen.DrawImage(sharedPixel, op)
		}
	}
}

// DrawOptions configures optional behavior for DrawImageBuffer.
type DrawOptions struct {
	// SpriteFunc returns a sprite image for a character, or nil to render as text.
	// Used by the client to render charmap PUA codepoints as sprites.
	SpriteFunc func(char rune, cx, cy int) *ebiten.Image

	// SkipFunc, if set, is called for each cell. If it returns true, the cell
	// is not drawn (treated as transparent). Used by the canvas compositing
	// path: placeholder cells are skipped so the canvas image shows through.
	SkipFunc func(char rune, cx, cy int) bool
}

// DrawImageBuffer renders an ImageBuffer to an Ebitengine screen image.
// Each cell is drawn as a colored background rectangle, then foreground text.
// If opts.SpriteFunc is set, it is called for each cell — if it returns a
// non-nil image, that image is drawn instead of the text glyph.
func DrawImageBuffer(screen *ebiten.Image, buf *render.ImageBuffer, fontFace text.Face, opts *DrawOptions) {
	for cy := 0; cy < buf.Height; cy++ {
		for cx := 0; cx < buf.Width; cx++ {
			p := &buf.Pixels[cy*buf.Width+cx]

			// Skip transparent cells (canvas placeholders).
			if opts != nil && opts.SkipFunc != nil && opts.SkipFunc(p.Char, cx, cy) {
				continue
			}

			px := cx * CellW
			py := cy * CellH

			// Background: draw a scaled 1x1 white pixel with color tint.
			if p.Bg != nil {
				r, g, b, _ := p.Bg.RGBA()
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Scale(float64(CellW), float64(CellH))
				op.GeoM.Translate(float64(px), float64(py))
				op.ColorScale.ScaleWithColor(color.RGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255,
				})
				screen.DrawImage(sharedPixel, op)
			}

			// Sprite override: if the caller provides a sprite for this cell, draw it.
			if opts != nil && opts.SpriteFunc != nil {
				if sprite := opts.SpriteFunc(p.Char, cx, cy); sprite != nil {
					sop := &ebiten.DrawImageOptions{}
					sw := float64(CellW) / float64(sprite.Bounds().Dx())
					sh := float64(CellH) / float64(sprite.Bounds().Dy())
					sop.GeoM.Scale(sw, sh)
					sop.GeoM.Translate(float64(px), float64(py))
					screen.DrawImage(sprite, sop)
					continue
				}
			}

			// Foreground: block element chars are pixel fills; everything else is font.
			if p.Char != ' ' && p.Char != 0 {
				fg := color.RGBA{R: 204, G: 204, B: 204, A: 255}
				if p.Fg != nil {
					r, g, b, _ := p.Fg.RGBA()
					fg = color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
				}
				// Block element characters (U+2580–U+259F) are rendered as
				// direct pixel fills so they work regardless of whether the
				// loaded font contains those glyphs (many don't).
				if mask := blockCharQuadrantMask(p.Char); mask >= 0 {
					drawBlockChar(screen, px, py, CellW, CellH, mask, fg)
				} else {
					// Clip to cell bounds so box-drawing glyphs don't bleed.
					dst := screen.SubImage(image.Rect(px, py, px+CellW, py+CellH)).(*ebiten.Image)
					dop := &text.DrawOptions{}
					dop.GeoM.Translate(float64(px), float64(py))
					dop.ColorScale.ScaleWithColor(fg)
					text.Draw(dst, string(p.Char), fontFace, dop)
				}
			}
		}
	}
}
