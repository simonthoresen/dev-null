package server

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"
)

func (a *App) EnablePinggyLogBridge(ctx context.Context, statusFile string) {
	go a.runPinggyLogBridge(ctx, statusFile)
}

func (a *App) runPinggyLogBridge(ctx context.Context, statusFile string) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	seenCount := 0
	seenMessages := make(map[string]struct{})

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lines, err := readPinggyLogLines(statusFile)
			if err != nil {
				continue
			}

			if seenCount > len(lines) {
				seenCount = 0
				seenMessages = make(map[string]struct{})
			}

			for _, line := range lines[seenCount:] {
				if _, exists := seenMessages[line]; exists {
					continue
				}
				seenMessages[line] = struct{}{}
				a.addPinggyMessage(line)
			}
			seenCount = len(lines)
		}
	}
}

func readPinggyLogLines(statusFile string) ([]string, error) {
	file, err := os.Open(statusFile)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	lines := make([]string, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "PINGGY_LOG=") {
			continue
		}

		message := strings.TrimSpace(strings.TrimPrefix(line, "PINGGY_LOG="))
		if message == "" {
			continue
		}
		lines = append(lines, message)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
