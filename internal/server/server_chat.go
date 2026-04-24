package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/console"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/widget"
)

// --- Broadcast and messaging ---

// playerSend wraps a tea.Program with a buffered channel and a single drain
// goroutine. This eliminates the per-broadcast goroutine spawned by the old
// safeSend pattern (160+ goroutines/sec at 16 players × 10 ticks/sec).
type playerSend struct {
	ch chan tea.Msg
}

func newPlayerSend(prog *tea.Program) *playerSend {
	ps := &playerSend{ch: make(chan tea.Msg, 16)}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("playerSend drain panic", "panic", r)
			}
		}()
		for msg := range ps.ch {
			prog.Send(msg)
		}
	}()
	return ps
}

func (ps *playerSend) Send(msg tea.Msg) {
	select {
	case ps.ch <- msg:
	default:
		// Channel full: drop TickMsg silently (next one arrives in ~100ms).
		// Log a warning for other message types so operator can detect a slow client.
		if _, isTick := msg.(domain.TickMsg); !isTick {
			slog.Warn("player send queue full, dropping message", "type", fmt.Sprintf("%T", msg))
		}
	}
}

func (ps *playerSend) close() {
	close(ps.ch)
}

// broadcastRepaint sends a full-screen repaint to all player programs.
// This discards the renderer's diff cache, causing the next render to emit
// a complete frame — recovering from any accumulated display artifacts.
func (a *Server) broadcastRepaint() {
	a.programsMu.Lock()
	progs := make([]msgSender, len(a.programs))
	i := 0
	for _, p := range a.programs {
		progs[i] = p
		i++
	}
	a.programsMu.Unlock()
	msg := tea.ClearScreen()
	for _, p := range progs {
		p.Send(msg)
	}
}

func (a *Server) broadcastMsg(msg tea.Msg) {
	a.programsMu.Lock()
	progs := make([]msgSender, len(a.programs))
	i := 0
	for _, p := range a.programs {
		progs[i] = p
		i++
	}
	a.programsMu.Unlock()

	for _, p := range progs {
		p.Send(msg)
	}

	a.consoleProgramMu.Lock()
	cs := a.consoleSender
	a.consoleProgramMu.Unlock()
	if cs != nil {
		go func() {
			defer func() { recover() }()
			cs.Send(msg)
		}()
	}
}

// fontTagRe matches <font=name>text</font> tags in chat messages.
var fontTagRe = regexp.MustCompile(`<font=([^>]+)>([^<]*)</font>`)

// expandFontTags replaces <font=name>text</font> tags with figlet-rendered ASCII art.
func expandFontTags(text string) string {
	return fontTagRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := fontTagRe.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		fontName := groups[1]
		innerText := groups[2]
		if innerText == "" {
			return ""
		}
		rendered := engine.Figlet(innerText, fontName)
		if rendered == "" {
			return innerText // fallback to plain text
		}
		return strings.TrimRight(rendered, "\n")
	})
}

func (a *Server) broadcastChat(msg domain.Message) {
	start := time.Now()

	// Expand <font=name>text</font> tags before broadcasting.
	if msg.Text != "" {
		msg.Text = expandFontTags(msg.Text)
	}

	// Messages with no visible text (e.g. sound-only stop commands) skip history
	// and log storage but are still broadcast so graphical clients receive the OSC.
	if msg.Text != "" {
		a.state.AddChat(msg)

		// Log system messages to the file/console slog view.
		// Use NoBroadcastContext so the SlogHandler does not re-broadcast to players
		// (the message is already being delivered via broadcastChat).
		if msg.Author == "" {
			slog.InfoContext(console.NoBroadcastContext(), msg.Text)
		}

		// Write to chat log file.
		if a.chatLogFile != nil {
			ts := time.Now().Format(domain.TimeFormatDateTime)
			var line string
			switch {
			case msg.IsPrivate:
				line = fmt.Sprintf("%s [PM %s→%s] %s\n", ts, msg.FromID, msg.ToID, msg.Text)
			case msg.Author == "":
				line = fmt.Sprintf("%s [system] %s\n", ts, msg.Text)
			default:
				line = fmt.Sprintf("%s <%s> %s\n", ts, msg.Author, msg.Text)
			}
			if _, err := a.chatLogFile.WriteString(line); err != nil {
				slog.Debug("chat log write failed", "error", err)
			}
		}

		select {
		case a.chatCh <- msg:
		default:
		}
	}

	a.broadcastMsg(domain.ChatMsg{Msg: msg})
	if dur := time.Since(start); dur > 100*time.Millisecond {
		slog.Warn("broadcastChat slow", "duration", dur, "text", msg.Text)
	}
}

