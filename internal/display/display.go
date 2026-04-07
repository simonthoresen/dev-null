package display

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
)

// BufferViewer is implemented by Bubble Tea models that can expose their
// raw render buffer, allowing the GUI backend to skip ANSI serialization.
type BufferViewer interface {
	ViewBuffer() *render.ImageBuffer
}

// Backend abstracts the display target for a Bubble Tea model.
// A model can be run in a terminal (TUI) or an Ebitengine window (GUI).
type Backend interface {
	// Run starts the display loop (blocking). The model is driven by the
	// backend: it receives tea.Msg via Update() and renders via View() or
	// ViewBuffer().
	Run(model tea.Model) error

	// Send delivers a message to the running model asynchronously.
	// Safe to call from any goroutine. No-op if Run() hasn't been called.
	Send(msg tea.Msg)
}

// Option configures a Backend.
type Option func(*options)

type options struct {
	fps          int
	programOpts  []tea.ProgramOption
	windowTitle  string
	windowWidth  int
	windowHeight int
}

func defaultOptions() options {
	return options{
		fps:          60,
		windowTitle:  "dev-null",
		windowWidth:  1200,
		windowHeight: 800,
	}
}

// WithFPS sets the target frames per second.
func WithFPS(fps int) Option {
	return func(o *options) { o.fps = fps }
}

// WithProgramOptions passes additional tea.ProgramOption values to the
// TerminalBackend's underlying tea.Program.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(o *options) { o.programOpts = append(o.programOpts, opts...) }
}

// WithWindowTitle sets the Ebitengine window title (GUI mode only).
func WithWindowTitle(title string) Option {
	return func(o *options) { o.windowTitle = title }
}

// WithWindowSize sets the initial Ebitengine window size (GUI mode only).
func WithWindowSize(w, h int) Option {
	return func(o *options) {
		o.windowWidth = w
		o.windowHeight = h
	}
}
