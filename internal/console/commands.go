package console

import (
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/localcmd"
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

// submitInput is called by NCTextInput.OnSubmit when Enter is pressed.
// text is already trimmed and non-empty; input is already cleared; history is already added.
func (m *Model) submitInput(text string) {
	// Reset tab completion on submit.
	m.tabCandidates = nil

	// Echo the command to the log so the user sees what was typed.
	m.appendLog("> " + text)

	if !strings.HasPrefix(text, "/") {
		m.appendLog("Type /help for available commands.")
		return
	}
	if strings.HasPrefix(text, "/plugin") {
		m.handlePluginCommand(text)
		return
	}
	if strings.HasPrefix(text, "/theme") {
		m.handleThemeCommand(text)
		return
	}
	if strings.HasPrefix(text, "/shader") {
		m.handleShaderCommand(text)
		return
	}
	ctx := domain.CommandContext{
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
	}
	m.api.DispatchCommand(text, ctx)
}

func (m *Model) handleThemeCommand(input string) {
	t, name := localcmd.HandleTheme(input, m.api.DataDir(), m.theme.Name, m.appendLog)
	if t != nil {
		m.theme, m.themeName = t, name
		m.persistServerConfig()
	}
}

func (m *Model) handlePluginCommand(input string) {
	p, n, changed := localcmd.HandlePlugin(input, m.api.DataDir(), m.api.Clock(),
		m.plugins, m.pluginNames, m.appendLog)
	m.plugins, m.pluginNames = p, n
	if changed {
		m.persistServerConfig()
	}
}

// dispatchPluginReply handles a string returned by a console plugin's onMessage hook.
// If it starts with "/" it's treated as a command, otherwise logged as info.
func (m *Model) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		ctx := domain.CommandContext{
			PlayerID:  "",
			IsConsole: true,
			IsAdmin:   true,
			Reply: func(s string) {
				m.appendLog(s)
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(domain.Message{Text: s, IsFromPlugin: true})
			},
			ServerLog: func(s string) {
				m.appendLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	// Plain text from console plugin -> broadcast as admin chat.
	m.api.BroadcastChat(domain.Message{Author: "admin", Text: text, IsFromPlugin: true})
}

func (m *Model) handleShaderCommand(input string) {
	s, n, changed := localcmd.HandleShader(input, m.api.DataDir(), m.api.Clock(),
		m.shaders, m.shaderNames, m.appendLog)
	m.shaders, m.shaderNames = s, n
	if changed {
		m.persistServerConfig()
	}
}
