package chrome

import (
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/localcmd"
)

// dispatchInput processes submitted text (commands, chat, plugins).
// Called by both the lobby and playing NCCommandInput controls.
func (m *Model) dispatchInput(text string) {
	if text == "" {
		return
	}
	// Handle /plugin and /theme locally (per-player).
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
	if strings.HasPrefix(text, "/") {
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		ctx := domain.CommandContext{
			PlayerID: m.playerID,
			IsAdmin:  isAdmin,
			Reply: func(s string) {
				msg := domain.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: s}
				m.api.SendToPlayer(m.playerID, domain.ChatMsg{Msg: msg})
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(domain.Message{Text: s})
			},
			ServerLog: func(s string) {
				m.api.ServerLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	// Regular chat
	playerName := "unknown"
	if p := m.api.State().GetPlayer(m.playerID); p != nil {
		playerName = p.Name
	}
	m.api.BroadcastChat(domain.Message{Author: playerName, Text: text})
}

// lobbyTabComplete provides tab completion for the lobby command input.
func (m *Model) lobbyTabComplete(current string) (string, bool) {
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

func (m *Model) handleThemeCommand(input string) {
	t, name := localcmd.HandleTheme(input, m.api.DataDir(), m.theme.Name, m.pluginReply)
	if t != nil {
		m.theme, m.themeName, m.gameWindow = t, name, nil
		m.persistClientConfig()
	}
}

func (m *Model) pluginReply(text string) {
	msg := domain.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: text}
	m.api.SendToPlayer(m.playerID, domain.ChatMsg{Msg: msg})
}

func (m *Model) handlePluginCommand(input string) {
	p, n, changed := localcmd.HandlePlugin(input, m.api.DataDir(), m.api.Clock(),
		m.plugins, m.pluginNames, m.pluginReply)
	m.plugins, m.pluginNames = p, n
	if changed {
		m.persistClientConfig()
	}
}

// dispatchPluginReply handles a string returned by a plugin's onMessage hook.
// If it starts with "/" it's treated as a command, otherwise as chat.
func (m *Model) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		ctx := domain.CommandContext{
			PlayerID: m.playerID,
			IsAdmin:  isAdmin,
			Reply: func(s string) {
				msg := domain.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: s}
				m.api.SendToPlayer(m.playerID, domain.ChatMsg{Msg: msg})
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(domain.Message{Text: s, IsFromPlugin: true})
			},
			ServerLog: func(s string) {
				m.api.ServerLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	playerName := "unknown"
	if p := m.api.State().GetPlayer(m.playerID); p != nil {
		playerName = p.Name
	}
	m.api.BroadcastChat(domain.Message{Author: playerName, Text: text, IsFromPlugin: true})
}

func (m *Model) handleShaderCommand(input string) {
	s, n, changed := localcmd.HandleShader(input, m.api.DataDir(), m.api.Clock(),
		m.shaders, m.shaderNames, m.pluginReply)
	m.shaders, m.shaderNames = s, n
	if changed {
		m.persistClientConfig()
	}
}
