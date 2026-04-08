package display

import (
	"image/color"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"dev-null/internal/clipboard"
)

// EbitenBackend runs a Bubble Tea model inside an Ebitengine window.
// It translates Ebitengine input events to tea.Msg, drives Update/View,
// and renders the resulting ImageBuffer as pixel cells.
type EbitenBackend struct {
	opts options

	model    tea.Model
	fontFace text.Face

	// Inbound message queue — fed by Send() and tea.Cmd goroutines.
	msgCh chan tea.Msg

	// Protects model access between Update() and Draw().
	mu    sync.Mutex
	dirty bool // true when model state changed since last Draw

	// Track window size for resize detection.
	lastCols int
	lastRows int

	// Cursor blink state.
	cursorStart time.Time
}

// NewEbitenBackend creates a backend that renders to an Ebitengine window.
func NewEbitenBackend(opts ...Option) *EbitenBackend {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &EbitenBackend{
		opts:        o,
		fontFace:    InitGUIFont(),
		msgCh:       make(chan tea.Msg, 256),
		dirty:       true, // force initial render
		cursorStart: time.Now(),
	}
}

// Run starts the Ebitengine game loop (blocking).
func (e *EbitenBackend) Run(model tea.Model) error {
	e.mu.Lock()
	e.model = model
	e.mu.Unlock()

	// Call Init and process any returned commands.
	cmd := model.Init()
	e.processCmd(cmd)

	// Send initial window size.
	cols := WindowCols(e.opts.windowWidth)
	rows := WindowRows(e.opts.windowHeight)
	e.lastCols = cols
	e.lastRows = rows
	e.Send(tea.WindowSizeMsg{Width: cols, Height: rows})

	ebiten.SetWindowSize(e.opts.windowWidth, e.opts.windowHeight)
	ebiten.SetWindowTitle(e.opts.windowTitle)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	return ebiten.RunGame(e)
}

// Send delivers a message to the model asynchronously.
func (e *EbitenBackend) Send(msg tea.Msg) {
	select {
	case e.msgCh <- msg:
	default:
		// Drop message if queue is full (shouldn't happen in practice).
	}
}

// Update implements ebiten.Game.
func (e *EbitenBackend) Update() error {
	// Handle window resize.
	w, h := ebiten.WindowSize()
	cols := WindowCols(w)
	rows := WindowRows(h)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols != e.lastCols || rows != e.lastRows {
		e.lastCols = cols
		e.lastRows = rows
		e.Send(tea.WindowSizeMsg{Width: cols, Height: rows})
	}

	// Poll Ebitengine input → tea.Msg.
	for _, msg := range PollKeyMessages() {
		e.Send(msg)
	}
	for _, msg := range PollMouseMessages() {
		e.Send(msg)
	}

	// Drain message queue and feed to model (limit per frame to avoid stalls).
	e.mu.Lock()
	defer e.mu.Unlock()

	for range 64 {
		select {
		case msg := <-e.msgCh:
			if _, ok := msg.(tea.QuitMsg); ok {
				return ebiten.Termination
			}
			// Reset cursor blink on any key press.
			if _, ok := msg.(tea.KeyPressMsg); ok {
				e.cursorStart = time.Now()
			}
			var cmd tea.Cmd
			e.model, cmd = e.model.Update(msg)
			e.processCmd(cmd)
			e.dirty = true
		default:
			return nil
		}
	}
	return nil
}

// Draw implements ebiten.Game.
func (e *EbitenBackend) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 0, G: 0, B: 0, A: 255})

	e.mu.Lock()
	defer e.mu.Unlock()

	// Call View() to update the render buffer and get cursor info.
	view := e.model.View()
	e.dirty = false

	// Read the buffer directly (no ANSI round-trip).
	if bv, ok := e.model.(BufferViewer); ok {
		if buf := bv.ViewBuffer(); buf != nil {
			DrawImageBuffer(screen, buf, e.fontFace)
		}
	}

	// Handle clipboard copy for GUI mode (no terminal to handle OSC 52).
	if cv, ok := e.model.(ClipboardViewer); ok {
		if text := cv.PopClipboard(); text != "" {
			go clipboard.Copy(text) //nolint:errcheck
		}
	}

	// Draw blinking cursor from the View's cursor position.
	if view.Cursor != nil {
		elapsed := time.Since(e.cursorStart)
		// 530ms on, 530ms off (standard terminal blink rate).
		blinkOn := (elapsed.Milliseconds()/530)%2 == 0
		if blinkOn {
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

// LayoutF implements ebiten.LayoutFer for HiDPI-aware rendering.
func (e *EbitenBackend) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	return GameLayout(outsideWidth, outsideHeight)
}

// Layout implements ebiten.Game (required by interface, but LayoutF takes precedence).
func (e *EbitenBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	return GameLayoutInt(outsideWidth, outsideHeight)
}

// processCmd runs a tea.Cmd in a goroutine, routing the result back via msgCh.
func (e *EbitenBackend) processCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg == nil {
			return
		}
		// Handle batch commands: if the result is a tea.BatchMsg, process each sub-cmd.
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, subCmd := range batch {
				e.processCmd(subCmd)
			}
			return
		}
		e.Send(msg)
	}()
}
