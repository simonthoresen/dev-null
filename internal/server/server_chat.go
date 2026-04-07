package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/console"
	"dev-null/internal/domain"
	"dev-null/internal/widget"
)

// --- Broadcast and messaging ---

// safeSend sends msg to s in a goroutine, recovering from any panic (e.g.
// if the program has already shut down).
func safeSend(s msgSender, msg tea.Msg) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Send panic", "panic", r)
			}
		}()
		s.Send(msg)
	}()
}

// broadcastRepaint sends a full-screen repaint to all player programs.
// This discards the renderer's diff cache, causing the next render to emit
// a complete frame — recovering from any accumulated display artifacts.
func (a *Server) broadcastRepaint() {
	a.programsMu.Lock()
	progs := make([]msgSender, 0, len(a.programs))
	for _, p := range a.programs {
		progs = append(progs, p)
	}
	a.programsMu.Unlock()
	for _, p := range progs {
		safeSend(p, tea.ClearScreen())
	}
}

func (a *Server) broadcastMsg(msg tea.Msg) {
	a.programsMu.Lock()
	progs := make([]msgSender, 0, len(a.programs))
	for _, p := range a.programs {
		progs = append(progs, p)
	}
	a.programsMu.Unlock()

	for _, p := range progs {
		safeSend(p, msg)
	}

	a.consoleProgramMu.Lock()
	cs := a.consoleSender
	a.consoleProgramMu.Unlock()
	if cs != nil {
		safeSend(cs, msg)
	}
}

func (a *Server) broadcastChat(msg domain.Message) {
	start := time.Now()

	// Messages with no visible text (e.g. sound-only stop commands) skip history
	// and log storage but are still broadcast so graphical clients receive the OSC.
	if msg.Text != "" {
		a.state.AddChat(msg)

		// Log system messages so they appear in the server log and console slog view.
		if msg.Author == "" {
			slog.Info(msg.Text)
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
		safeSend(p, msg)
	}
}

// ShowDialog sends a modal dialog request to the specified player's TUI program.
func (a *Server) ShowDialog(playerID string, d domain.DialogRequest) {
	a.programsMu.Lock()
	prog := a.programs[playerID]
	a.programsMu.Unlock()
	if prog != nil {
		safeSend(prog, widget.ShowDialogMsg{Dialog: d})
	}
}

func (a *Server) serverLog(line string) {
	slog.Info(line)
}

// InstallConsoleSlogHandler wraps the current default slog handler to also
// route records to the server's slogCh. Call after server creation.
func (a *Server) InstallConsoleSlogHandler() {
	existing := slog.Default().Handler()
	handler := console.NewSlogHandler(a.slogCh, existing)
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
