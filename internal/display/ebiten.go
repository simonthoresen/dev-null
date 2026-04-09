package display

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

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
func RunWindow(renderer Renderer, title string, width, height int, iconICO []byte) error {
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
	if len(iconICO) > 0 {
		_ = SetWindowIconFromICO(iconICO)
	}

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

// --- Helpers for external renderers ---

// DrawCells renders an ImageBuffer to the screen using the window's font.
// Convenience wrapper around DrawImageBuffer for use by Renderer implementations.
func DrawCells(w *Window, screen *ebiten.Image, buf *render.ImageBuffer, opts *DrawOptions) {
	DrawImageBuffer(screen, buf, w.FontFace, opts)
}
