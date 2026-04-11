package runlog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GlobalLevel is the runtime-adjustable log level for the system logger.
// It is initialised by Configure* and can be changed at runtime via SetLevel.
var GlobalLevel = new(slog.LevelVar)

// SetLevel sets the system log level at runtime.
func SetLevel(level slog.Level) { GlobalLevel.Set(level) }

// GetLevel returns the current system log level.
func GetLevel() slog.Level { return GlobalLevel.Level() }

// ConfigureFromEnv sets up slog from DEV_NULL_LOG_FILE / DEV_NULL_LOG_LEVEL.
// If DEV_NULL_LOG_FILE is not set, logs are discarded.
func ConfigureFromEnv(component string) (func() error, error) {
	logFilePath := strings.TrimSpace(os.Getenv("DEV_NULL_LOG_FILE"))
	level := parseLevel(strings.TrimSpace(os.Getenv("DEV_NULL_LOG_LEVEL")))
	return configure(logFilePath, level, component)
}

// ConfigureAuto sets up slog with an automatic timestamped log file in logsDir.
// The log file is named <component>-<timestamp>.log.
// DEV_NULL_LOG_FILE overrides the auto path. DEV_NULL_LOG_LEVEL sets the level.
func ConfigureAuto(logsDir, component string) (func() error, error) {
	logFilePath := strings.TrimSpace(os.Getenv("DEV_NULL_LOG_FILE"))
	if logFilePath == "" {
		ts := time.Now().Format("20060102-150405")
		logFilePath = filepath.Join(logsDir, fmt.Sprintf("%s-%s.log", component, ts))
	}
	level := parseLevel(strings.TrimSpace(os.Getenv("DEV_NULL_LOG_LEVEL")))
	return configure(logFilePath, level, component)
}

func configure(logFilePath string, level slog.Level, component string) (func() error, error) {
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
		slog.Info("logging to file", "path", logFilePath)
	}

	GlobalLevel.Set(level)
	handler := slog.NewTextHandler(output, &slog.HandlerOptions{
		Level:     GlobalLevel,
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
