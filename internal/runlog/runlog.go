package runlog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ConfigureFromEnv(component string) (func() error, error) {
	logFilePath := strings.TrimSpace(os.Getenv("NULL_SPACE_LOG_FILE"))
	level := parseLevel(strings.TrimSpace(os.Getenv("NULL_SPACE_LOG_LEVEL")))

	var (
		output io.Writer = io.Discard
		closer io.Closer
	)

	if logFilePath != "" {
		logFilePath = stampedPath(logFilePath)

		if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
			return nil, err
		}

		file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
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

// stampedPath inserts a timestamp into the log file path so each run gets its
// own file. "dist/logs/server.log" → "dist/logs/server-20060102-150405.log".
func stampedPath(path string) string {
	stamp := time.Now().Format("20060102-150405")
	ext := filepath.Ext(path)
	if ext != "" {
		return strings.TrimSuffix(path, ext) + "-" + stamp + ext
	}
	return path + "-" + stamp
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
