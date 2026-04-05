//go:build windows

package server

import (
	"os"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"dev-null/internal/client"
)

// watchTerminalResize polls for terminal size changes on Windows (no SIGWINCH).
// Returns a stop function to clean up.
func watchTerminalResize(conn *client.SSHConn) func() {
	done := make(chan struct{})
	go func() {
		lastW, lastH, _ := xterm.GetSize(os.Stdout.Fd())
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if w, h, err := xterm.GetSize(os.Stdout.Fd()); err == nil && (w != lastW || h != lastH) {
					lastW, lastH = w, h
					conn.SendWindowChange(w, h)
				}
			}
		}
	}()
	return func() { close(done) }
}
