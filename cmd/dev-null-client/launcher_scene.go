package main

import (
	"image"
	"image/color"
	"math"

	"dev-null/internal/engine"
)

// launcherScene draws an animated background for the launcher: a twinkling
// starfield with a slowly rotating, lambert-lit cube on top. Rendering goes
// straight into an *image.RGBA via engine.Rasterizer (the same triangle
// rasterizer the in-game 3D path uses), so the launcher and games share the
// pixel pipeline without going through a JS VM.
type launcherScene struct {
	img    *image.RGBA
	raster *engine.Rasterizer
	w, h   int
}

func newLauncherScene() *launcherScene { return &launcherScene{} }

// Render produces a w×h RGBA frame for the given elapsed time (seconds).
// The same backing image is reused across calls until the size changes.
func (s *launcherScene) Render(w, h int, elapsed float64) *image.RGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	if s.img == nil || s.w != w || s.h != h {
		s.img = image.NewRGBA(image.Rect(0, 0, w, h))
		s.raster = engine.NewRasterizer(s.img)
		s.w, s.h = w, h
	}

	s.drawBackground(elapsed)
	s.raster.ClearDepth()
	s.drawCube(elapsed)
	return s.img
}

var launcherBgColor = color.RGBA{R: 0x03, G: 0x05, B: 0x0d, A: 0xff}

func (s *launcherScene) drawBackground(t float64) {
	// Fill background.
	pix := s.img.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i+0] = launcherBgColor.R
		pix[i+1] = launcherBgColor.G
		pix[i+2] = launcherBgColor.B
		pix[i+3] = launcherBgColor.A
	}

	// Deterministic starfield with a time-modulated twinkle.
	const stars = 220
	stride := s.img.Stride
	for i := 0; i < stars; i++ {
		sx := (i*137 + 53) % s.w
		sy := (i*89 + 17) % s.h
		tw := 0.5 + 0.5*math.Sin(t*1.6+float64(i)*0.37)
		v := uint8(clampInt(110+int(tw*130), 0, 255))
		off := sy*stride + sx*4
		pix[off+0] = v
		pix[off+1] = v
		pix[off+2] = uint8(clampInt(int(v)+20, 0, 255))
		pix[off+3] = 0xff
	}
}

// Cube geometry: 6 faces × 2 triangles, with a base color per face.
var cubeFaces = [...]struct {
	idx   [4]int
	color color.RGBA
}{
	{[4]int{0, 1, 2, 3}, color.RGBA{0x5c, 0x6b, 0xc0, 0xff}}, // -Z
	{[4]int{5, 4, 7, 6}, color.RGBA{0x42, 0xa5, 0xf5, 0xff}}, // +Z
	{[4]int{4, 0, 3, 7}, color.RGBA{0x26, 0xa6, 0x9a, 0xff}}, // -X
	{[4]int{1, 5, 6, 2}, color.RGBA{0xef, 0x6c, 0x00, 0xff}}, // +X
	{[4]int{4, 5, 1, 0}, color.RGBA{0x7e, 0x57, 0xc2, 0xff}}, // -Y
	{[4]int{3, 2, 6, 7}, color.RGBA{0xec, 0x40, 0x7a, 0xff}}, // +Y
}

var cubeVerts = [8][3]float64{
	{-1, -1, -1}, {1, -1, -1}, {1, 1, -1}, {-1, 1, -1},
	{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1},
}

func (s *launcherScene) drawCube(t float64) {
	cx := float64(s.w) * 0.5
	cy := float64(s.h) * 0.5
	base := math.Min(float64(s.w), float64(s.h)) * 0.18
	sx := base * 2
	sy := base * 2

	ax := t * 0.55
	ay := t * 0.85
	az := t * 0.20
	cosX, sinX := math.Cos(ax), math.Sin(ax)
	cosY, sinY := math.Cos(ay), math.Sin(ay)
	cosZ, sinZ := math.Cos(az), math.Sin(az)

	rotate := func(v [3]float64) [3]float64 {
		x, y, z := v[0], v[1], v[2]
		rx := x*cosY + z*sinY
		rz := -x*sinY + z*cosY
		ry := y*cosX - rz*sinX
		rz = y*sinX + rz*cosX
		fx := rx*cosZ - ry*sinZ
		fy := rx*sinZ + ry*cosZ
		return [3]float64{fx, fy, rz}
	}

	// Camera-space depth used by the rasterizer (smaller = closer).
	project := func(v [3]float64) [3]float64 {
		z := v[2] + 4.5
		d := 4.0 / z
		return [3]float64{cx + v[0]*sx*d, cy + v[1]*sy*d, z}
	}

	var rv, pv [8][3]float64
	for i, v := range cubeVerts {
		rv[i] = rotate(v)
		pv[i] = project(rv[i])
	}

	lightDir := [3]float64{-0.4, -0.6, -0.7}
	llen := math.Sqrt(lightDir[0]*lightDir[0] + lightDir[1]*lightDir[1] + lightDir[2]*lightDir[2])
	lightDir[0] /= llen
	lightDir[1] /= llen
	lightDir[2] /= llen
	const ambient = 0.18

	for _, face := range cubeFaces {
		a, b, c := rv[face.idx[0]], rv[face.idx[1]], rv[face.idx[2]]
		ux, uy, uz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
		vx, vy, vz := c[0]-a[0], c[1]-a[1], c[2]-a[2]
		nx := uy*vz - uz*vy
		ny := uz*vx - ux*vz
		nz := ux*vy - uy*vx
		nlen := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if nlen == 0 {
			continue
		}
		nx /= nlen
		ny /= nlen
		nz /= nlen

		lit := engine.Lambert(nx, ny, nz, lightDir[0], lightDir[1], lightDir[2], face.color, ambient)

		p0, p1, p2, p3 := pv[face.idx[0]], pv[face.idx[1]], pv[face.idx[2]], pv[face.idx[3]]
		s.raster.FillTriangle(
			p0[0], p0[1], p0[2],
			p1[0], p1[1], p1[2],
			p2[0], p2[1], p2[2],
			lit, lit, lit,
		)
		s.raster.FillTriangle(
			p0[0], p0[1], p0[2],
			p2[0], p2[1], p2[2],
			p3[0], p3[1], p3[2],
			lit, lit, lit,
		)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
