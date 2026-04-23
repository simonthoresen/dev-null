package engine

import (
	"image"
	"image/color"
	"math"
)

// Rasterizer renders depth-tested, per-vertex-shaded triangles directly
// into an *image.RGBA. It is attached to JSCanvas on demand.
//
// Coordinate convention: vertex (x, y) are screen pixels (floating-point,
// so sub-pixel positions are fine). z is a camera-space depth where a
// smaller z means closer to the camera. Initial depth values are
// +inf (math.MaxFloat32), and a fragment wins the depth test iff its z
// is strictly less than the stored value.
//
// Triangles with any winding order are rasterized (no backface cull).
// Degenerate triangles (zero signed area) are dropped.
type Rasterizer struct {
	img   *image.RGBA
	depth []float32
	w, h  int
}

// NewRasterizer wraps an existing RGBA image and allocates a depth buffer.
func NewRasterizer(img *image.RGBA) *Rasterizer {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	d := make([]float32, w*h)
	for i := range d {
		d[i] = math.MaxFloat32
	}
	return &Rasterizer{img: img, depth: d, w: w, h: h}
}

// ClearDepth resets the depth buffer to +inf.
func (r *Rasterizer) ClearDepth() {
	for i := range r.depth {
		r.depth[i] = math.MaxFloat32
	}
}

// FillTriangle rasterizes a triangle with per-vertex colors interpolated
// across its interior (Gouraud shading). Depth test: fragment's barycentric-
// interpolated z must be strictly less than the stored depth.
func (r *Rasterizer) FillTriangle(
	x0, y0, z0,
	x1, y1, z1,
	x2, y2, z2 float64,
	c0, c1, c2 color.RGBA,
) {
	// Signed twice-area. Used both for barycentric normalization and for
	// flipping the inside test on CW-wound triangles.
	area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
	if math.Abs(area) < 1e-9 {
		return // degenerate
	}
	invArea := 1.0 / area

	// Screen-space bounding box clipped to canvas.
	minX := int(math.Floor(min3(x0, x1, x2)))
	maxX := int(math.Ceil(max3(x0, x1, x2)))
	minY := int(math.Floor(min3(y0, y1, y2)))
	maxY := int(math.Ceil(max3(y0, y1, y2)))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX > r.w-1 {
		maxX = r.w - 1
	}
	if maxY > r.h-1 {
		maxY = r.h - 1
	}
	if minX > maxX || minY > maxY {
		return
	}

	// Edge function E(a, b, p) = (b.x - a.x)*(p.y - a.y) - (b.y - a.y)*(p.x - a.x).
	// This equals twice the signed area of triangle (a, b, p), so its sign
	// matches `area` for points on the same side of edge a→b as the
	// opposite vertex. Barycentric weights are:
	//   w0 = E(v1, v2, p)/area, w1 = E(v2, v0, p)/area, w2 = E(v0, v1, p)/area.
	// Each edge function is linear in p, so we precompute the dx/dy step.
	px := float64(minX) + 0.5
	py := float64(minY) + 0.5

	e0Row := (x2-x1)*(py-y1) - (y2-y1)*(px-x1) // opposite v0
	e1Row := (x0-x2)*(py-y2) - (y0-y2)*(px-x2) // opposite v1
	e2Row := (x1-x0)*(py-y0) - (y1-y0)*(px-x0) // opposite v2

	e0dx := -(y2 - y1)
	e0dy := x2 - x1
	e1dx := -(y0 - y2)
	e1dy := x0 - x2
	e2dx := -(y1 - y0)
	e2dy := x1 - x0

	pix := r.img.Pix
	stride := r.img.Stride

	// Inside test: all three edge fns share sign with area.
	flip := area < 0

	r0, g0, b0, a0 := float64(c0.R), float64(c0.G), float64(c0.B), float64(c0.A)
	r1, g1, b1, a1 := float64(c1.R), float64(c1.G), float64(c1.B), float64(c1.A)
	r2, g2, b2, a2 := float64(c2.R), float64(c2.G), float64(c2.B), float64(c2.A)

	rowBase := minY*stride + minX*4
	rowDepth := minY*r.w + minX

	for y := minY; y <= maxY; y++ {
		e0 := e0Row
		e1 := e1Row
		e2 := e2Row
		pi := rowBase
		di := rowDepth
		for x := minX; x <= maxX; x++ {
			var inside bool
			if flip {
				inside = e0 <= 0 && e1 <= 0 && e2 <= 0
			} else {
				inside = e0 >= 0 && e1 >= 0 && e2 >= 0
			}
			if inside {
				w0 := e0 * invArea
				w1 := e1 * invArea
				w2 := e2 * invArea
				z := w0*z0 + w1*z1 + w2*z2
				if float32(z) < r.depth[di] {
					r.depth[di] = float32(z)
					rr := w0*r0 + w1*r1 + w2*r2
					gg := w0*g0 + w1*g1 + w2*g2
					bb := w0*b0 + w1*b1 + w2*b2
					aa := w0*a0 + w1*a1 + w2*a2
					pix[pi+0] = clamp8(rr)
					pix[pi+1] = clamp8(gg)
					pix[pi+2] = clamp8(bb)
					pix[pi+3] = clamp8(aa)
				}
			}
			e0 += e0dx
			e1 += e1dx
			e2 += e2dx
			pi += 4
			di++
		}
		e0Row += e0dy
		e1Row += e1dy
		e2Row += e2dy
		rowBase += stride
		rowDepth += r.w
	}
}

// Lambert computes c * (ambient + (1 - ambient) * max(0, dot(n, l))).
// n and l must be unit vectors. ambient in [0, 1].
//
// This is the only built-in lighting model; callers that need Phong or
// anything fancier compute lit vertex colors themselves and hand them
// to FillTriangle directly.
func Lambert(nx, ny, nz, lx, ly, lz float64, base color.RGBA, ambient float64) color.RGBA {
	if ambient < 0 {
		ambient = 0
	}
	if ambient > 1 {
		ambient = 1
	}
	d := nx*lx + ny*ly + nz*lz
	if d < 0 {
		d = 0
	}
	k := ambient + (1-ambient)*d
	return color.RGBA{
		R: clamp8(float64(base.R) * k),
		G: clamp8(float64(base.G) * k),
		B: clamp8(float64(base.B) * k),
		A: base.A,
	}
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func min3(a, b, c float64) float64 {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func max3(a, b, c float64) float64 {
	if a > b {
		if a > c {
			return a
		}
		return c
	}
	if b > c {
		return b
	}
	return c
}
