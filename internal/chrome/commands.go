package chrome

import (
	"os"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/localcmd"
)

// dispatchInput processes submitted text (commands, chat, plugins).
func (m *Model) dispatchInput(text string) {
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		cmd, arg := parseCommand(text)

		// Local commands (per-player, not in server registry).
		switch cmd {
		case "/theme-list":
			localcmd.HandleThemeList(m.api.DataDir(), m.theme.Name, m.pluginReply)
			return
		case "/theme-load":
			if arg == "" {
				m.pluginReply("Usage: /theme-load <name>")
				return
			}
			if t, name := localcmd.HandleThemeLoad(arg, m.api.DataDir(), m.pluginReply); t != nil {
				m.theme, m.themeName, m.gameWindow = t, name, nil
				m.persistClientConfig()
			}
			return
		case "/plugin-list":
			localcmd.HandlePluginList(m.api.DataDir(), m.pluginNames, m.pluginReply)
			return
		case "/plugin-load":
			if arg == "" {
				m.pluginReply("Usage: /plugin-load <name>")
				return
			}
			p, n, changed := localcmd.HandlePluginLoad(arg, m.api.DataDir(), m.api.Clock(),
				m.plugins, m.pluginNames, m.pluginReply)
			m.plugins, m.pluginNames = p, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/plugin-unload":
			if arg == "" {
				m.pluginReply("Usage: /plugin-unload <name>")
				return
			}
			p, n, changed := localcmd.HandlePluginUnload(arg, m.plugins, m.pluginNames, m.pluginReply)
			m.plugins, m.pluginNames = p, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/shader-list":
			localcmd.HandleShaderList(m.api.DataDir(), m.shaderNames, m.pluginReply)
			return
		case "/shader-load":
			if arg == "" {
				m.pluginReply("Usage: /shader-load <name>")
				return
			}
			s, n, changed := localcmd.HandleShaderLoad(arg, m.api.DataDir(), m.api.Clock(),
				m.shaders, m.shaderNames, m.pluginReply)
			m.shaders, m.shaderNames = s, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/shader-unload":
			if arg == "" {
				m.pluginReply("Usage: /shader-unload <name>")
				return
			}
			s, n, changed := localcmd.HandleShaderUnload(arg, m.shaders, m.shaderNames, m.pluginReply)
			m.shaders, m.shaderNames = s, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/shader-up":
			if arg == "" {
				m.pluginReply("Usage: /shader-up <name>")
				return
			}
			s, n, changed := localcmd.HandleShaderUp(arg, m.shaders, m.shaderNames, m.pluginReply)
			m.shaders, m.shaderNames = s, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/shader-down":
			if arg == "" {
				m.pluginReply("Usage: /shader-down <name>")
				return
			}
			s, n, changed := localcmd.HandleShaderDown(arg, m.shaders, m.shaderNames, m.pluginReply)
			m.shaders, m.shaderNames = s, n
			if changed {
				m.persistClientConfig()
			}
			return
		case "/synth-list":
			m.handleSynthList()
			return
		case "/synth-load":
			if arg == "" {
				m.pluginReply("Usage: /synth-load <name>")
				return
			}
			m.handleSynthLoad(arg)
			return
		// Graphics preference commands.
		case "/render-ascii":
			m.setGraphicsPref(domain.RenderModeAscii)
			m.persistClientConfig()
			return
		case "/render-blocks":
			m.setGraphicsPref(domain.RenderModeBlocks)
			m.persistClientConfig()
			return
		case "/render-pixels":
			m.setGraphicsPref(domain.RenderModePixels)
			m.persistClientConfig()
			return
		}

		// Server-side commands via registry.
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
			Clipboard: func(s string) {
				m.pendingClipboard = s
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	// Regular chat.
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

func (m *Model) pluginReply(text string) {
	msg := domain.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: text}
	m.api.SendToPlayer(m.playerID, domain.ChatMsg{Msg: msg})
}

// dispatchPluginReply handles a string returned by a plugin's onMessage hook.
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

// handleSynthList lists available SoundFonts.
func (m *Model) handleSynthList() {
	sf2Dir := filepath.Join(m.api.DataDir(), "soundfonts")
	entries, _ := os.ReadDir(sf2Dir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sf2") {
			name := strings.TrimSuffix(e.Name(), ".sf2")
			tag := ""
			if name == m.synthName {
				tag = " [active]"
			}
			names = append(names, name+tag)
		}
	}
	if len(names) == 0 {
		m.pluginReply("No SoundFonts found in soundfonts/")
	} else {
		m.pluginReply("SoundFonts: " + strings.Join(names, ", "))
	}
}

// handleSynthLoad selects a SoundFont by name.
func (m *Model) handleSynthLoad(name string) {
	sf2Dir := filepath.Join(m.api.DataDir(), "soundfonts")
	sf2Path := filepath.Join(sf2Dir, name+".sf2")
	if _, err := os.Stat(sf2Path); err != nil {
		m.pluginReply("SoundFont not found: " + name)
		return
	}
	m.synthName = name
	m.synthSent = false
	m.persistClientConfig()
	m.pluginReply("SoundFont: " + name)
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
