package client

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"image/color"
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

// cellW() and cellH() return the pixel dimensions of a single terminal cell.
// These delegate to display.CellW/CellH which are set by GUIFontFace.
func cellW() int { return display.CellW }
func cellH() int { return display.CellH }

// ClientRenderer implements display.Renderer for the SSH client.
// It reads from an SSH connection, renders the ANSI stream or local game
// state, and handles canvas overlays and audio.
type ClientRenderer struct {
	conn     *SSHConn
	grid     *TerminalGrid
	fontFace text.Face // set from Window.FontFace on first Draw

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

	// Connection state.
	started    chan struct{} // closed on first Update(); readLoop waits on this
	connClosed bool         // set by readLoop when SSH connection closes

	// Asset loading progress.
	assetTotal    int // expected asset count (from asset-manifest OSC)
	assetReceived int // assets received so far

	// Audio state. Keys are bare filenames (e.r. "music.ogg").
	audioAssets  map[string][]byte        // raw decoded asset bytes
	audioPlayers map[string]*audio.Player // currently playing audio players

	// MIDI synthesizer for SoundFont-based audio.
	midiSynth *MidiSynth

	// Directory paths for locating SoundFont files and other assets.
	// installDir is the exe directory (bundled read-only assets).
	// dataDir is the user data directory (user-added/overridden content).
	installDir string
	dataDir    string

	// Cached cursor image.
	cursorImg *ebiten.Image

	// Read buffer for SSH data.
	readBuf []byte
	mu      sync.Mutex
}

// NewClientRenderer creates a new SSH client renderer.
// Use with display.RunWindow to open the GUI.
func NewClientRenderer(conn *SSHConn, width, height int, playerID, installDir, dataDir string) *ClientRenderer {
	cols := display.WindowCols(width)
	rows := display.WindowRows(height)

	t := theme.Default()

	r := &ClientRenderer{
		conn:          conn,
		grid:          NewTerminalGrid(cols, rows),
		localRenderer: NewLocalRenderer(),
		clientScreen:  NewClientScreen(t),
		playerID:      playerID,
		dataDir:       dataDir,
		installDir:    installDir,
		midiSynth:     NewMidiSynth(findSoundFont(installDir, dataDir, "chiptune")),
		readBuf:       make([]byte, 64*1024),
		started:       make(chan struct{}),
	}

	// readLoop is started from HandleInput (first frame), not here.
	return r
}

