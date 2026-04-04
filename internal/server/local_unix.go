//go:build !windows

package server

import (
	"os"
	"os/signal"
	"syscall"

	xterm "github.com/charmbracelet/x/term"

	"null-space/internal/client"
)

// watchTerminalResize listens for SIGWINCH and forwards resize events to the
// SSH session. Returns a stop function to clean up.
func watchTerminalResize(conn *client.SSHConn) func() {
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	go func() {
		for range sigwinch {
			if w, h, err := xterm.GetSize(os.Stdout.Fd()); err == nil {
				conn.SendWindowChange(w, h)
			}
		}
	}()
	return func() {
		signal.Stop(sigwinch)
		close(sigwinch)
	}
}
