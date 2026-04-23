package engine

import (
	"image"
	"image/color"
	"testing"
)

// newRaster builds a small RGBA canvas pre-filled with a background color
// and wraps it in a Rasterizer for test draws.
func newRaster(w, h int, bg color.RGBA) (*image.RGBA, *Rasterizer) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		off := i * 4
		img.Pix[off+0] = bg.R
		img.Pix[off+1] = bg.G
		img.Pix[off+2] = bg.B
		img.Pix[off+3] = bg.A
	}
	return img, NewRasterizer(img)
}

func TestFillTriangleFlat(t *testing.T) {
	_, r := newRaster(10, 10, color.RGBA{A: 255})
	red := color.RGBA{R: 255, A: 255}
	r.FillTriangle(
		1, 1, 0,
		8, 1, 0,
		1, 8, 0,
		red, red, red,
	)
	// Interior pixel should be red.
	if got := r.img.RGBAAt(3, 3); got != red {
		t.Errorf("(3,3) = %v, want red %v", got, red)
	}
	// Pixel outside triangle should be untouched.
	if got := r.img.RGBAAt(7, 7); got.R != 0 {
		t.Errorf("(7,7) = %v, expected untouched (R=0)", got)
	}
}

func TestDepthOcclusion(t *testing.T) {
	_, r := newRaster(10, 10, color.RGBA{A: 255})
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}

	// Far triangle first (z=5).
	r.FillTriangle(1, 1, 5, 9, 1, 5, 1, 9, 5, red, red, red)
	// Near triangle on top (z=1).
	r.FillTriangle(1, 1, 1, 9, 1, 1, 1, 9, 1, blue, blue, blue)
	if got := r.img.RGBAAt(3, 3); got != blue {
		t.Errorf("expected near triangle to win depth test, got %v", got)
	}

	// Now the other order: draw near first, far second. Far should be occluded.
	_, r2 := newRaster(10, 10, color.RGBA{A: 255})
	r2.FillTriangle(1, 1, 1, 9, 1, 1, 1, 9, 1, blue, blue, blue)
	r2.FillTriangle(1, 1, 5, 9, 1, 5, 1, 9, 5, red, red, red)
	if got := r2.img.RGBAAt(3, 3); got != blue {
		t.Errorf("expected far triangle to be occluded, got %v", got)
	}
}

func TestGouraudInterpolation(t *testing.T) {
	_, r := newRaster(21, 21, color.RGBA{A: 255})
	// Big triangle with three distinct vertex colors. Interior color should
	// be the barycentric blend.
	red := color.RGBA{R: 255, A: 255}
	green := color.RGBA{G: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	r.FillTriangle(
		10, 1, 0,
		1, 19, 0,
		19, 19, 0,
		red, green, blue,
	)
	// Centre of the triangle: weights are roughly (1/3, 1/3, 1/3) → (85, 85, 85).
	got := r.img.RGBAAt(10, 13)
	if got.R < 60 || got.R > 110 || got.G < 60 || got.G > 110 || got.B < 60 || got.B > 110 {
		t.Errorf("centre color %v not near equal-blend (~85,85,85)", got)
	}
	// Near the red vertex: red channel dominates.
	near := r.img.RGBAAt(10, 3)
	if near.R < 150 || near.G > 80 || near.B > 80 {
		t.Errorf("near-red vertex color %v doesn't show red dominance", near)
	}
}

func TestClipToCanvas(t *testing.T) {
	_, r := newRaster(10, 10, color.RGBA{A: 255})
	red := color.RGBA{R: 255, A: 255}
	// Triangle that extends well outside the canvas. Should not panic and
	// should render inside pixels correctly.
	r.FillTriangle(
		-5, -5, 0,
		20, 5, 0,
		5, 20, 0,
		red, red, red,
	)
	// At least one in-bounds pixel should be red.
	touched := 0
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if r.img.RGBAAt(x, y).R == 255 {
				touched++
			}
		}
	}
	if touched == 0 {
		t.Error("expected off-canvas triangle to still fill visible pixels")
	}
}