func (a *Server) sendToPlayer(playerID string, msg tea.Msg) {
	a.programsMu.Lock()
	p := a.programs[playerID]
	a.programsMu.Unlock()
	if p != nil {
		p.Send(msg)
	}
}

// ShowDialog sends a modal dialog request to the specified player's TUI program.
func (a *Server) ShowDialog(playerID string, d domain.DialogRequest) {
	a.programsMu.Lock()
	prog := a.programs[playerID]
	a.programsMu.Unlock()
	if prog != nil {
		prog.Send(widget.ShowDialogMsg{Dialog: d})
	}
}

// serverLog logs an operator-facing line to the file/console but NOT to player chat.
// Use direct slog.Info for events that should also appear in player chat.
func (a *Server) serverLog(line string) {
	slog.InfoContext(console.NoBroadcastContext(), line)
}

// InstallConsoleSlogHandler wraps the current default slog handler to also
// route records to the server's slogCh. Call after server creation.
//
// Subtle: Go's slog.SetDefault also redirects log.Default() output back
// through the new slog handler. If the wrapped handler is Go's built-in
// defaultHandler (which writes via log.Default().Output()), this creates a
// cycle that deadlocks on the log package's internal mutex. To avoid this,
// we detect the defaultHandler case and replace it with a TextHandler that
// writes directly to os.Stderr, breaking the cycle.
func (a *Server) InstallConsoleSlogHandler() {
	existing := slog.Default().Handler()

	// Go's defaultHandler is unexported, so detect it by checking whether
	// the handler produces the standard-library log format (timestamp prefix).
	// A simpler approach: if the handler is NOT a known concrete type that
	// writes directly to an io.Writer (TextHandler, JSONHandler), replace it
	// with a TextHandler wrapping os.Stderr. In practice, runlog.ConfigureFromEnv
	// installs a TextHandler, so only the no-runlog case (client --local) hits this.
	switch existing.(type) {
	case *slog.TextHandler, *slog.JSONHandler:
		// Already a direct-writer handler — safe to wrap.
	default:
		// Likely the built-in defaultHandler. Replace with a direct stderr writer
		// to avoid the slog → defaultHandler → log.Default() → slog cycle.
		existing = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	handler := console.NewSlogHandler(a.slogCh, existing, func(msg string) {
		a.broadcastChat(domain.Message{Text: msg})
	})
	slog.SetDefault(slog.New(handler))
}

// --- Chat log ---

// OpenChatLog derives the chat log path from DEV_NULL_LOG_FILE by inserting
// "-chat" before the extension. E.g. "logs/20260401-152702.log" → "logs/20260401-152702-chat.log".
// If no log file is configured, no chat log is created.
func (a *Server) OpenChatLog() {
	serverLog := os.Getenv("DEV_NULL_LOG_FILE")
	if serverLog == "" {
		return
	}
	ext := filepath.Ext(serverLog)
	path := strings.TrimSuffix(serverLog, ext) + "-chat" + ext
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Warn("failed to open chat log", "path", path, "error", err)
		return
	}
	a.chatLogFile = f
	slog.Info("chat log opened", "path", path)
}

// CloseChatLog closes the chat log file.
func (a *Server) CloseChatLog() {
	if a.chatLogFile != nil {
		a.chatLogFile.Close()
		a.chatLogFile = nil
	}
}
