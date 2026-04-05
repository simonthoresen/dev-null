package runlog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func ConfigureFromEnv(component string) (func() error, error) {
	logFilePath := strings.TrimSpace(os.Getenv("DEV_NULL_LOG_FILE"))
	level := parseLevel(strings.TrimSpace(os.Getenv("DEV_NULL_LOG_LEVEL")))

	var (
		output io.Writer = io.Discard
		closer io.Closer
	)

	if logFilePath != "" {
		if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
			return nil, err
		}

		// Append to existing file (PS1 creates it) or create new.
		file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}

		output = file
		closer = file
	}

	handler := slog.NewTextHandler(output, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})
	logger := slog.New(handler).With("component", component, "pid", os.Getpid())
	slog.SetDefault(logger)

	return func() error {
		if closer == nil {
			return nil
		}
		return closer.Close()
	}, nil
}


func parseLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
