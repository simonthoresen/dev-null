// Package display provides pluggable display backends (terminal TUI vs Ebitengine GUI)
// for rendering ImageBuffer-based UI to different output targets.
package display

import (
	"image"
	"image/color"
	"math"
	"os"
	"runtime"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/bitmapfont/v4"

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

// GUIFontFace loads a system monospace TTF font suitable for the GUI backend.
// On Windows it tries Cascadia Mono then Consolas; falls back to the bitmap font.
// Cell dimensions (CellW, CellH) are updated to match the loaded font.
func GUIFontFace(size float64) text.Face {
	if size <= 0 {
		size = 16
	}

	if runtime.GOOS == "windows" {
		// Try fonts in preference order.
		for _, path := range []string{
			`C:\Windows\Fonts\CascadiaMono.ttf`,
			`C:\Windows\Fonts\consola.ttf`,
		} {
			if face := loadTTF(path, size); face != nil {
				updateCellSize(face)
				return face
			}
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

// DrawImageBuffer renders an ImageBuffer to an Ebitengine screen image.
// Each cell is drawn as a colored background rectangle, then foreground text.
// Text is clipped to cell bounds via SubImage to prevent box-drawing glyph overflow.
func DrawImageBuffer(screen *ebiten.Image, buf *render.ImageBuffer, fontFace text.Face) {
	for cy := 0; cy < buf.Height; cy++ {
		for cx := 0; cx < buf.Width; cx++ {
			p := &buf.Pixels[cy*buf.Width+cx]
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

			// Foreground text — clip to cell bounds so box-drawing glyphs
			// don't bleed into adjacent cells.
			if p.Char != ' ' && p.Char != 0 {
				fg := color.RGBA{R: 204, G: 204, B: 204, A: 255}
				if p.Fg != nil {
					r, g, b, _ := p.Fg.RGBA()
					fg = color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
				}
				// Block-element characters (█▀▄▌▐▖▗▘▙▚▛▜▝▞▟) don't overflow
				// their cell, so skip the SubImage clip that causes visible
				// seam lines when the TTF glyph doesn't fill the cell exactly.
				dst := screen
				if p.Char < 0x2580 || p.Char > 0x259F {
					dst = screen.SubImage(image.Rect(px, py, px+CellW, py+CellH)).(*ebiten.Image)
				}
				dop := &text.DrawOptions{}
				dop.GeoM.Translate(float64(px), float64(py))
				dop.ColorScale.ScaleWithColor(fg)
				text.Draw(dst, string(p.Char), fontFace, dop)
			}
		}
	}
}
