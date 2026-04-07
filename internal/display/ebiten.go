package display

import (
	"image/color"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
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

	// Protects model and buf access between Update() and Draw().
	mu  sync.Mutex
	buf interface{} // *render.ImageBuffer via BufferViewer, or nil

	// Track window size for resize detection.
	lastCols int
	lastRows int

	// Cursor blink state.
	cursorVisible bool
	cursorTicker  time.Time
}

// NewEbitenBackend creates a backend that renders to an Ebitengine window.
func NewEbitenBackend(opts ...Option) *EbitenBackend {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &EbitenBackend{
		opts:          o,
		fontFace:      DefaultFontFace(),
		msgCh:         make(chan tea.Msg, 256),
		cursorVisible: true,
		cursorTicker:  time.Now(),
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
	cols := e.opts.windowWidth / CellW
	rows := e.opts.windowHeight / CellH
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
	cols := w / CellW
	rows := h / CellH
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

	// Drain message queue and feed to model.
	e.mu.Lock()
	defer e.mu.Unlock()

	for {
		select {
		case msg := <-e.msgCh:
			if _, ok := msg.(tea.QuitMsg); ok {
				return ebiten.Termination
			}
			var cmd tea.Cmd
			e.model, cmd = e.model.Update(msg)
			e.processCmd(cmd)
		default:
			return nil
		}
	}
}

// Draw implements ebiten.Game.
func (e *EbitenBackend) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 0, G: 0, B: 0, A: 255})

	e.mu.Lock()
	defer e.mu.Unlock()

	// Prefer direct buffer access (no ANSI round-trip).
	if bv, ok := e.model.(BufferViewer); ok {
		if buf := bv.ViewBuffer(); buf != nil {
			DrawImageBuffer(screen, buf, e.fontFace)
			return
		}
	}

	// Fallback: call View() and render the string content.
	// For models that don't implement BufferViewer, we'd need to parse ANSI.
	// For now, this path just renders the model's string output as plain text.
	view := e.model.View()
	if view.Content != "" {
		dop := &text.DrawOptions{}
		dop.GeoM.Translate(0, 0)
		dop.ColorScale.ScaleWithColor(color.RGBA{R: 204, G: 204, B: 204, A: 255})
		text.Draw(screen, view.Content, e.fontFace, dop)
	}
}

// Layout implements ebiten.Game.
func (e *EbitenBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
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
