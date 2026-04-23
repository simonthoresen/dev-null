package engine

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/fogleman/gg"
)

// JSCanvas wraps a fogleman/gg context and exposes Canvas2D-like methods to JS.
//
// Gradients returned from createRadialGradient/createLinearGradient are
// keyed by a per-canvas opaque id string; setFillStyle/setStrokeStyle
// inspect their argument to decide whether it is a color string or a
// gradient handle. The canvas is recreated each frame, so the registry
// does not outlive a render pass.
type JSCanvas struct {
	dc          *gg.Context
	width       int
	height      int
	gradients   map[string]gg.Pattern
	gradCounter int
	raster      *Rasterizer // lazily initialized on first 3D call
}

// NewJSCanvas creates a new headless canvas with the given pixel dimensions.
// scaleY scales the drawing context vertically. Pass 1.0 for no scaling.
// For terminal quadrant rendering the canvas is w*2 × h*4 pixels (h*4 because
// terminal cells are ~1:2 wide:tall, so 4 rows of pixels fill one cell row
// at the same physical size as 2 pixels fill one cell column). With scaleY=1.0
// the game sees w*2 × h*4 logical pixels that are visually square — one logical
// pixel maps to cellWidth/2 × cellWidth/2 on screen regardless of cell aspect.
func NewJSCanvas(width, height int, scaleY float64) *JSCanvas {
	dc := gg.NewContext(width, height)
	logicalH := int(math.Round(float64(height) / scaleY))
	if scaleY != 1.0 {
		dc.Scale(1.0, scaleY)
	}
	return &JSCanvas{dc: dc, width: width, height: logicalH, gradients: map[string]gg.Pattern{}}
}

