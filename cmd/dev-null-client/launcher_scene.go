package main

import (
	"image"
	"image/color"
	"math"

	"dev-null/internal/engine"
)

// launcherScene renders a small island in a calm sea, with a cube on its
// peak. The camera flies a slow drone-orbit around the island, with a gentle
// independent yaw so the framing breathes. Lighting is a single directional
// sun: lambert shading on terrain / cube / water, and the cube casts a
// hard-edged shadow onto the terrain via per-vertex AABB ray tests.
//
// Geometry:
//   - sky: vertical gradient fill (also rendered to draw the sun disc)
//   - terrain: heightmapped grid (terrainSize × terrainSize), baked once
//   - water: low-res grid with sinusoidal wave displacement, refreshed each
//     frame so its triangle normals shimmer in the sun
//   - cube: 6 lambert-lit faces
//
// Rendering uses engine.Rasterizer directly (depth-tested, per-vertex-shaded
// triangles) — same primitive the in-game 3D path uses, no JS bridge.
type launcherScene struct {
	img    *image.RGBA
	raster *engine.Rasterizer
	w, h   int

	terrainV     [terrainSize * terrainSize]vec3
	terrainN     [terrainSize * terrainSize]vec3
	terrainBaked bool
}

func newLauncherScene() *launcherScene { return &launcherScene{} }

const (
	islandRadius  = 2.6
	seaLevel      = 0.0
	cubeHalf      = 0.45
	cubeY         = 1.55
	terrainSize   = 36
	terrainExtent = 4.0
	waterSize     = 22
	waterExtent   = 8.0
)

// ── Vec3 helpers ───────────────────────────────────────────────────────────

type vec3 [3]float64

