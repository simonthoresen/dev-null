package display

import (
	tea "charm.land/bubbletea/v2"
)

// TerminalBackend runs a Bubble Tea model in the current terminal.
// This is the traditional TUI mode — a thin wrapper around tea.Program.
type TerminalBackend struct {
	opts    options
	program *tea.Program
}

// NewTerminalBackend creates a backend that renders to the terminal.
func NewTerminalBackend(opts ...Option) *TerminalBackend {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &TerminalBackend{opts: o}
}

// Run starts the Bubble Tea program (blocking).
func (t *TerminalBackend) Run(model tea.Model) error {
	progOpts := []tea.ProgramOption{
		tea.WithFPS(t.opts.fps),
	}
	progOpts = append(progOpts, t.opts.programOpts...)

	t.program = tea.NewProgram(model, progOpts...)
	_, err := t.program.Run()
	return err
}

// Send delivers a message to the running tea.Program.
func (t *TerminalBackend) Send(msg tea.Msg) {
	if t.program != nil {
		t.program.Send(msg)
	}
}

// Program returns the underlying tea.Program, or nil if Run() hasn't been called.
// This is needed by server code that broadcasts tick messages to programs.
func (t *TerminalBackend) Program() *tea.Program {
	return t.program
}
