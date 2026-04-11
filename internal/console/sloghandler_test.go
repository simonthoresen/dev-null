package console

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// discardHandler is a slog.Handler that discards all records (for testing).
type discardHandler struct{}

func (h *discardHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *discardHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (h *discardHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *discardHandler) WithGroup(_ string) slog.Handler               { return h }

func TestSlogDebugRoutedToConsole(t *testing.T) {
	ch := make(chan SlogLine, 10)
	wrapped := &discardHandler{}
	handler := NewSlogHandler(ch, wrapped, nil)

	ctx := t.Context()

	// Debug messages should appear in channel.
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "plugin loaded: greeter", 0)
	_ = handler.Handle(ctx, rec)

	// INFO message should also appear.
	rec2 := slog.NewRecord(time.Now(), slog.LevelInfo, "server started", 0)
	_ = handler.Handle(ctx, rec2)

	close(ch)
	var messages []string
	for sl := range ch {
		messages = append(messages, sl.Text)
	}

	if len(messages) != 2 {
		t.Errorf("expected 2 messages in console channel, got %d: %v", len(messages), messages)
	}
}

func TestSlogBlockedInRenderPath(t *testing.T) {
	ch := make(chan SlogLine, 10)
	wrapped := &discardHandler{}
	handler := NewSlogHandler(ch, wrapped, nil)

	ctx := t.Context()

	// Normal call — should appear in channel.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "normal log", 0)
	_ = handler.Handle(ctx, rec)

	// Simulate a call from inside a Render method.
	renderHelper := func() {
		rec2 := slog.NewRecord(time.Now(), slog.LevelInfo, "render log", 0)
		_ = handler.Handle(ctx, rec2)
	}
	// Simulate being inside a View/Render cycle.
	EnterRenderPath()
	renderHelper()
	LeaveRenderPath()

	close(ch)
	var messages []string
	for sl := range ch {
		messages = append(messages, sl.Text)
	}

	if len(messages) != 1 {
		t.Errorf("expected 1 message (render-path one blocked), got %d: %v", len(messages), messages)
	}
	if len(messages) > 0 && strings.Contains(messages[0], "render log") {
		t.Error("render-path message should have been blocked")
	}
}