// parseCanvasHexColor parses a "#rgb", "#rgba", "#rrggbb", or "#rrggbbaa"
// color string into an image/color.RGBA. Returns black on parse error.
func parseCanvasHexColor(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	expand := func(h string) string {
		var b strings.Builder
		for _, r := range h {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		return b.String()
	}
	switch len(s) {
	case 3:
		s = expand(s) + "ff"
	case 4:
		s = expand(s)
	case 6:
		s = s + "ff"
	case 8:
		// already 8
	default:
		return color.RGBA{}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}
	}
	return color.RGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// ToPNG renders the canvas to a PNG byte slice.
func (c *JSCanvas) ToPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToRGBA returns the canvas as an *image.RGBA.
func (c *JSCanvas) ToRGBA() *image.RGBA {
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

// ToJSObject creates a goja-compatible object with Canvas2D methods.
func (c *JSCanvas) ToJSObject(vm *goja.Runtime) map[string]any {
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
		// setFillStyle / setStrokeStyle accept either a hex color string
		// or a gradient handle returned from createRadialGradient /
		// createLinearGradient.
		"setFillStyle": func(arg any) {
			if !c.applyPattern(arg, true) {
				// Fallback: treat non-string/non-gradient as opaque black.
				c.dc.SetRGBA(0, 0, 0, 1)
			}
		},
		"setStrokeStyle": func(arg any) {
			if !c.applyPattern(arg, false) {
				c.dc.SetRGBA(0, 0, 0, 1)
			}
		},

		// ── Gradients ──
		"createRadialGradient": func(x0, y0, r0, x1, y1, r1 float64) map[string]any {
			return c.registerGradient(gg.NewRadialGradient(x0, y0, r0, x1, y1, r1))
		},
		"createLinearGradient": func(x0, y0, x1, y1 float64) map[string]any {
			return c.registerGradient(gg.NewLinearGradient(x0, y0, x1, y1))
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

		// ── 3D triangle rasterizer ──
		// All 3D primitives share a depth buffer that is allocated on the
		// first call and reset by clearDepth(). A fresh canvas starts with
		// no depth buffer, so a game that never calls fillTriangle3D* pays
		// zero memory overhead.
		"fillTriangle3D": func(v0, v1, v2 []float64, colors []string) {
			if len(v0) < 3 || len(v1) < 3 || len(v2) < 3 || len(colors) < 3 {
				return
			}
			c.ensureRaster().FillTriangle(
				v0[0], v0[1], v0[2],
				v1[0], v1[1], v1[2],
				v2[0], v2[1], v2[2],
				parseCanvasHexColor(colors[0]),
				parseCanvasHexColor(colors[1]),
				parseCanvasHexColor(colors[2]),
			)
		},
		"fillTriangle3DFlat": func(v0, v1, v2 []float64, colorHex string) {
			if len(v0) < 3 || len(v1) < 3 || len(v2) < 3 {
				return
			}
			col := parseCanvasHexColor(colorHex)
			c.ensureRaster().FillTriangle(
				v0[0], v0[1], v0[2],
				v1[0], v1[1], v1[2],
				v2[0], v2[1], v2[2],
				col, col, col,
			)
		},
		"fillTriangle3DLit": func(
			v0, v1, v2 []float64,
			n0, n1, n2 []float64,
			lightDir []float64,
			colorHex string,
			ambient float64,
		) {
			if len(v0) < 3 || len(v1) < 3 || len(v2) < 3 {
				return
			}
			if len(n0) < 3 || len(n1) < 3 || len(n2) < 3 || len(lightDir) < 3 {
				return
			}
			base := parseCanvasHexColor(colorHex)
			lx, ly, lz := lightDir[0], lightDir[1], lightDir[2]
			c0 := Lambert(n0[0], n0[1], n0[2], lx, ly, lz, base, ambient)
			c1 := Lambert(n1[0], n1[1], n1[2], lx, ly, lz, base, ambient)
			c2 := Lambert(n2[0], n2[1], n2[2], lx, ly, lz, base, ambient)
			c.ensureRaster().FillTriangle(
				v0[0], v0[1], v0[2],
				v1[0], v1[1], v1[2],
				v2[0], v2[1], v2[2],
				c0, c1, c2,
			)
		},
		"clearDepth": func() {
			if c.raster != nil {
				c.raster.ClearDepth()
			}
		},

		// ── Math constants (convenience) ──
		"PI":  math.Pi,
		"TAU": 2 * math.Pi,
	}
}

// ensureRaster lazily constructs the rasterizer tied to this canvas's RGBA
// backing buffer. Reusing the same image means 2D and 3D ops composite
// naturally.
func (c *JSCanvas) ensureRaster() *Rasterizer {
	if c.raster == nil {
		c.raster = NewRasterizer(c.ToRGBA())
	}
	return c.raster
}

// registerGradient stores a gradient under an opaque id and returns a JS
// handle object exposing addColorStop. The id is hidden in the handle so
// setFillStyle/setStrokeStyle can recover it.
func (c *JSCanvas) registerGradient(grad gg.Pattern) map[string]any {
	c.gradCounter++
	id := fmt.Sprintf("__grad_%d", c.gradCounter)
	c.gradients[id] = grad
	if g, ok := grad.(gg.Gradient); ok {
		return map[string]any{
			"_id": id,
			"addColorStop": func(offset float64, colorHex string) {
				g.AddColorStop(offset, parseCanvasHexColor(colorHex))
			},
		}
	}
	return map[string]any{"_id": id}
}

// applyPattern sets either a solid color or a gradient as the fill/stroke
// style. Returns false if arg was neither a known hex string nor a
// gradient handle.
func (c *JSCanvas) applyPattern(arg any, fill bool) bool {
	switch v := arg.(type) {
	case string:
		c.dc.SetHexColor(v)
		return true
	case map[string]any:
		id, ok := v["_id"].(string)
		if !ok {
			return false
		}
		g, ok := c.gradients[id]
		if !ok {
			return false
		}
		if fill {
			c.dc.SetFillStyle(g)
		} else {
			c.dc.SetStrokeStyle(g)
		}
		return true
	}
	return false
}
