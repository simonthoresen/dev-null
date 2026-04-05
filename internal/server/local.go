package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	xterm "github.com/charmbracelet/x/term"

	"dev-null/internal/client"
	"dev-null/internal/network"
)

// PreloadGame loads a game by name before any players connect.
// The name is resolved to a path under the data directory, or treated as a URL.
func (a *Server) PreloadGame(name string) error {
	var path string
	if network.IsURL(name) {
		path = name
	} else {
		path = filepath.Join(a.dataDir, "games", name+".js")
	}
	return a.loadGame(path)
}

// PreloadResume resumes a saved game before any players connect.
func (a *Server) PreloadResume(gameName, saveName string) error {
	return a.resumeGame(gameName, saveName)
}

// RunLocalSSH starts the full SSH server, then connects back to it via a real
// SSH client and pipes the session to the local terminal. The user sees exactly
// what a remote player would see when running `ssh -p <port> localhost`.
// This exercises the entire network pipeline (SSH transport, session middleware,
// PTY, KittyStripWriter, etc.) without needing a separate SSH client.
// termOverride, if non-empty, requests a specific color profile for this session
// (values: truecolor, 256color, ansi, ascii).
func (a *Server) RunLocalSSH(ctx context.Context, playerName string, port int, termOverride string) error {
	// Mark this player name for auto-admin on connect.
	a.localPlayerName = playerName

	// Start SSH server and wait for it to be ready.
	ready := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- a.StartWithReady(ctx, ready)
	}()

	select {
	case <-ready:
		// Server is listening.
	case err := <-serverErr:
		return fmt.Errorf("server failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Get initial terminal size from stdout (the screen buffer handle on Windows).
	// Using Stdin.Fd() is wrong on Windows: GetConsoleScreenBufferInfo requires a
	// screen buffer handle (stdout/stderr), not the console input handle (stdin).
	w, h, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil {
		w, h = 120, 50
	}

	// Connect via SSH with the actual terminal dimensions so the first frame
	// is rendered at the correct size (avoids orphaned characters from a
	// mis-sized initial render that the diff renderer can't clean up).
	conn, err := client.Dial("127.0.0.1", port, playerName, false, termOverride, w, h)
	if err != nil {
		return fmt.Errorf("local SSH dial: %w", err)
	}
	defer conn.Close()
	// Guarantee alt-screen exit even if the SSH session closes before bubbletea
	// writes its own \x1b[?1049l cleanup sequence.
	defer fmt.Fprint(os.Stdout, "\x1b[?1049l\x1b[?25h")

	// Send initial window size.
	conn.SendWindowChange(w, h)

	// Put the local terminal in raw mode so ANSI sequences pass through.
	oldState, err := xterm.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return fmt.Errorf("make raw: %w", err)
	}
	defer xterm.Restore(os.Stdin.Fd(), oldState)

	// Forward terminal resize events (platform-specific).
	stopResize := watchTerminalResize(conn)
	defer stopResize()

	// Bidirectional pipe: SSH ↔ terminal.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(os.Stdout, conn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(conn, os.Stdin)
	}()

	// Wait for either direction to finish (e.g. server closes session, or user quits).
	wg.Wait()
	return nil
}