func vSub(a, b vec3) vec3   { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func vAdd(a, b vec3) vec3   { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func vScale(a vec3, k float64) vec3 {
	return vec3{a[0] * k, a[1] * k, a[2] * k}
}
func vDot(a, b vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func vCross(a, b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func vNorm(a vec3) vec3 {
	l := math.Sqrt(vDot(a, a))
	if l == 0 {
		return vec3{}
	}
	return vScale(a, 1/l)
}

// ── Public entry point ─────────────────────────────────────────────────────

func (s *launcherScene) Render(w, h int, t float64) *image.RGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	if s.img == nil || s.w != w || s.h != h {
		s.img = image.NewRGBA(image.Rect(0, 0, w, h))
		s.raster = engine.NewRasterizer(s.img)
		s.w, s.h = w, h
	}
	if !s.terrainBaked {
		s.bakeTerrain()
		s.terrainBaked = true
	}

	cam := s.buildCamera(t)
	s.drawSky(cam)
	s.raster.ClearDepth()
	s.drawWater(cam, t)
	s.drawTerrain(cam)
	s.drawCube(cam)
	return s.img
}

// ── Terrain ────────────────────────────────────────────────────────────────

func terrainHeight(x, z float64) float64 {
	r := math.Sqrt(x*x + z*z)
	if r >= islandRadius {
		return -0.08
	}
	f := 1 - r/islandRadius
	f = f * f * (3 - 2*f) // smoothstep falloff to the shore
	bump := 0.55 * math.Sin(x*1.4) * math.Cos(z*1.6)
	bump += 0.30 * math.Sin(x*3.0+z*1.5)
	bump += 0.15 * math.Cos(x*2.5 - z*3.0)
	return f * (0.55 + 0.55*bump)
}

func (s *launcherScene) bakeTerrain() {
	step := 2 * terrainExtent / float64(terrainSize-1)
	for j := 0; j < terrainSize; j++ {
		for i := 0; i < terrainSize; i++ {
			x := -terrainExtent + float64(i)*step
			z := -terrainExtent + float64(j)*step
			s.terrainV[j*terrainSize+i] = vec3{x, terrainHeight(x, z), z}
		}
	}
	for j := 0; j < terrainSize; j++ {
		for i := 0; i < terrainSize; i++ {
			il, ir := clampI(i-1, 0, terrainSize-1), clampI(i+1, 0, terrainSize-1)
			jl, jr := clampI(j-1, 0, terrainSize-1), clampI(j+1, 0, terrainSize-1)
			vL := s.terrainV[j*terrainSize+il]
			vR := s.terrainV[j*terrainSize+ir]
			vD := s.terrainV[jl*terrainSize+i]
			vU := s.terrainV[jr*terrainSize+i]
			n := vNorm(vCross(vSub(vU, vD), vSub(vR, vL)))
			s.terrainN[j*terrainSize+i] = n
		}
	}
}

func terrainColorAt(y float64) color.RGBA {
	switch {
	case y < 0.05:
		return color.RGBA{R: 215, G: 200, B: 145, A: 0xff} // beach
	case y < 0.45:
		return color.RGBA{R: 70, G: 140, B: 70, A: 0xff} // grass
	case y < 0.75:
		return color.RGBA{R: 95, G: 105, B: 60, A: 0xff} // dark grass
	default:
		return color.RGBA{R: 160, G: 155, B: 145, A: 0xff} // rock
	}
}

func (s *launcherScene) drawTerrain(cam *camera) {
	cubeMin := vec3{-cubeHalf, cubeY - cubeHalf, -cubeHalf}
	cubeMax := vec3{cubeHalf, cubeY + cubeHalf, cubeHalf}
	toSun := vScale(cam.sunDir, -1)

	var lit [terrainSize * terrainSize]color.RGBA
	for k := 0; k < terrainSize*terrainSize; k++ {
		v := s.terrainV[k]
		n := s.terrainN[k]
		base := terrainColorAt(v[1])
		c := engine.Lambert(n[0], n[1], n[2], cam.sunDir[0], cam.sunDir[1], cam.sunDir[2], base, 0.30)
		// Cast a ray from the vertex toward the sun; if it hits the cube AABB
		// the terrain point is in shadow.
		origin := vAdd(v, vec3{0, 0.002, 0})
		if rayHitsAABB(origin, toSun, cubeMin, cubeMax) {
			c = scaleColor(c, 0.55)
		}
		lit[k] = c
	}

	for j := 0; j < terrainSize-1; j++ {
		for i := 0; i < terrainSize-1; i++ {
			a := j*terrainSize + i
			b := a + 1
			c := (j+1)*terrainSize + i
			d := c + 1
			cam.drawTri(s.raster, s.terrainV[a], s.terrainV[b], s.terrainV[d], lit[a], lit[b], lit[d])
			cam.drawTri(s.raster, s.terrainV[a], s.terrainV[d], s.terrainV[c], lit[a], lit[d], lit[c])
		}
	}
}

// ── Water ──────────────────────────────────────────────────────────────────

func waterY(x, z, t float64) float64 {
	return seaLevel +
		0.05*math.Sin(x*1.3+t*1.2) +
		0.04*math.Cos(z*1.5+t*0.9) +
		0.025*math.Sin((x+z)*0.9+t*0.7)
}

func (s *launcherScene) drawWater(cam *camera, t float64) {
	step := 2 * waterExtent / float64(waterSize)
	base := color.RGBA{R: 30, G: 95, B: 145, A: 0xff}
	for j := 0; j < waterSize; j++ {
		for i := 0; i < waterSize; i++ {
			x0 := -waterExtent + float64(i)*step
			z0 := -waterExtent + float64(j)*step
			x1 := x0 + step
			z1 := z0 + step
			v00 := vec3{x0, waterY(x0, z0, t), z0}
			v01 := vec3{x1, waterY(x1, z0, t), z0}
			v10 := vec3{x0, waterY(x0, z1, t), z1}
			v11 := vec3{x1, waterY(x1, z1, t), z1}
			cam.drawLitQuad(s.raster, v00, v01, v11, v10, base, 0.40)
		}
	}
}

// ── Cube ───────────────────────────────────────────────────────────────────

var cubeFaceIdx = [6][4]int{
	{0, 1, 2, 3}, // -Z
	{5, 4, 7, 6}, // +Z
	{4, 0, 3, 7}, // -X
	{1, 5, 6, 2}, // +X
	{4, 5, 1, 0}, // -Y
	{3, 2, 6, 7}, // +Y
}

func (s *launcherScene) drawCube(cam *camera) {
	c := vec3{0, cubeY, 0}
	h := cubeHalf
	corners := [8]vec3{
		{c[0] - h, c[1] - h, c[2] - h}, {c[0] + h, c[1] - h, c[2] - h},
		{c[0] + h, c[1] + h, c[2] - h}, {c[0] - h, c[1] + h, c[2] - h},
		{c[0] - h, c[1] - h, c[2] + h}, {c[0] + h, c[1] - h, c[2] + h},
		{c[0] + h, c[1] + h, c[2] + h}, {c[0] - h, c[1] + h, c[2] + h},
	}
	base := color.RGBA{R: 210, G: 90, B: 70, A: 0xff}
	for _, f := range cubeFaceIdx {
		cam.drawLitQuad(s.raster, corners[f[0]], corners[f[1]], corners[f[2]], corners[f[3]], base, 0.25)
	}
}

// ── Sky ────────────────────────────────────────────────────────────────────

func (s *launcherScene) drawSky(cam *camera) {
	pix := s.img.Pix
	stride := s.img.Stride
	topR, topG, topB := 38.0, 70.0, 130.0
	botR, botG, botB := 175.0, 200.0, 220.0
	for y := 0; y < s.h; y++ {
		f := float64(y) / float64(s.h-1)
		r := uint8(topR*(1-f) + botR*f)
		g := uint8(topG*(1-f) + botG*f)
		b := uint8(topB*(1-f) + botB*f)
		row := y * stride
		for x := 0; x < s.w; x++ {
			off := row + x*4
			pix[off+0] = r
			pix[off+1] = g
			pix[off+2] = b
			pix[off+3] = 0xff
		}
	}

	// Sun: project the sun direction (treated as a point at infinity)
	// into screen space and splat a soft disc.
	sx, sy, ok := cam.projectDirection(vScale(cam.sunDir, -1))
	if !ok {
		return
	}
	radius := math.Min(float64(s.w), float64(s.h)) * 0.05
	core := color.RGBA{R: 255, G: 245, B: 200, A: 0xff}
	halo := color.RGBA{R: 255, G: 220, B: 160, A: 0xff}
	r2 := radius * radius
	hr2 := (radius * 1.9) * (radius * 1.9)
	minX := int(sx - radius*1.9)
	maxX := int(sx + radius*1.9)
	minY := int(sy - radius*1.9)
	maxY := int(sy + radius*1.9)
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= s.w {
		maxX = s.w - 1
	}
	if maxY >= s.h {
		maxY = s.h - 1
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := float64(x) - sx
			dy := float64(y) - sy
			d2 := dx*dx + dy*dy
			off := y*stride + x*4
			if d2 < r2 {
				pix[off+0] = core.R
				pix[off+1] = core.G
				pix[off+2] = core.B
			} else if d2 < hr2 {
				k := 1.0 - (d2-r2)/(hr2-r2)
				pix[off+0] = blendByte(pix[off+0], halo.R, k*0.7)
				pix[off+1] = blendByte(pix[off+1], halo.G, k*0.7)
				pix[off+2] = blendByte(pix[off+2], halo.B, k*0.7)
			}
		}
	}
}

// ── Camera + projection ────────────────────────────────────────────────────

type camera struct {
	pos                vec3
	right, up, fwd     vec3
	focal              float64
	cx, cy             float64
	sunDir             vec3
}

func (s *launcherScene) buildCamera(t float64) *camera {
	const (
		orbitR = 7.5
		orbitW = 0.10
		camY   = 3.6
	)
	lookAt := vec3{0, 0.6, 0}
	a := t * orbitW
	pos := vec3{
		lookAt[0] + orbitR*math.Cos(a),
		camY,
		lookAt[2] + orbitR*math.Sin(a),
	}
	fwd0 := vNorm(vSub(lookAt, pos))
	// Slow extra yaw around world Y so the framing breathes independently
	// of the orbit.
	yaw := 0.35 * math.Sin(t*0.18)
	cosY, sinY := math.Cos(yaw), math.Sin(yaw)
	fwd := vNorm(vec3{
		fwd0[0]*cosY + fwd0[2]*sinY,
		fwd0[1],
		-fwd0[0]*sinY + fwd0[2]*cosY,
	})
	right := vNorm(vCross(fwd, vec3{0, 1, 0}))
	up := vCross(right, fwd)

	return &camera{
		pos:    pos,
		right:  right,
		up:     up,
		fwd:    fwd,
		focal:  math.Min(float64(s.w), float64(s.h)) * 0.75,
		cx:     float64(s.w) * 0.5,
		cy:     float64(s.h) * 0.5,
		sunDir: vNorm(vec3{-0.45, -0.75, -0.4}),
	}
}

// project transforms a world-space point to screen-space + camera depth.
// Returns ok=false if the point is at or behind the near plane.
func (c *camera) project(p vec3) (px, py, pz float64, ok bool) {
	d := vSub(p, c.pos)
	ex := vDot(d, c.right)
	ey := vDot(d, c.up)
	ez := vDot(d, c.fwd)
	if ez < 0.1 {
		return 0, 0, 0, false
	}
	return c.cx + c.focal*ex/ez, c.cy - c.focal*ey/ez, ez, true
}

// projectDirection projects a direction (treated as a point at infinity).
func (c *camera) projectDirection(dir vec3) (px, py float64, ok bool) {
	ex := vDot(dir, c.right)
	ey := vDot(dir, c.up)
	ez := vDot(dir, c.fwd)
	if ez <= 0 {
		return 0, 0, false
	}
	return c.cx + c.focal*ex/ez, c.cy - c.focal*ey/ez, true
}

func (c *camera) drawTri(r *engine.Rasterizer, a, b, d vec3, ca, cb, cd color.RGBA) {
	pax, pay, paz, oa := c.project(a)
	pbx, pby, pbz, ob := c.project(b)
	pdx, pdy, pdz, od := c.project(d)
	if !oa || !ob || !od {
		return
	}
	r.FillTriangle(pax, pay, paz, pbx, pby, pbz, pdx, pdy, pdz, ca, cb, cd)
}

// drawLitQuad lambert-shades a quad with its face normal and rasterizes it
// as two flat triangles.
func (c *camera) drawLitQuad(r *engine.Rasterizer, a, b, cv, d vec3, base color.RGBA, ambient float64) {
	n := vNorm(vCross(vSub(b, a), vSub(cv, a)))
	lit := engine.Lambert(n[0], n[1], n[2], c.sunDir[0], c.sunDir[1], c.sunDir[2], base, ambient)
	c.drawTri(r, a, b, cv, lit, lit, lit)
	c.drawTri(r, a, cv, d, lit, lit, lit)
}

// ── Misc helpers ───────────────────────────────────────────────────────────

func rayHitsAABB(origin, dir, mn, mx vec3) bool {
	tmin, tmax := math.Inf(-1), math.Inf(1)
	for i := 0; i < 3; i++ {
		if math.Abs(dir[i]) < 1e-9 {
			if origin[i] < mn[i] || origin[i] > mx[i] {
				return false
			}
			continue
		}
		invD := 1.0 / dir[i]
		t1 := (mn[i] - origin[i]) * invD
		t2 := (mx[i] - origin[i]) * invD
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tmin {
			tmin = t1
		}
		if t2 < tmax {
			tmax = t2
		}
		if tmin > tmax {
			return false
		}
	}
	return tmax > 1e-3
}

func scaleColor(c color.RGBA, k float64) color.RGBA {
	return color.RGBA{
		R: clampByte(float64(c.R) * k),
		G: clampByte(float64(c.G) * k),
		B: clampByte(float64(c.B) * k),
		A: 0xff,
	}
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func blendByte(dst, src uint8, k float64) uint8 {
	if k <= 0 {
		return dst
	}
	if k >= 1 {
		return src
	}
	return uint8(float64(dst)*(1-k) + float64(src)*k)
}
