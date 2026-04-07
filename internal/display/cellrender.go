// Package display provides pluggable display backends (terminal TUI vs Ebitengine GUI)
// for rendering ImageBuffer-based UI to different output targets.
package display

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/bitmapfont/v4"

	"dev-null/internal/render"
)

// CellW and CellH are the pixel dimensions of a single terminal cell.
const (
	CellW = 10
	CellH = 20
)

// DefaultFontFace returns the built-in bitmap font face for terminal rendering.
func DefaultFontFace() text.Face {
	return text.NewGoXFace(bitmapfont.Face)
}

// DrawImageBuffer renders an ImageBuffer to an Ebitengine screen image.
// Each cell is drawn as a colored background rectangle, then foreground text.
func DrawImageBuffer(screen *ebiten.Image, buf *render.ImageBuffer, fontFace text.Face) {
	for cy := 0; cy < buf.Height; cy++ {
		for cx := 0; cx < buf.Width; cx++ {
			p := &buf.Pixels[cy*buf.Width+cx]
			px := cx * CellW
			py := cy * CellH

			// Background.
			if p.Bg != nil {
				r, gg, b, _ := p.Bg.RGBA()
				bgImg := ebiten.NewImage(CellW, CellH)
				bgImg.Fill(color.RGBA{R: uint8(r >> 8), G: uint8(gg >> 8), B: uint8(b >> 8), A: 255})
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Translate(float64(px), float64(py))
				screen.DrawImage(bgImg, op)
			}

			// Foreground text.
			if p.Char != ' ' && p.Char != 0 {
				fg := color.RGBA{R: 204, G: 204, B: 204, A: 255}
				if p.Fg != nil {
					r, gg, b, _ := p.Fg.RGBA()
					fg = color.RGBA{R: uint8(r >> 8), G: uint8(gg >> 8), B: uint8(b >> 8), A: 255}
				}
				dop := &text.DrawOptions{}
				dop.GeoM.Translate(float64(px), float64(py))
				dop.ColorScale.ScaleWithColor(fg)
				text.Draw(screen, string(p.Char), fontFace, dop)
			}
		}
	}
}
