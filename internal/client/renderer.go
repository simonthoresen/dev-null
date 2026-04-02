package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/bitmapfont/v4"

	"null-space/common"
)

// cellW and cellH are the pixel dimensions of a single terminal cell.
const (
	cellW = 10
	cellH = 20
)

// Game implements ebiten.Game for the null-space client.
type Game struct {
	conn *SSHConn
	grid *TerminalGrid

	// Font for text cell rendering.
	fontFace text.Face

	// Charmap state.
	charmapDef  *common.CharMapDef
	atlasImage  *ebiten.Image
	spriteCache map[rune]*ebiten.Image // PUA codepoint → cropped sprite

	// Canvas frame — rendered image from server-side renderCanvas.
	canvasFrame *ebiten.Image // latest decoded canvas frame, or nil

	// Read buffer for SSH data.
	readBuf []byte
	mu      sync.Mutex
}

// DefaultFontFace returns the built-in bitmap font face for terminal rendering.
func DefaultFontFace() text.Face {
	return text.NewGoXFace(bitmapfont.Face)
}

// NewGame creates a new client game instance.
func NewGame(conn *SSHConn, fontFace text.Face, width, height int) *Game {
	cols := width / cellW
	rows := height / cellH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	g := &Game{
		conn:        conn,
		grid:        NewTerminalGrid(cols, rows),
		fontFace:    fontFace,
		spriteCache: make(map[rune]*ebiten.Image),
		readBuf:     make([]byte, 64*1024),
	}

	// Start reading SSH output in background.
	go g.readLoop()

	return g
}

func (g *Game) readLoop() {
	for {
		n, err := g.conn.Read(g.readBuf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, g.readBuf[:n])
			g.mu.Lock()
			g.grid.Feed(data)

			// Check for new charmap data.
			if g.grid.CharmapJSON != nil {
				g.loadCharmap(g.grid.CharmapJSON)
				g.grid.CharmapJSON = nil
			}
			if g.grid.AtlasData != nil {
				g.loadAtlas(g.grid.AtlasData)
				g.grid.AtlasData = nil
			}
			if g.grid.FrameData != nil {
				g.loadCanvasFrame(g.grid.FrameData)
				g.grid.FrameData = nil
			}
			g.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (g *Game) loadCharmap(jsonData []byte) {
	var def common.CharMapDef
	if err := json.Unmarshal(jsonData, &def); err != nil {
		return
	}
	g.charmapDef = &def
	g.spriteCache = make(map[rune]*ebiten.Image)
}

func (g *Game) loadAtlas(gzipData []byte) {
	gz, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return
	}
	defer gz.Close()

	raw, err := io.ReadAll(gz)
	if err != nil {
		return
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}

	g.atlasImage = ebiten.NewImageFromImage(img)
	g.spriteCache = make(map[rune]*ebiten.Image)
}

func (g *Game) loadCanvasFrame(gzipData []byte) {
	gz, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return
	}
	defer gz.Close()

	raw, err := io.ReadAll(gz)
	if err != nil {
		return
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}

	g.canvasFrame = ebiten.NewImageFromImage(img)
}

func (g *Game) getSprite(r rune) *ebiten.Image {
	if g.charmapDef == nil || g.atlasImage == nil {
		return nil
	}
	if cached, ok := g.spriteCache[r]; ok {
		return cached
	}
	entry := g.charmapDef.Lookup(r)
	if entry == nil {
		return nil
	}
	// Crop the sprite from the atlas.
	sprite := g.atlasImage.SubImage(image.Rect(entry.X, entry.Y, entry.X+entry.W, entry.Y+entry.H)).(*ebiten.Image)
	g.spriteCache[r] = sprite
	return sprite
}

// Update implements ebiten.Game.
func (g *Game) Update() error {
	// Handle window resize.
	w, h := ebiten.WindowSize()
	cols := w / cellW
	rows := h / cellH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	g.mu.Lock()
	if cols != g.grid.Width || rows != g.grid.Height {
		g.grid.Resize(cols, rows)
		g.mu.Unlock()
		_ = g.conn.SendWindowChange(cols, rows)
	} else {
		g.mu.Unlock()
	}

	// Handle keyboard input.
	g.handleInput()

	return nil
}

// Draw implements ebiten.Game.
func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 0, G: 0, B: 0, A: 255})

	g.mu.Lock()
	defer g.mu.Unlock()

	vx := g.grid.ViewportX
	vy := g.grid.ViewportY
	vw := g.grid.ViewportW
	vh := g.grid.ViewportH

	// Draw canvas frame as the viewport background (if present).
	if g.canvasFrame != nil && vw > 0 && vh > 0 {
		vpPx := vx * cellW
		vpPy := vy * cellH
		vpPw := vw * cellW
		vpPh := vh * cellH
		fop := &ebiten.DrawImageOptions{}
		// Scale canvas frame to fit the viewport pixel area.
		fw := float64(vpPw) / float64(g.canvasFrame.Bounds().Dx())
		fh := float64(vpPh) / float64(g.canvasFrame.Bounds().Dy())
		fop.GeoM.Scale(fw, fh)
		fop.GeoM.Translate(float64(vpPx), float64(vpPy))
		screen.DrawImage(g.canvasFrame, fop)
	}

	for cy := 0; cy < g.grid.Height; cy++ {
		for cx := 0; cx < g.grid.Width; cx++ {
			cell := g.grid.At(cx, cy)
			if cell == nil {
				continue
			}

			px := cx * cellW
			py := cy * cellH

			// Draw background.
			bgImg := ebiten.NewImage(cellW, cellH)
			bgImg.Fill(cell.Bg)
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(px), float64(py))
			screen.DrawImage(bgImg, op)

			// Check if this is a PUA cell inside the viewport.
			inViewport := vw > 0 && vh > 0 &&
				cx >= vx && cx < vx+vw &&
				cy >= vy && cy < vy+vh

			if inViewport && common.IsPUA(cell.Char) {
				// Render sprite from charmap.
				sprite := g.getSprite(cell.Char)
				if sprite != nil {
					sop := &ebiten.DrawImageOptions{}
					// Scale sprite to fit cell.
					sw := float64(cellW) / float64(sprite.Bounds().Dx())
					sh := float64(cellH) / float64(sprite.Bounds().Dy())
					sop.GeoM.Scale(sw, sh)
					sop.GeoM.Translate(float64(px), float64(py))
					screen.DrawImage(sprite, sop)
				}
			} else if cell.Char != ' ' && cell.Char != 0 {
				// Render text character.
				dop := &text.DrawOptions{}
				dop.GeoM.Translate(float64(px), float64(py))
				dop.ColorScale.ScaleWithColor(cell.Fg)
				text.Draw(screen, string(cell.Char), g.fontFace, dop)
			}
		}
	}
}

// Layout implements ebiten.Game.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}
