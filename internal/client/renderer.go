package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"dev-null/internal/display"
	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// sharedAudioCtx is the process-wide Ebitengine audio context (44100 Hz sample rate).
// Created lazily on first use via sync.Once to avoid panicking if audio is never used.
var (
	sharedAudioCtx     *audio.Context
	sharedAudioCtxOnce sync.Once
)

func getAudioCtx() *audio.Context {
	sharedAudioCtxOnce.Do(func() {
		sharedAudioCtx = audio.NewContext(44100)
	})
	return sharedAudioCtx
}

// cellW and cellH are the pixel dimensions of a single terminal cell.
// These alias display.CellW/CellH for internal use.
const (
	cellW = display.CellW
	cellH = display.CellH
)

// Game implements ebiten.Game for the dev-null client.
type Game struct {
	conn *SSHConn
	grid *TerminalGrid

	// Font for text cell rendering.
	fontFace text.Face

	// Charmap state.
	charmapDef  *render.CharMapDef
	atlasImage  *ebiten.Image
	spriteCache map[rune]*ebiten.Image // PUA codepoint → cropped sprite

	// Canvas frame — rendered image from server-side renderCanvas.
	canvasFrame *ebiten.Image // latest decoded canvas frame, or nil

	// Local rendering — runs game JS on the client with server-provided state.
	localRenderer *LocalRenderer
	clientScreen  *ClientScreen // full NC-style UI for local rendering
	gameSrcFiles  []GameSrcFile // JS source files for the current game
	gameStateJSON []byte        // latest decompressed game state JSON
	renderMode    string        // "local" or "remote" (default)
	localCanvas   *ebiten.Image // locally rendered canvas frame
	localBuf      *render.ImageBuffer // locally rendered cell buffer (full screen)
	playerID      string        // this client's player ID
	chatLines     []string      // chat messages (received from ANSI stream for now)

	// Asset loading progress.
	assetTotal    int // expected asset count (from asset-manifest OSC)
	assetReceived int // assets received so far

	// Audio state. Keys are bare filenames (e.g. "music.ogg").
	audioAssets  map[string][]byte        // raw decoded asset bytes
	audioPlayers map[string]*audio.Player // currently playing audio players

	// MIDI synthesizer for SoundFont-based audio.
	midiSynth *MidiSynth

	// Data directory path (for locating SoundFont files, etc.).
	dataDir string

	// Read buffer for SSH data.
	readBuf []byte
	mu      sync.Mutex
}

// DefaultFontFace returns the built-in bitmap font face for terminal rendering.
func DefaultFontFace() text.Face {
	return display.DefaultFontFace()
}

