package engine

import (
	"bytes"
	"image"
	"image/png"
	"math"

	"github.com/dop251/goja"
	"github.com/fogleman/gg"
)

// jsCanvas wraps a fogleman/gg context and exposes Canvas2D-like methods to JS.
type jsCanvas struct {
	dc     *gg.Context
	width  int
	height int
}

// newJSCanvas creates a new headless canvas with the given pixel dimensions.
func newJSCanvas(width, height int) *jsCanvas {
	dc := gg.NewContext(width, height)
	return &jsCanvas{dc: dc, width: width, height: height}
}

// toPNG renders the canvas to a PNG byte slice.
func (c *jsCanvas) toPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// toRGBA returns the canvas as an *image.RGBA.
func (c *jsCanvas) toRGBA() *image.RGBA {
	img := c.dc.Image()
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	// Convert if necessary.
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}

// toJSObject creates a goja-compatible object with Canvas2D methods.
func (c *jsCanvas) toJSObject(vm *goja.Runtime) map[string]any {
	return map[string]any{
		"width":  c.width,
		"height": c.height,

		// ── State ──
		"save":    func() { c.dc.Push() },
		"restore": func() { c.dc.Pop() },

		// ── Transforms ──
		"translate": func(x, y float64) { c.dc.Translate(x, y) },
		"rotate":    func(angle float64) { c.dc.Rotate(angle) },
		"scale":     func(sx, sy float64) { c.dc.Scale(sx, sy) },

		// ── Style ──
		"setFillStyle": func(color string) {
			c.dc.SetHexColor(color)
		},
		"setStrokeStyle": func(color string) {
			c.dc.SetHexColor(color)
		},
		"setLineWidth": func(w float64) {
			c.dc.SetLineWidth(w)
		},
		"setGlobalAlpha": func(a float64) {
			// fogleman/gg doesn't have global alpha; approximate by adjusting color.
			// This is a known limitation — document it.
			_ = a
		},
		"setFont": func(size float64) {
			// Use the default font at the given size.
			// fogleman/gg loads system fonts; for embedded use, we skip font loading.
			_ = size
		},

		// ── Rectangles ──
		"fillRect": func(x, y, w, h float64) {
			c.dc.DrawRectangle(x, y, w, h)
			c.dc.Fill()
		},
		"strokeRect": func(x, y, w, h float64) {
			c.dc.DrawRectangle(x, y, w, h)
			c.dc.Stroke()
		},
		"clearRect": func(x, y, w, h float64) {
			c.dc.SetRGBA(0, 0, 0, 0)
			c.dc.DrawRectangle(x, y, w, h)
			c.dc.Fill()
		},

		// ── Path ──
		"beginPath": func() { c.dc.ClearPath() },
		"closePath": func() { c.dc.ClosePath() },
		"moveTo":    func(x, y float64) { c.dc.MoveTo(x, y) },
		"lineTo":    func(x, y float64) { c.dc.LineTo(x, y) },
		"arc": func(x, y, radius, startAngle, endAngle float64) {
			// Canvas2D arc goes counterclockwise when anticlockwise=true.
			// fogleman/gg DrawArc goes from angle1 to angle2 counterclockwise.
			c.dc.DrawArc(x, y, radius, startAngle, endAngle)
		},
		"quadraticCurveTo": func(cpx, cpy, x, y float64) {
			c.dc.QuadraticTo(cpx, cpy, x, y)
		},
		"bezierCurveTo": func(cp1x, cp1y, cp2x, cp2y, x, y float64) {
			c.dc.CubicTo(cp1x, cp1y, cp2x, cp2y, x, y)
		},
		"fill":   func() { c.dc.Fill() },
		"stroke": func() { c.dc.Stroke() },

		// ── Circles (convenience, not standard Canvas2D but useful) ──
		"fillCircle": func(x, y, r float64) {
			c.dc.DrawCircle(x, y, r)
			c.dc.Fill()
		},
		"strokeCircle": func(x, y, r float64) {
			c.dc.DrawCircle(x, y, r)
			c.dc.Stroke()
		},

		// ── Text ──
		"fillText": func(text string, x, y float64) {
			c.dc.DrawString(text, x, y)
		},

		// ── Lines ──
		"drawLine": func(x1, y1, x2, y2 float64) {
			c.dc.DrawLine(x1, y1, x2, y2)
			c.dc.Stroke()
		},

		// ── Ellipse ──
		"fillEllipse": func(x, y, rx, ry float64) {
			c.dc.DrawEllipse(x, y, rx, ry)
			c.dc.Fill()
		},
		"strokeEllipse": func(x, y, rx, ry float64) {
			c.dc.DrawEllipse(x, y, rx, ry)
			c.dc.Stroke()
		},

		// ── Pixel manipulation ──
		"setPixel": func(x, y int, color string) {
			c.dc.SetHexColor(color)
			c.dc.SetPixel(x, y)
		},

		// ── Math constants (convenience) ──
		"PI":  math.Pi,
		"TAU": 2 * math.Pi,
	}
}