func (r *ClientRenderer) readLoop() {
	// Wait for the game loop to start before reading SSH data.
	// On Windows, blocking SSH reads before ebiten.RunGame's event loop
	// can prevent window creation.
	<-r.started

	for {
		n, err := r.conn.Read(r.readBuf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, r.readBuf[:n])
			r.mu.Lock()
			r.grid.Feed(data)

			// Game source files — load into local renderer.
			if len(r.grid.GameSrcFiles) > 0 {
				r.gameSrcFiles = r.grid.GameSrcFiles
				r.grid.GameSrcFiles = nil
				r.localRenderer.LoadGame(r.gameSrcFiles)
			}
			// Game state baseline — replace Game.state wholesale.
			if r.grid.StateData != nil {
				r.gameStateJSON = decompressBytes(r.grid.StateData)
				r.grid.StateData = nil
				if r.gameStateJSON != nil {
					r.localRenderer.SetState(r.gameStateJSON)
				}
			}
			// Game state patch — merge changed top-level keys into Game.state.
			// The server guarantees a baseline (ns;state) has arrived first.
			if r.grid.StatePatch != nil {
				if patch := decompressBytes(r.grid.StatePatch); patch != nil {
					r.localRenderer.MergeStatePatch(patch)
					// Cache an updated "gameStateJSON != nil" signal so
					// render paths that gate on it keep working.
					if r.gameStateJSON == nil {
						r.gameStateJSON = patch
					}
				}
				r.grid.StatePatch = nil
			}
			// Render mode.
			if r.grid.RenderMode != "" {
				r.renderMode = r.grid.RenderMode
				r.grid.RenderMode = ""
			}
			// Server-assigned session ID — overrides the --player default
			// that we bootstrapped with, so games can look up s.players[pid].
			if r.grid.PlayerID != "" {
				r.playerID = r.grid.PlayerID
				r.grid.PlayerID = ""
			}
			// Asset manifest — resets progress tracking and clears old assets/sounds.
			if r.grid.AssetManifestTotal > 0 {
				r.assetTotal = r.grid.AssetManifestTotal
				r.assetReceived = 0
				r.audioAssets = make(map[string][]byte)
				r.stopSound("")
				r.midiSynth.AllNotesOff()
				r.grid.AssetManifestTotal = 0
			}
			// Incoming binary assets.
			for _, a := range r.grid.AssetFiles {
				if r.audioAssets == nil {
					r.audioAssets = make(map[string][]byte)
				}
				r.audioAssets[filepath.Base(a.Name)] = a.Data
				r.assetReceived++
			}
			r.grid.AssetFiles = nil
			// Sound commands.
			for _, cmd := range r.grid.SoundCmds {
				if cmd.Stop {
					r.stopSound(cmd.Filename)
				} else {
					r.playSound(cmd.Filename, cmd.Loop)
				}
			}
			r.grid.SoundCmds = nil
			// MIDI events.
			for _, ev := range r.grid.MidiEvents {
				r.midiSynth.DispatchEvent(ev)
			}
			r.grid.MidiEvents = nil
			// SoundFont switch.
			if r.grid.SynthName != "" {
				sf2Path := findSoundFont(r.installDir, r.dataDir, r.grid.SynthName)
				if err := r.midiSynth.LoadSoundFont(sf2Path); err == nil {
					r.midiSynth.mu.Lock()
					r.midiSynth.fontName = r.grid.SynthName
					r.midiSynth.mu.Unlock()
				}
				r.grid.SynthName = ""
			}
			r.mu.Unlock()
		}
		if err != nil {
			r.mu.Lock()
			r.connClosed = true
			r.mu.Unlock()
			return
		}
	}
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

// audioStream is the common interface implemented by all Ebitengine audio decoders.
type audioStream interface {
	io.ReadSeeker
	Length() int64
}

// playSound plays the named audio file. If the file is not yet loaded (asset not received),
// the call is silently dropped. If the sound is already playing, it is restarted.
// Must be called with r.mu held.
func (r *ClientRenderer) playSound(filename string, loop bool) {
	data, ok := r.audioAssets[filename]
	if !ok {
		return // asset not yet received
	}
	r.stopSound(filename) // stop any existing player for this file

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
	if r.audioPlayers == nil {
		r.audioPlayers = make(map[string]*audio.Player)
	}
	r.audioPlayers[filename] = player
}

// stopSound stops playback of the named audio file. An empty filename stops all sounds.
// Must be called with r.mu held.
func (r *ClientRenderer) stopSound(filename string) {
	if r.audioPlayers == nil {
		return
	}
	if filename == "" {
		for _, p := range r.audioPlayers {
			p.Pause()
			p.Close()
		}
		r.audioPlayers = make(map[string]*audio.Player)
		return
	}
	if p, ok := r.audioPlayers[filename]; ok {
		p.Pause()
		p.Close()
		delete(r.audioPlayers, filename)
	}
}

// drawLoadingOverlay renders a centered progress bar when assets are still loading.
func (r *ClientRenderer) drawLoadingOverlay(screen *ebiten.Image) {
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
	if r.assetTotal > 0 {
		fillW := barW * r.assetReceived / r.assetTotal
		if fillW > 0 {
			fillImg := ebiten.NewImage(fillW, barH)
			fillImg.Fill(color.RGBA{R: 80, G: 180, B: 80, A: 255})
			op2 := &ebiten.DrawImageOptions{}
			op2.GeoM.Translate(float64(bx), float64(by))
			screen.DrawImage(fillImg, op2)
		}
	}

	// Label.
	label := fmt.Sprintf("Loading assets... %d/%d", r.assetReceived, r.assetTotal)
	dop := &text.DrawOptions{}
	dop.GeoM.Translate(float64(bx), float64(by+barH+4))
	dop.ColorScale.ScaleWithColor(color.RGBA{R: 200, G: 200, B: 200, A: 255})
	text.Draw(screen, label, r.fontFace, dop)
}