func TestDegenerateTriangleSkipped(t *testing.T) {
	_, r := newRaster(10, 10, color.RGBA{A: 255})
	red := color.RGBA{R: 255, A: 255}
	// Three collinear vertices → zero area, must not write anything.
	r.FillTriangle(
		1, 1, 0,
		5, 5, 0,
		9, 9, 0,
		red, red, red,
	)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if r.img.RGBAAt(x, y).R != 0 {
				t.Errorf("degenerate triangle wrote to (%d,%d)", x, y)
			}
		}
	}
}

func TestBothWindingsRender(t *testing.T) {
	// Two triangles, same three vertices but opposite winding, should both
	// fill their interior (no backface cull).
	red := color.RGBA{R: 255, A: 255}
	_, ccw := newRaster(10, 10, color.RGBA{A: 255})
	ccw.FillTriangle(1, 1, 0, 1, 8, 0, 8, 1, 0, red, red, red)
	if ccw.img.RGBAAt(3, 3).R != 255 {
		t.Error("CCW triangle did not fill interior")
	}
	_, cw := newRaster(10, 10, color.RGBA{A: 255})
	cw.FillTriangle(1, 1, 0, 8, 1, 0, 1, 8, 0, red, red, red)
	if cw.img.RGBAAt(3, 3).R != 255 {
		t.Error("CW triangle did not fill interior")
	}
}

func TestLambertShading(t *testing.T) {
	base := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	// Fully lit: normal straight at light. Expect base * (ambient + (1-ambient)*1) = base.
	lit := Lambert(0, 0, 1, 0, 0, 1, base, 0.2)
	if lit.R < 195 || lit.G < 95 || lit.B < 45 {
		t.Errorf("fully-lit surface dimmed unexpectedly: %v", lit)
	}
	// Back of surface: dot < 0 clamps to 0, color = base * ambient.
	unlit := Lambert(0, 0, -1, 0, 0, 1, base, 0.2)
	wantR, wantG, wantB := uint8(40), uint8(20), uint8(10)
	if abs8(unlit.R, wantR) > 2 || abs8(unlit.G, wantG) > 2 || abs8(unlit.B, wantB) > 2 {
		t.Errorf("back-facing surface = %v, want ~(%d,%d,%d)", unlit, wantR, wantG, wantB)
	}
	// 90° grazing: dot = 0 → ambient only.
	graze := Lambert(1, 0, 0, 0, 0, 1, base, 0.2)
	if graze.R > unlit.R+2 || graze.G > unlit.G+2 || graze.B > unlit.B+2 {
		t.Errorf("90° surface brighter than back surface: graze=%v unlit=%v", graze, unlit)
	}
	// Ambient=1 means full base regardless of normal.
	ambAll := Lambert(0, 0, -1, 0, 0, 1, base, 1.0)
	if ambAll.R != base.R || ambAll.G != base.G || ambAll.B != base.B {
		t.Errorf("ambient=1 should ignore lighting, got %v", ambAll)
	}
}

func TestClearDepthAllowsOverdraw(t *testing.T) {
	_, r := newRaster(10, 10, color.RGBA{A: 255})
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	r.FillTriangle(1, 1, 1, 9, 1, 1, 1, 9, 1, red, red, red)
	// Without clearing depth, a farther triangle would be occluded.
	r.FillTriangle(1, 1, 5, 9, 1, 5, 1, 9, 5, blue, blue, blue)
	if r.img.RGBAAt(3, 3) != red {
		t.Fatalf("setup: expected red on top")
	}
	// After clearing, the far triangle draws again unobstructed.
	r.ClearDepth()
	r.FillTriangle(1, 1, 5, 9, 1, 5, 1, 9, 5, blue, blue, blue)
	if r.img.RGBAAt(3, 3) != blue {
		t.Errorf("after ClearDepth, expected blue, got %v", r.img.RGBAAt(3, 3))
	}
}

func abs8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
