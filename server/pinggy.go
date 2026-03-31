package server

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
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
			status, err := readPinggyStatus(statusFile)
			if err != nil {
				slog.Debug("pinggy log bridge read failed", "error", err)
				continue
			}

			if status.TcpAddress != "" {
				a.state.mu.Lock()
				changed := a.state.Net.PinggyURL != status.TcpAddress
				a.state.Net.PinggyURL = status.TcpAddress
				a.state.mu.Unlock()
				if changed {
					a.LogInviteCommand()
				}
			}

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
		}
	}
}

type pinggyStatus struct {
	LogLines    []string
	TcpAddress  string
	JoinCommand string
}

func readPinggyStatus(statusFile string) (*pinggyStatus, error) {
	file, err := os.Open(statusFile)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	status := &pinggyStatus{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "PINGGY_LOG="):
			message := strings.TrimSpace(strings.TrimPrefix(line, "PINGGY_LOG="))
			if message != "" {
				status.LogLines = append(status.LogLines, message)
			}
		case strings.HasPrefix(line, "PINGGY_TCP="):
			status.TcpAddress = strings.TrimSpace(strings.TrimPrefix(line, "PINGGY_TCP="))
		case strings.HasPrefix(line, "PINGGY_JOIN="):
			status.JoinCommand = strings.TrimSpace(strings.TrimPrefix(line, "PINGGY_JOIN="))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return status, nil
}
