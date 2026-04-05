package server

import (
	"context"
	"log/slog"
	"time"

	"dev-null/internal/network"
)

// EnablePinggyLogBridge starts polling the Pinggy status file and updates
// state.Net.PinggyURL when the TCP address is found.
func (a *Server) EnablePinggyLogBridge(ctx context.Context, statusFile string) {
	slog.Info("pinggy log bridge enabled", "status_file", statusFile)
	go a.runPinggyLogBridge(ctx, statusFile)
}

func (a *Server) runPinggyLogBridge(ctx context.Context, statusFile string) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	seenCount := 0
	seenMessages := make(map[string]struct{})

	for {
		select {
		case <-ctx.Done():
			slog.Info("pinggy log bridge stopped")
			return
		case <-ticker.C:
			status, err := network.ReadPinggyStatus(statusFile)
			if err != nil {
				slog.Debug("pinggy log bridge read failed", "error", err)
				continue
			}

			// Process log lines before the invite so Pinggy output appears first.
			if seenCount > len(status.LogLines) {
				seenCount = 0
				seenMessages = make(map[string]struct{})
			}

			for _, line := range status.LogLines[seenCount:] {
				if _, exists := seenMessages[line]; exists {
					continue
				}
				seenMessages[line] = struct{}{}
				a.serverLog("[pinggy] " + line)
			}
			seenCount = len(status.LogLines)

			if status.TcpAddress != "" {
				a.state.Lock()
				changed := a.state.Net.PinggyURL != status.TcpAddress
				a.state.Net.PinggyURL = status.TcpAddress
				a.state.Unlock()
				if changed {
					a.LogInviteCommand()
				}
			}
		}
	}
}
