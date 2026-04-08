package display

import (
	"image/color"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"dev-null/internal/clipboard"
	"dev-null/internal/render"
)

// Renderer provides game-specific logic to a Window.
// Implement this for server (Bubble Tea model) or client (SSH connection).
type Renderer interface {
	// HandleInput processes keyboard/mouse input each frame.
	HandleInput(w *Window)
	// Draw renders the current frame (background is already filled black).
	Draw(w *Window, screen *ebiten.Image)
	// Resize is called when the window size changes (in cell units).
	Resize(cols, rows int)
	// ShouldClose returns true when the window should exit.
	ShouldClose() bool
}

// Window is the single Ebitengine GUI implementation used by both server and
// client. Plug in a Renderer for game-specific behavior.
type Window struct {
	FontFace text.Face
	renderer Renderer
	started  chan struct{}
	lastCols int
	lastRows int
}

// RunWindow creates a Window with the given renderer and runs the Ebitengine
// game loop (blocking). This is the single entry point for all GUI rendering.
func RunWindow(renderer Renderer, title string, width, height int) error {
	w := &Window{
		FontFace: InitGUIFont(),
		renderer: renderer,
		started:  make(chan struct{}),
		lastCols: WindowCols(width),
		lastRows: WindowRows(height),
	}

	ebiten.SetWindowSize(width, height)
	ebiten.SetWindowTitle(title)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	return ebiten.RunGame(w)
}

// Update implements ebiten.Game.
func (w *Window) Update() error {
	// Signal that the game loop is running (first frame only).
	select {
	case <-w.started:
	default:
		close(w.started)
		w.renderer.Resize(w.lastCols, w.lastRows)
	}

	if w.renderer.ShouldClose() {
		return ebiten.Termination
	}

	// Handle window resize.
	if cols, rows, changed := w.detectResize(); changed {
		w.renderer.Resize(cols, rows)
	}

	w.renderer.HandleInput(w)
	return nil
}

// Draw implements ebiten.Game.
func (w *Window) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	w.renderer.Draw(w, screen)
}

// LayoutF implements ebiten.LayoutFer for HiDPI-aware rendering.
func (w *Window) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	return GameLayout(outsideWidth, outsideHeight)
}

// Layout implements ebiten.Game.
func (w *Window) Layout(outsideWidth, outsideHeight int) (int, int) {
	return GameLayoutInt(outsideWidth, outsideHeight)
}

// Started returns a channel that is closed when the game loop starts.
// Useful for deferring work (like SSH reads) until the window exists.
func (w *Window) Started() <-chan struct{} { return w.started }

func (w *Window) detectResize() (cols, rows int, changed bool) {
	ww, hh := ebiten.WindowSize()
	cols = WindowCols(ww)
	rows = WindowRows(hh)
	changed = cols != w.lastCols || rows != w.lastRows
	if changed {
		w.lastCols = cols
		w.lastRows = rows
	}
	return
}

// --- ServerRenderer: wraps a Bubble Tea model for the server GUI ---

// ServerRenderer drives a Bubble Tea model inside a Window.
// Used by the server console and --local --no-ssh mode.
type ServerRenderer struct {
	model tea.Model
	msgCh chan tea.Msg

	mu          sync.Mutex
	cursorStart time.Time
}

// NewServerRenderer creates a renderer that drives the given Bubble Tea model.
func NewServerRenderer() *ServerRenderer {
	return &ServerRenderer{
		msgCh:       make(chan tea.Msg, 256),
		cursorStart: time.Now(),
	}
}

// Run initializes the model and starts the Ebitengine window (blocking).
func (r *ServerRenderer) Run(model tea.Model, title string, width, height int) error {
	r.mu.Lock()
	r.model = model
	r.mu.Unlock()

	cmd := model.Init()
	r.processCmd(cmd)

	// Send initial window size.
	r.Send(tea.WindowSizeMsg{
		Width:  WindowCols(width),
		Height: WindowRows(height),
	})

	return RunWindow(r, title, width, height)
}

// Send delivers a message to the model asynchronously.
func (r *ServerRenderer) Send(msg tea.Msg) {
	select {
	case r.msgCh <- msg:
	default:
	}
}

func (r *ServerRenderer) HandleInput(w *Window) {
	for _, msg := range PollKeyMessages() {
		r.Send(msg)
	}
	for _, msg := range PollMouseMessages() {
		r.Send(msg)
	}
}

func (r *ServerRenderer) Draw(w *Window, screen *ebiten.Image) {
	r.mu.Lock()
	defer r.mu.Unlock()

	view := r.model.View()

	if bv, ok := r.model.(BufferViewer); ok {
		if buf := bv.ViewBuffer(); buf != nil {
			DrawImageBuffer(screen, buf, w.FontFace, nil)
		}
	}

	if cv, ok := r.model.(ClipboardViewer); ok {
		if text := cv.PopClipboard(); text != "" {
			go clipboard.Copy(text) //nolint:errcheck
		}
	}

	// Draw blinking cursor.
	if view.Cursor != nil {
		elapsed := time.Since(r.cursorStart)
		if (elapsed.Milliseconds()/530)%2 == 0 {
			cx := view.Cursor.Position.X
			cy := view.Cursor.Position.Y
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(float64(CellW), float64(CellH))
			op.GeoM.Translate(float64(cx*CellW), float64(cy*CellH))
			op.ColorScale.ScaleWithColor(color.RGBA{R: 200, G: 200, B: 200, A: 180})
			screen.DrawImage(sharedPixel, op)
		}
	}
}

func (r *ServerRenderer) Resize(cols, rows int) {
	r.Send(tea.WindowSizeMsg{Width: cols, Height: rows})
}

func (r *ServerRenderer) ShouldClose() bool {
	// Drain message queue and feed to model.
	r.mu.Lock()
	defer r.mu.Unlock()

	for range 64 {
		select {
		case msg := <-r.msgCh:
			if _, ok := msg.(tea.QuitMsg); ok {
				return true
			}
			if _, ok := msg.(tea.KeyPressMsg); ok {
				r.cursorStart = time.Now()
			}
			var cmd tea.Cmd
			r.model, cmd = r.model.Update(msg)
			r.processCmd(cmd)
		default:
			return false
		}
	}
	return false
}

func (r *ServerRenderer) processCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, subCmd := range batch {
				r.processCmd(subCmd)
			}
			return
		}
		r.Send(msg)
	}()
}

// --- Helpers for external renderers ---

// DrawCells renders an ImageBuffer to the screen using the window's font.
// Convenience wrapper around DrawImageBuffer for use by Renderer implementations.
func DrawCells(w *Window, screen *ebiten.Image, buf *render.ImageBuffer, opts *DrawOptions) {
	DrawImageBuffer(screen, buf, w.FontFace, opts)
}
