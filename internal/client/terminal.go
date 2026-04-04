// terminal.go implements terminal mode: local game rendering output to a terminal
// via ANSI escape sequences, instead of to an Ebitengine graphical window.
package client

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	xterm "github.com/charmbracelet/x/term"
	"github.com/charmbracelet/colorprofile"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// TerminalRunner runs the client in terminal mode: connects via SSH, receives
// game source + state via OSC, renders locally with the JS VM, and outputs
// ANSI to stdout.
type TerminalRunner struct {
	conn     *SSHConn
	grid     *TerminalGrid
	renderer *LocalRenderer
	screen   *ClientScreen
	playerID string
	profile  colorprofile.Profile

	gameSrcFiles  []GameSrcFile
	gameStateJSON []byte
	renderMode    string

	width, height int
	mu            sync.Mutex
	done          chan struct{}
}

// RunTerminal is the entry point for terminal mode. It blocks until the
// connection is closed or the user presses Ctrl-C.
// profile controls the color depth used when rendering to the local terminal.
func RunTerminal(conn *SSHConn, playerID string, profile colorprofile.Profile) error {
	// Get terminal size.
	w, h, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil {
		w, h = 120, 50
	}

	// Send initial terminal size to server.
	_ = conn.SendWindowChange(w, h)

	t := theme.Default()

	tr := &TerminalRunner{
		conn:     conn,
		grid:     NewTerminalGrid(w, h),
		renderer: NewLocalRenderer(),
		screen:   NewClientScreen(t),
		playerID: playerID,
		profile:  profile,
		width:    w,
		height:   h,
		done:     make(chan struct{}),
	}

	// Put terminal in raw mode.
	oldState, err := xterm.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer func() {
		// Show cursor, leave alt screen, restore terminal.
		os.Stdout.WriteString("\x1b[?25h\x1b[?1049l")
		xterm.Restore(os.Stdin.Fd(), oldState)
	}()

	// Enter alt screen, hide cursor.
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l")

	// Start background goroutines.
	go tr.readSSH()
	go tr.forwardStdin()
	go tr.watchResize()
	go tr.renderLoop()

	<-tr.done
	return nil
}

// readSSH reads data from the SSH connection and feeds it to the ANSI parser.
// We use TerminalGrid as an OSC parser — the parsed cell grid is ignored,
// only game source/state/mode OSC data is consumed.
func (tr *TerminalRunner) readSSH() {
	buf := make([]byte, 64*1024)
	for {
		n, err := tr.conn.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			tr.mu.Lock()
			tr.grid.Feed(data)

			// Extract OSC data for local rendering.
			if len(tr.grid.GameSrcFiles) > 0 {
				tr.gameSrcFiles = tr.grid.GameSrcFiles
				tr.grid.GameSrcFiles = nil
				tr.renderer.LoadGame(tr.gameSrcFiles)
			}
			if tr.grid.StateData != nil {
				tr.gameStateJSON = decompressBytes(tr.grid.StateData)
				tr.grid.StateData = nil
				if tr.gameStateJSON != nil {
					tr.renderer.SetState(tr.gameStateJSON)
				}
			}
			if tr.grid.RenderMode != "" {
				tr.renderMode = tr.grid.RenderMode
				tr.grid.RenderMode = ""
			}
			// Clear charmap/atlas/frame data — terminal mode can't use them.
			tr.grid.CharmapJSON = nil
			tr.grid.AtlasData = nil
			tr.grid.FrameData = nil
			tr.mu.Unlock()
		}
		if err != nil {
			close(tr.done)
			return
		}
	}
}

// forwardStdin reads raw bytes from os.Stdin and writes them to the SSH
// connection, so keypresses reach the server.
func (tr *TerminalRunner) forwardStdin() {
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if _, werr := tr.conn.Write(buf[:n]); werr != nil {
				close(tr.done)
				return
			}
		}
		if err != nil {
			close(tr.done)
			return
		}
	}
}

// watchResize polls for terminal size changes and sends window-change events
// to the server. Polling is used for cross-platform compatibility (SIGWINCH
// is not available on Windows).
func (tr *TerminalRunner) watchResize() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w, h, err := xterm.GetSize(os.Stdout.Fd())
			if err != nil {
				continue
			}
			tr.mu.Lock()
			changed := w != tr.width || h != tr.height
			if changed {
				tr.width = w
				tr.height = h
				tr.grid.Resize(w, h)
			}
			tr.mu.Unlock()
			if changed {
				_ = tr.conn.SendWindowChange(w, h)
			}
		case <-tr.done:
			return
		}
	}
}

// renderLoop runs at ~30fps, rendering the local game state to the terminal.
// When no game JS is loaded, it falls back to piping the server's ANSI stream.
func (tr *TerminalRunner) renderLoop() {
	ticker := time.NewTicker(33 * time.Millisecond) // ~30fps
	defer ticker.Stop()

	var lastOutput string

	for {
		select {
		case <-ticker.C:
			tr.mu.Lock()
			output := tr.render()
			tr.mu.Unlock()

			if output != lastOutput {
				// Move cursor home and write the full frame.
				io.WriteString(os.Stdout, "\x1b[H"+output)
				lastOutput = output
			}
		case <-tr.done:
			return
		}
	}
}

// render produces the current frame as an ANSI string. Must be called with
// tr.mu held.
func (tr *TerminalRunner) render() string {
	w, h := tr.width, tr.height

	// If game JS is loaded and we have state, render locally.
	if tr.renderer.IsLoaded() && tr.gameStateJSON != nil {
		renderFn := func(buf *render.ImageBuffer, x, y, bw, bh int) {
			cellBuf := tr.renderer.RenderCells(tr.playerID, bw, bh)
			if cellBuf != nil {
				buf.Blit(x, y, cellBuf)
			}
		}

		buf := tr.screen.RenderPlaying(w, h, nil, "Terminal", renderFn)
		if buf != nil {
			return buf.ToString(tr.profile)
		}
	}

	// Fallback: render the parsed ANSI grid (server-rendered content).
	buf := render.NewImageBuffer(w, h)
	for cy := 0; cy < h && cy < tr.grid.Height; cy++ {
		for cx := 0; cx < w && cx < tr.grid.Width; cx++ {
			cell := tr.grid.At(cx, cy)
			if cell == nil {
				continue
			}
			buf.SetChar(cx, cy, cell.Char, cell.Fg, cell.Bg, cell.Attr)
		}
	}
	return buf.ToString(tr.profile)
}
