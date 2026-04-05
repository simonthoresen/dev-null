package chrome

import (
	"fmt"
	"path/filepath"
	"strings"

	"null-space/internal/domain"
	"null-space/internal/engine"
	"null-space/internal/theme"
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
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := theme.ListThemes(m.api.DataDir())
		if len(available) == 0 {
			m.pluginReply("No themes found in themes/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if strings.EqualFold(name, m.theme.Name) {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available themes:\n" + strings.Join(lines, "\n"))
		return
	}
	name := parts[1]
	path := filepath.Join(m.api.DataDir(), "themes", name+".json")
	t, err := theme.Load(path)
	if err != nil {
		m.pluginReply(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.themeName = name
	m.gameWindow = nil // force rebuild with new theme
	m.pluginReply(fmt.Sprintf("Theme changed to: %s", t.Name))
	m.persistClientConfig()
}

func (m *Model) pluginReply(text string) {
	msg := domain.Message{IsReply: true, IsPrivate: true, ToID: m.playerID, Text: text}
	m.api.SendToPlayer(m.playerID, domain.ChatMsg{Msg: msg})
}

func (m *Model) handlePluginCommand(input string) {
	parts := strings.Fields(input)
	// /plugin with no args -> list
	if len(parts) <= 1 {
		available := engine.ListScripts(filepath.Join(m.api.DataDir(), "plugins"))
		loadedSet := make(map[string]bool)
		for _, n := range m.pluginNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.pluginNames) == 0 {
			m.pluginReply("No plugins found in plugins/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [loaded]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available plugins:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.pluginReply("Usage: /plugin load <name|url>")
			return
		}
		nameOrURL := parts[2]
		name, path, err := engine.ResolvePluginPath(nameOrURL, m.api.DataDir())
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.pluginNames {
			if strings.EqualFold(n, name) {
				m.pluginReply(fmt.Sprintf("Plugin '%s' is already loaded.", name))
				return
			}
		}
		pl, err := engine.LoadPlugin(path, m.api.Clock())
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed to load plugin: %v", err))
			return
		}
		m.plugins = append(m.plugins, pl)
		m.pluginNames = append(m.pluginNames, name)
		m.pluginReply(fmt.Sprintf("Plugin loaded: %s", name))
		m.persistClientConfig()
	case "unload":
		if len(parts) < 3 {
			m.pluginReply("Usage: /plugin unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.pluginNames {
			if strings.EqualFold(n, target) {
				m.plugins[i].Unload()
				m.plugins = append(m.plugins[:i], m.plugins[i+1:]...)
				m.pluginNames = append(m.pluginNames[:i], m.pluginNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.pluginReply(fmt.Sprintf("Plugin '%s' is not loaded.", target))
			return
		}
		m.pluginReply(fmt.Sprintf("Plugin unloaded: %s", target))
		m.persistClientConfig()
	case "list":
		if len(m.pluginNames) == 0 {
			m.pluginReply("No plugins currently loaded.")
			return
		}
		m.pluginReply("Loaded plugins: " + strings.Join(m.pluginNames, ", "))
	default:
		m.pluginReply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", parts[1]))
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
	parts := strings.Fields(input)
	// /shader with no args -> list
	if len(parts) <= 1 {
		available := engine.ListScripts(filepath.Join(m.api.DataDir(), "shaders"))
		loadedSet := make(map[string]bool)
		for _, n := range m.shaderNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.shaderNames) == 0 {
			m.pluginReply("No shaders found in shaders/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.pluginReply("Available shaders:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader load <name|url>")
			return
		}
		nameOrURL := parts[2]
		name, path, err := engine.ResolveShaderPath(nameOrURL, m.api.DataDir())
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.shaderNames {
			if strings.EqualFold(n, name) {
				m.pluginReply(fmt.Sprintf("Shader '%s' is already loaded.", name))
				return
			}
		}
		sh, err := engine.LoadShader(path, m.api.Clock())
		if err != nil {
			m.pluginReply(fmt.Sprintf("Failed to load shader: %v", err))
			return
		}
		m.shaders = append(m.shaders, sh)
		m.shaderNames = append(m.shaderNames, name)
		m.pluginReply(fmt.Sprintf("Shader loaded: %s", name))
		m.persistClientConfig()
	case "unload":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.shaderNames {
			if strings.EqualFold(n, target) {
				m.shaders[i].Unload()
				m.shaders = append(m.shaders[:i], m.shaders[i+1:]...)
				m.shaderNames = append(m.shaderNames[:i], m.shaderNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.pluginReply(fmt.Sprintf("Shader '%s' is not loaded.", target))
			return
		}
		m.pluginReply(fmt.Sprintf("Shader unloaded: %s", target))
		m.persistClientConfig()
	case "list":
		if len(m.shaderNames) == 0 {
			m.pluginReply("No shaders currently loaded.")
			return
		}
		var lines []string
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		m.pluginReply("Active shaders (in order):\n" + strings.Join(lines, "\n"))
	case "up":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader up <name>")
			return
		}
		m.moveShader(parts[2], -1)
	case "down":
		if len(parts) < 3 {
			m.pluginReply("Usage: /shader down <name>")
			return
		}
		m.moveShader(parts[2], +1)
	default:
		m.pluginReply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list, up, down", parts[1]))
	}
}

func (m *Model) moveShader(name string, delta int) {
	idx := -1
	for i, n := range m.shaderNames {
		if strings.EqualFold(n, name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.pluginReply(fmt.Sprintf("Shader '%s' is not loaded.", name))
		return
	}
	newIdx := idx + delta
	if newIdx < 0 || newIdx >= len(m.shaderNames) {
		m.pluginReply(fmt.Sprintf("Shader '%s' is already at position %d.", name, idx+1))
		return
	}
	m.shaders[idx], m.shaders[newIdx] = m.shaders[newIdx], m.shaders[idx]
	m.shaderNames[idx], m.shaderNames[newIdx] = m.shaderNames[newIdx], m.shaderNames[idx]
	m.pluginReply(fmt.Sprintf("Shader '%s' moved to position %d.", name, newIdx+1))
}
