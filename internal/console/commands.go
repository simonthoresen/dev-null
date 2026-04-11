package console

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/localcmd"
	"dev-null/internal/render"
	"dev-null/internal/runlog"
)

// tabComplete handles tab completion for the input control.
func (m *Model) tabComplete(current string) (string, bool) {
	if !strings.HasPrefix(current, "/") {
		return "", false
	}
	if m.tabCandidates == nil {
		m.tabPrefix, m.tabCandidates = m.api.TabCandidates(current, m.api.State().PlayerNames())
		m.tabIndex = 0
	}
	if len(m.tabCandidates) == 0 {
		return "", false
	}
	result := m.tabPrefix + m.tabCandidates[m.tabIndex]
	m.tabIndex = (m.tabIndex + 1) % len(m.tabCandidates)
	return result, true
}

// consoleCtx returns a CommandContext for the server console.
func (m *Model) consoleCtx() domain.CommandContext {
	return domain.CommandContext{
		PlayerID:  "",
		IsConsole: true,
		IsAdmin:   true,
		Reply: func(s string) {
			m.appendLog(s)
		},
		Broadcast: func(s string) {
			m.api.BroadcastChat(domain.Message{Text: s})
		},
		ServerLog: func(s string) {
			m.appendLog(s)
		},
		Clipboard: func(s string) {
			m.pendingClipboard = s
			// Write OSC 52 to stdout immediately for TUI mode.
			// In GUI mode stdout isn't a terminal, but PopClipboard() handles it.
			fmt.Fprint(os.Stdout, render.EncodeOSC52(s))
		},
	}
}

// submitInput is called by NCTextInput.OnSubmit when Enter is pressed.
func (m *Model) submitInput(text string) {
	m.tabCandidates = nil

	m.appendLog("> " + text)

	if !strings.HasPrefix(text, "/") {
		m.appendLog("Type /help for available commands.")
		return
	}

	cmd, arg := parseCommand(text)

	// Local commands (per-console, not in server registry).
	switch cmd {
	case "/theme-list":
		localcmd.HandleThemeList(m.api.DataDir(), m.theme.Name, m.appendLog)
		return
	case "/theme-load":
		if arg == "" {
			m.appendLog("Usage: /theme-load <name>")
			return
		}
		if t, name := localcmd.HandleThemeLoad(arg, m.api.DataDir(), m.appendLog); t != nil {
			m.theme, m.themeName = t, name
			m.persistServerConfig()
		}
		return
	case "/plugin-list":
		localcmd.HandlePluginList(m.api.DataDir(), m.pluginNames, m.appendLog)
		return
	case "/plugin-load":
		if arg == "" {
			m.appendLog("Usage: /plugin-load <name>")
			return
		}
		p, n, changed := localcmd.HandlePluginLoad(arg, m.api.DataDir(), m.api.Clock(),
			m.plugins, m.pluginNames, m.appendLog)
		m.plugins, m.pluginNames = p, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/plugin-unload":
		if arg == "" {
			m.appendLog("Usage: /plugin-unload <name>")
			return
		}
		p, n, changed := localcmd.HandlePluginUnload(arg, m.plugins, m.pluginNames, m.appendLog)
		m.plugins, m.pluginNames = p, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/shader-list":
		localcmd.HandleShaderList(m.api.DataDir(), m.shaderNames, m.appendLog)
		return
	case "/shader-load":
		if arg == "" {
			m.appendLog("Usage: /shader-load <name>")
			return
		}
		s, n, changed := localcmd.HandleShaderLoad(arg, m.api.DataDir(), m.api.Clock(),
			m.shaders, m.shaderNames, m.appendLog)
		m.shaders, m.shaderNames = s, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/shader-unload":
		if arg == "" {
			m.appendLog("Usage: /shader-unload <name>")
			return
		}
		s, n, changed := localcmd.HandleShaderUnload(arg, m.shaders, m.shaderNames, m.appendLog)
		m.shaders, m.shaderNames = s, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/shader-up":
		if arg == "" {
			m.appendLog("Usage: /shader-up <name>")
			return
		}
		s, n, changed := localcmd.HandleShaderUp(arg, m.shaders, m.shaderNames, m.appendLog)
		m.shaders, m.shaderNames = s, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/shader-down":
		if arg == "" {
			m.appendLog("Usage: /shader-down <name>")
			return
		}
		s, n, changed := localcmd.HandleShaderDown(arg, m.shaders, m.shaderNames, m.appendLog)
		m.shaders, m.shaderNames = s, n
		if changed {
			m.persistServerConfig()
		}
		return
	case "/set-log-level":
		if arg == "" {
			m.appendLog(fmt.Sprintf("log level: %s", logLevelName(runlog.GetLevel())))
			return
		}
		level, ok := parseLogLevel(arg)
		if !ok {
			m.appendLog("Usage: /set-log-level <debug|info|warn|error>")
			return
		}
		runlog.SetLevel(level)
		m.appendLog(fmt.Sprintf("log level set to: %s", logLevelName(level)))
		return

	case "/show-log-level":
		if arg == "" {
			m.appendLog(fmt.Sprintf("show level: %s", logLevelName(m.showLevel)))
			return
		}
		level, ok := parseLogLevel(arg)
		if !ok {
			m.appendLog("Usage: /show-log-level <debug|info|warn|error>")
			return
		}
		m.showLevel = level
		m.rebuildVisibleLines()
		m.appendLog(fmt.Sprintf("show level set to: %s", logLevelName(level)))
		return
	}

	// Server-side commands via registry.
	m.api.DispatchCommand(text, m.consoleCtx())
}

// dispatchPluginReply handles a string returned by a console plugin's onMessage hook.
func (m *Model) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		ctx := m.consoleCtx()
		ctx.Broadcast = func(s string) {
			m.api.BroadcastChat(domain.Message{Text: s, IsFromPlugin: true})
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	m.api.BroadcastChat(domain.Message{Author: "admin", Text: text, IsFromPlugin: true})
}

// parseCommand splits "/cmd-name arg1 arg2" into ("/cmd-name", "arg1 arg2").
func parseCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	idx := strings.IndexByte(text, ' ')
	if idx < 0 {
		return text, ""
	}
	return text[:idx], strings.TrimSpace(text[idx+1:])
}

// parseLogLevel converts a level name to a slog.Level.
func parseLogLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return 0, false
}

// logLevelName returns the canonical short name for a slog.Level.
func logLevelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "error"
	case level >= slog.LevelWarn:
		return "warn"
	case level >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}