// NewGame creates a new client game instance.
func NewGame(conn *SSHConn, fontFace text.Face, width, height int, playerID, dataDir string) *Game {
	cols := width / cellW
	rows := height / cellH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	t := theme.Default()

	g := &Game{
		conn:          conn,
		grid:          NewTerminalGrid(cols, rows),
		fontFace:      fontFace,
		spriteCache:   make(map[rune]*ebiten.Image),
		localRenderer: NewLocalRenderer(),
		clientScreen:  NewClientScreen(t),
		playerID:      playerID,
		dataDir:       dataDir,
		midiSynth:     NewMidiSynth(filepath.Join(dataDir, "soundfonts", "chiptune.sf2")),
		readBuf:       make([]byte, 64*1024),
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
			// Game source files — load into local renderer.
			if len(g.grid.GameSrcFiles) > 0 {
				g.gameSrcFiles = g.grid.GameSrcFiles
				g.grid.GameSrcFiles = nil
				g.localRenderer.LoadGame(g.gameSrcFiles)
			}
			// Game state delta — update local renderer.
			if g.grid.StateData != nil {
				g.gameStateJSON = decompressBytes(g.grid.StateData)
				g.grid.StateData = nil
				if g.gameStateJSON != nil {
					g.localRenderer.SetState(g.gameStateJSON)
				}
			}
			// Render mode.
			if g.grid.RenderMode != "" {
				g.renderMode = g.grid.RenderMode
				g.grid.RenderMode = ""
			}
			// Asset manifest — resets progress tracking and clears old assets/sounds.
			if g.grid.AssetManifestTotal > 0 {
				g.assetTotal = g.grid.AssetManifestTotal
				g.assetReceived = 0
				g.audioAssets = make(map[string][]byte)
				g.stopSound("")
				g.midiSynth.AllNotesOff()
				g.grid.AssetManifestTotal = 0
			}
			// Incoming binary assets.
			for _, a := range g.grid.AssetFiles {
				if g.audioAssets == nil {
					g.audioAssets = make(map[string][]byte)
				}
				g.audioAssets[filepath.Base(a.Name)] = a.Data
				g.assetReceived++
			}
			g.grid.AssetFiles = nil
			// Sound commands.
			for _, cmd := range g.grid.SoundCmds {
				if cmd.Stop {
					g.stopSound(cmd.Filename)
				} else {
					g.playSound(cmd.Filename, cmd.Loop)
				}
			}
			g.grid.SoundCmds = nil
			// MIDI events.
			for _, ev := range g.grid.MidiEvents {
				g.midiSynth.DispatchEvent(ev)
			}
			g.grid.MidiEvents = nil
			// SoundFont switch.
			if g.grid.SynthName != "" {
				sf2Path := filepath.Join(g.dataDir, "soundfonts", g.grid.SynthName+".sf2")
				if err := g.midiSynth.LoadSoundFont(sf2Path); err == nil {
					g.midiSynth.mu.Lock()
					g.midiSynth.fontName = g.grid.SynthName
					g.midiSynth.mu.Unlock()
				}
				g.grid.SynthName = ""
			}
			g.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (g *Game) loadCharmap(jsonData []byte) {
	var def render.CharMapDef
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

// decompressBytes decompresses gzipped data.
func decompressBytes(data []byte) []byte {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil
	}
	return raw
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

// audioStream is the common interface implemented by all Ebitengine audio decoders.
type audioStream interface {
	io.ReadSeeker
	Length() int64
}

// playSound plays the named audio file. If the file is not yet loaded (asset not received),
// the call is silently dropped. If the sound is already playing, it is restarted.
// Must be called with g.mu held.
func (g *Game) playSound(filename string, loop bool) {
	data, ok := g.audioAssets[filename]
	if !ok {
		return // asset not yet received
	}
	g.stopSound(filename) // stop any existing player for this file

	ext := strings.ToLower(filepath.Ext(filename))
	ctx := getAudioCtx()

	var stream audioStream
	switch ext {
	case ".ogg":
		s, err := vorbis.DecodeWithSampleRate(44100, bytes.NewReader(data))
		if err != nil {
			return
		}
		stream = s
	case ".mp3":
		s, err := mp3.DecodeWithSampleRate(44100, bytes.NewReader(data))
		if err != nil {
			return
		}
		stream = s
	case ".wav":
		s, err := wav.DecodeWithSampleRate(44100, bytes.NewReader(data))
		if err != nil {
			return
		}
		stream = s
	default:
		return
	}

	var readSeeker io.ReadSeeker = stream
	if loop {
		readSeeker = audio.NewInfiniteLoop(stream, stream.Length())
	}
	player, err := ctx.NewPlayer(readSeeker)
	if err != nil {
		return
	}
	player.Play()
	if g.audioPlayers == nil {
		g.audioPlayers = make(map[string]*audio.Player)
	}
	g.audioPlayers[filename] = player
}

// stopSound stops playback of the named audio file. An empty filename stops all sounds.
// Must be called with g.mu held.
func (g *Game) stopSound(filename string) {
	if g.audioPlayers == nil {
		return
	}
	if filename == "" {
		for _, p := range g.audioPlayers {
			p.Pause()
			p.Close()
		}
		g.audioPlayers = make(map[string]*audio.Player)
		return
	}
	if p, ok := g.audioPlayers[filename]; ok {
		p.Pause()
		p.Close()
		delete(g.audioPlayers, filename)
	}
}

// drawLoadingOverlay renders a centered progress bar when assets are still loading.
func (g *Game) drawLoadingOverlay(screen *ebiten.Image) {
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	barW := w / 2
	barH := 16
	bx := (w - barW) / 2
	by := h/2 - barH/2

	// Background bar.
	bgImg := ebiten.NewImage(barW, barH)
	bgImg.Fill(color.RGBA{R: 60, G: 60, B: 60, A: 220})
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(bx), float64(by))
	screen.DrawImage(bgImg, op)

	// Fill bar.
	if g.assetTotal > 0 {
		fillW := barW * g.assetReceived / g.assetTotal
		if fillW > 0 {
			fillImg := ebiten.NewImage(fillW, barH)
			fillImg.Fill(color.RGBA{R: 80, G: 180, B: 80, A: 255})
			op2 := &ebiten.DrawImageOptions{}
			op2.GeoM.Translate(float64(bx), float64(by))
			screen.DrawImage(fillImg, op2)
		}
	}

	// Label.
	label := fmt.Sprintf("Loading assets... %d/%d", g.assetReceived, g.assetTotal)
	dop := &text.DrawOptions{}
	dop.GeoM.Translate(float64(bx), float64(by+barH+4))
	dop.ColorScale.ScaleWithColor(color.RGBA{R: 200, G: 200, B: 200, A: 255})
	text.Draw(screen, label, g.fontFace, dop)
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

	// Full local rendering: if we have game JS + state and mode is not "remote",
	// render the entire UI locally (NC chrome + game viewport) — no dependency on ANSI stream.
	if g.localRenderer.IsLoaded() && g.gameStateJSON != nil && g.clientScreen != nil && g.renderMode != "remote" {
		g.drawLocal(screen)
		return
	}

	// Fallback: remote rendering from parsed ANSI stream.
	g.drawRemote(screen)

	// Loading overlay: shown while assets are still being received.
	if g.assetTotal > 0 && g.assetReceived < g.assetTotal {
		g.drawLoadingOverlay(screen)
	}
}

// drawLocal renders the full screen using the client's local JS runtime + NC widgets.
func (g *Game) drawLocal(screen *ebiten.Image) {
	cols := g.grid.Width
	rows := g.grid.Height

	var vpX, vpY, vpW, vpH int
	renderFn := func(buf *render.ImageBuffer, x, y, w, h int) {
		vpX, vpY, vpW, vpH = x, y, w, h
		// Call the game's cell-based render locally.
		g.localRenderer.RenderCells(g.playerID, w, h)
		// For now, just run the cell render into the buffer.
		cellBuf := g.localRenderer.RenderCells(g.playerID, w, h)
		if cellBuf != nil {
			buf.Blit(x, y, cellBuf)
		}
	}

	// Render the full NC screen.
	g.localBuf = g.clientScreen.RenderPlaying(cols, rows, g.chatLines, "Local render", renderFn)

	// Draw cell buffer to Ebitengine screen.
	if g.localBuf != nil {
		g.drawImageBuffer(screen, g.localBuf)
	}

	// Draw local canvas frame in the viewport if available.
	if vpW > 0 && vpH > 0 && g.localRenderer.HasCanvas() {
		scale := g.localRenderer.CanvasScale
		canvasImg := g.localRenderer.RenderCanvas(g.playerID, vpW*scale, vpH*scale)
		if canvasImg != nil {
			fop := &ebiten.DrawImageOptions{}
			fw := float64(vpW*cellW) / float64(canvasImg.Bounds().Dx())
			fh := float64(vpH*cellH) / float64(canvasImg.Bounds().Dy())
			fop.GeoM.Scale(fw, fh)
			fop.GeoM.Translate(float64(vpX*cellW), float64(vpY*cellH))
			screen.DrawImage(canvasImg, fop)
		}
	}
}

// drawRemote renders from the parsed ANSI stream (server-rendered).
func (g *Game) drawRemote(screen *ebiten.Image) {
	vx := g.grid.ViewportX
	vy := g.grid.ViewportY
	vw := g.grid.ViewportW
	vh := g.grid.ViewportH

	// Local canvas rendering: if the local renderer has game JS + state,
	// render canvas locally at the client's chosen scale (no server bandwidth cost).
	if vw > 0 && vh > 0 && g.localRenderer.IsLoaded() && g.localRenderer.HasCanvas() && g.gameStateJSON != nil {
		scale := g.localRenderer.CanvasScale
		g.localCanvas = g.localRenderer.RenderCanvas(g.playerID, vw*scale, vh*scale)
	}

	// Draw canvas frame in the viewport (prefer local, fall back to server-sent).
	canvasImg := g.localCanvas
	if canvasImg == nil {
		canvasImg = g.canvasFrame
	}
	if canvasImg != nil && vw > 0 && vh > 0 {
		vpPx := vx * cellW
		vpPy := vy * cellH
		vpPw := vw * cellW
		vpPh := vh * cellH
		fop := &ebiten.DrawImageOptions{}
		fw := float64(vpPw) / float64(canvasImg.Bounds().Dx())
		fh := float64(vpPh) / float64(canvasImg.Bounds().Dy())
		fop.GeoM.Scale(fw, fh)
		fop.GeoM.Translate(float64(vpPx), float64(vpPy))
		screen.DrawImage(canvasImg, fop)
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

			if inViewport && render.IsPUA(cell.Char) {
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
// drawImageBuffer renders an ImageBuffer to the Ebitengine screen.
func (g *Game) drawImageBuffer(screen *ebiten.Image, buf *render.ImageBuffer) {
	display.DrawImageBuffer(screen, buf, g.fontFace)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}