// HandleInput implements display.Renderer.
func (r *ClientRenderer) HandleInput(w *display.Window) {
	// Start readLoop on first frame (game loop is running, window exists).
	select {
	case <-r.started:
	default:
		close(r.started)
		go r.readLoop()
	}
	r.midiSynth.ensurePlayer()
	r.handleInput()
}

// ShouldClose implements display.Renderer.
func (r *ClientRenderer) ShouldClose() bool {
	r.mu.Lock()
	closed := r.connClosed
	r.mu.Unlock()
	return closed
}

// Resize implements display.Renderer.
func (r *ClientRenderer) Resize(cols, rows int) {
	r.mu.Lock()
	r.grid.Resize(cols, rows)
	r.mu.Unlock()
	_ = r.conn.SendWindowChange(cols, rows)
}

// Draw implements display.Renderer.
func (r *ClientRenderer) Draw(w *display.Window, screen *ebiten.Image) {
	r.fontFace = w.FontFace
	r.mu.Lock()
	defer r.mu.Unlock()

	// Always use remote rendering: the server's ANSI stream provides the full chrome
	// (menus, chat, overlays, status bar). Local rendering only enhances the game
	// viewport — the local canvas is overlaid by drawRemote when available.
	r.drawRemote(screen)

	// Loading overlay: shown while assets are still being received.
	if r.assetTotal > 0 && r.assetReceived < r.assetTotal {
		r.drawLoadingOverlay(screen)
	}
}

// drawLocal renders the full screen using the client's local JS runtime + NC widgets.
func (r *ClientRenderer) drawLocal(screen *ebiten.Image) {
	cols := r.grid.Width
	rows := r.grid.Height

	var vpX, vpY, vpW, vpH int
	renderFn := func(buf *render.ImageBuffer, x, y, w, h int) {
		vpX, vpY, vpW, vpH = x, y, w, h
		// Call the game's cell-based render locally.
		r.localRenderer.RenderCells(r.playerID, w, h)
		// For now, just run the cell render into the buffer.
		cellBuf := r.localRenderer.RenderCells(r.playerID, w, h)
		if cellBuf != nil {
			buf.Blit(x, y, cellBuf)
		}
	}

	// Render the full NC screen.
	r.localBuf = r.clientScreen.RenderPlaying(cols, rows, r.chatLines, "Local render", renderFn)

	// Draw cell buffer to Ebitengine screen.
	if r.localBuf != nil {
		r.drawImageBuffer(screen, r.localBuf, nil)
	}

	// Draw local canvas frame in the viewport if available.
	if vpW > 0 && vpH > 0 && r.localRenderer.HasCanvas() {
		canvasImg := r.localRenderer.RenderCanvas(r.playerID, vpW*cellW(), vpH*cellH())
		if canvasImg != nil {
			fop := &ebiten.DrawImageOptions{}
			fw := float64(vpW*cellW()) / float64(canvasImg.Bounds().Dx())
			fh := float64(vpH*cellH()) / float64(canvasImg.Bounds().Dy())
			fop.GeoM.Scale(fw, fh)
			fop.GeoM.Translate(float64(vpX*cellW()), float64(vpY*cellH()))
			screen.DrawImage(canvasImg, fop)
		}
	}
}

