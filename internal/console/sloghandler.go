package console

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// inRenderPath is an atomic counter incremented when entering View/Render
// and decremented on exit. When > 0, the console slog handler suppresses
// messages to avoid the feedback loop: View -> slog -> console -> Update -> View.
//
// This is safe across goroutines: each BubbleTea program runs View on its
// own goroutine. Multiple programs rendering concurrently all increment the
// counter, and any slog call sees counter > 0 and skips the console channel.
// False positives (a non-render goroutine seeing counter > 0 because another
// goroutine is rendering) are harmless — the message still goes to the file
// log, it's just not duplicated to the console display.
var inRenderPath atomic.Int32

// EnterRenderPath marks the current execution as inside a View/Render cycle.
// Call with defer LeaveRenderPath() at the top of View and Render methods.
func EnterRenderPath() { inRenderPath.Add(1) }

// LeaveRenderPath unmarks the render cycle.
func LeaveRenderPath() { inRenderPath.Add(-1) }

// LogCategory tags console log lines for filtering.
type LogCategory int

const (
	CatDebug   LogCategory = iota
	CatInfo
	CatWarn
	CatError
	CatChat    // player chat, system messages
	CatCommand // "> /help" echo and command replies
)

// SlogLine carries a formatted slog record to the console.
type SlogLine struct {
	Cat  LogCategory
	Text string
}

// SlogHandler is a slog.Handler that routes log records to the
// console's slogCh channel, formatted with timestamp and level prefix.
// It also forwards to a wrapped handler (the file handler) so logs go
// to both the console and the log file.
type SlogHandler struct {
	ch      chan<- SlogLine
	wrapped slog.Handler
	attrs   []slog.Attr
	group   string
}

// NewSlogHandler wraps an existing handler and tees records to ch.
func NewSlogHandler(ch chan<- SlogLine, wrapped slog.Handler) slog.Handler {
	return &SlogHandler{ch: ch, wrapped: wrapped}
}

func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

func (h *SlogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always forward to the wrapped handler (file) first.
	err := h.wrapped.Handle(ctx, r)

	// Don't route to the console channel if we're inside a View/Render call.
	// Sending to the channel triggers Update -> View -> more slog calls -> loop.
	if inRenderPath.Load() > 0 {
		return err
	}

	cat := slogLevelToCategory(r.Level)
	prefix := slogLevelPrefix(r.Level)
	ts := r.Time.Format("15:04:05")

	var attrs string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "component" && a.Key != "pid" {
			attrs += " " + a.Key + "=" + a.Value.String()
		}
		return true
	})

	text := fmt.Sprintf("%s %s %s%s", ts, prefix, r.Message, attrs)

	select {
	case h.ch <- SlogLine{Cat: cat, Text: text}:
	default:
	}

	return err
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogHandler{
		ch:      h.ch,
		wrapped: h.wrapped.WithAttrs(attrs),
		attrs:   append(h.attrs, attrs...),
		group:   h.group,
	}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	return &SlogHandler{
		ch:      h.ch,
		wrapped: h.wrapped.WithGroup(name),
		attrs:   h.attrs,
		group:   name,
	}
}

func slogLevelToCategory(level slog.Level) LogCategory {
	switch {
	case level >= slog.LevelError:
		return CatError
	case level >= slog.LevelWarn:
		return CatWarn
	case level >= slog.LevelInfo:
		return CatInfo
	default:
		return CatDebug
	}
}

func slogLevelPrefix(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "[ERR]"
	case level >= slog.LevelWarn:
		return "[WRN]"
	case level >= slog.LevelInfo:
		return "[INF]"
	default:
		return "[DBG]"
	}
}
