package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"null-space/internal/console"
	"null-space/internal/domain"
	"null-space/internal/widget"
)

// --- Broadcast and messaging ---

func (a *Server) broadcastMsg(msg tea.Msg) {
	a.programsMu.Lock()
	progs := make([]*tea.Program, 0, len(a.programs))
	for _, p := range a.programs {
		progs = append(progs, p)
	}
	a.programsMu.Unlock()

	for _, p := range progs {
		go p.Send(msg)
	}

	a.consoleProgramMu.Lock()
	cp := a.consoleProgram
	a.consoleProgramMu.Unlock()
	if cp != nil {
		go cp.Send(msg)
	}
}

func (a *Server) broadcastChat(msg domain.Message) {
	start := time.Now()
	a.state.AddChat(msg)

	// Write to chat log file.
	if a.chatLogFile != nil {
		ts := time.Now().Format("2006-01-02 15:04:05")
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
		go p.Send(msg)
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

// OpenChatLog derives the chat log path from NULL_SPACE_LOG_FILE by inserting
// "-chat" before the extension. E.g. "logs/20260401-152702.log" → "logs/20260401-152702-chat.log".
// If no log file is configured, no chat log is created.
func (a *Server) OpenChatLog() {
	serverLog := os.Getenv("NULL_SPACE_LOG_FILE")
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