// drawRemote renders from the parsed ANSI stream (server-rendered).
func (r *ClientRenderer) drawRemote(screen *ebiten.Image) {
	vx := r.grid.ViewportX
	vy := r.grid.ViewportY
	vw := r.grid.ViewportW
	vh := r.grid.ViewportH

	canReaderLocal := vw > 0 && vh > 0 && r.localRenderer.IsLoaded() && r.localRenderer.HasCanvas() && r.gameStateJSON != nil

	// Blocks-local mode: render canvas locally and convert to quadrant block
	// characters. We render into a scratch buffer and copy only into cells
	// that still hold the CanvasCell placeholder, so any menu/dialog cells
	// the server composited on top of the viewport are preserved.
	if r.renderMode == "blocks-local" && canReaderLocal {
		img := r.localRenderer.RenderCanvasImage(r.playerID, vw*2, vh*4)
		if img != nil {
			qbuf := render.NewImageBuffer(vw, vh)
			render.ImageToQuadrants(img, qbuf, 0, 0, vw, vh)
			buf := r.grid.ToImageBuffer()
			for cy := 0; cy < vh; cy++ {
				for cx := 0; cx < vw; cx++ {
					if !render.IsCanvasCell(buf.CharAt(vx+cx, vy+cy)) {
						continue
					}
					src := qbuf.Pixels[cy*qbuf.Width+cx]
					buf.SetChar(vx+cx, vy+cy, src.Char, src.Fg, src.Bg, src.Attr)
				}
			}
			r.drawImageBuffer(screen, buf, nil)
			if r.grid.CursorVisible {
				r.drawCursor(screen)
			}
			return
		}
	}

	// Pixels mode: render canvas locally at full display pixel resolution.
	if r.renderMode == "local" && canReaderLocal {
		r.localCanvas = r.localRenderer.RenderCanvas(r.playerID, vw*cellW(), vh*cellH())
	} else {
		r.localCanvas = nil
	}

	// Canvas HD compositing: server fills viewport with CanvasCell placeholders;
	// we draw the locally-rendered canvas first, then cells on top — skipping
	// placeholders so the canvas shows through. Menus/dialogs that overlap
	// replace placeholders with real cells, rendering on top automatically.
	canvasImg := r.localCanvas
	if canvasImg != nil && vw > 0 && vh > 0 {
		vpPx := vx * cellW()
		vpPy := vy * cellH()
		vpPw := vw * cellW()
		vpPh := vh * cellH()
		fop := &ebiten.DrawImageOptions{}
		fw := float64(vpPw) / float64(canvasImg.Bounds().Dx())
		fh := float64(vpPh) / float64(canvasImg.Bounds().Dy())
		fop.GeoM.Scale(fw, fh)
		fop.GeoM.Translate(float64(vpPx), float64(vpPy))
		screen.DrawImage(canvasImg, fop)
	}

	// Render cell buffer on top. Skip CanvasCell placeholders so local canvas shows through.
	buf := r.grid.ToImageBuffer()
	hasCanvas := canvasImg != nil && vw > 0 && vh > 0
	var drawOpts *display.DrawOptions
	if hasCanvas {
		drawOpts = &display.DrawOptions{
			SkipFunc: func(char rune, _ int, _ int) bool {
				return render.IsCanvasCell(char)
			},
		}
	}
	r.drawImageBuffer(screen, buf, drawOpts)

	// Draw text cursor if visible.
	if r.grid.CursorVisible {
		r.drawCursor(screen)
	}
}

// drawCursor renders a steady block cursor at the grid's cursor position.
func (r *ClientRenderer) drawCursor(screen *ebiten.Image) {
	cx := r.grid.CursorX
	cy := r.grid.CursorY
	if cx < 0 || cx >= r.grid.Width || cy < 0 || cy >= r.grid.Height {
		return
	}

	px := cx * cellW()
	py := cy * cellH()

	if r.cursorImg == nil {
		r.cursorImg = ebiten.NewImage(cellW(), cellH())
		r.cursorImg.Fill(color.RGBA{R: 204, G: 204, B: 204, A: 180})
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(px), float64(py))
	screen.DrawImage(r.cursorImg, op)
}

// Layout implements ebiten.Game.
// drawImageBuffer renders an ImageBuffer to the Ebitengine screen.
func (r *ClientRenderer) drawImageBuffer(screen *ebiten.Image, buf *render.ImageBuffer, opts *display.DrawOptions) {
	display.DrawImageBuffer(screen, buf, r.fontFace, opts)
}

// Layout and LayoutF are inherited from the embedded display.Window struct.
