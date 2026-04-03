package chrome

import (
	"fmt"
	"path/filepath"
	"strings"

	"null-space/internal/domain"
	"null-space/internal/engine"
)

// invalidateMenuCache forces the next cachedMenus() call to rebuild.
func (m *Model) invalidateMenuCache() {
	m.menuCache = nil
}

// cachedMenus returns the menu tree, rebuilding only when the active game has changed.
func (m *Model) cachedMenus() []domain.MenuDef {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	m.api.State().RUnlock()

	if m.menuCache != nil && m.menuCacheGame == game {
		return m.menuCache
	}

	fileItems := []domain.MenuItemDef{
		{Label: "&Games...", Handler: func(_ string) { m.showGamesDialog() }},
		{Label: "---"},
		{Label: "&Themes...", Handler: func(_ string) { m.showPlayerListDialog("Themes", "themes", ".json") }},
		{Label: "&Plugins...", Handler: func(_ string) { m.showPlayerListDialog("Plugins", "plugins", ".js") }},
		{Label: "&Shaders...", Handler: func(_ string) { m.showShaderDialog() }},
		{Label: "---"},
	}
	if m.IsLocal {
		fileItems = append(fileItems, domain.MenuItemDef{
			Label: "&Quit",
			Handler: func(_ string) {
				// Ctrl+C is the reliable quit path in local mode.
			},
		})
	} else {
		fileItems = append(fileItems, domain.MenuItemDef{
			Label: "&Disconnect",
			Handler: func(playerID string) {
				go m.api.KickPlayer(playerID)
			},
		})
	}
	menus := []domain.MenuDef{{Label: "&File", Items: fileItems}}
	if game != nil {
		menus = append(menus, game.Menus()...)
	}
	menus = append(menus, domain.MenuDef{
		Label: "&Help",
		Items: []domain.MenuItemDef{
			{Label: "&About...", Handler: func(_ string) {
				m.overlay.PushDialog(domain.DialogRequest{
					Title:   "About",
					Body:    engine.AboutLogo(),
					Buttons: []string{"OK"},
				})
			}},
		},
	})
	m.menuCache = menus
	m.menuCacheGame = game
	return menus
}

func (m *Model) showGamesDialog() {
	m.api.State().RLock()
	player := m.api.State().Players[m.playerID]
	phase := m.api.State().GamePhase
	gameName := m.api.State().GameName
	m.api.State().RUnlock()

	isAdmin := player != nil && player.IsAdmin
	available := engine.ListGames(filepath.Join(m.api.DataDir(), "games"))
	saves := m.api.ListSuspends()

	var lines []string

	// Current game status.
	if phase != domain.PhaseNone && gameName != "" {
		status := "playing"
		if phase == domain.PhaseSuspended {
			status = "suspended"
		}
		lines = append(lines, fmt.Sprintf("Current: %s (%s)", gameName, status))
		lines = append(lines, "")
	}

	// Available games.
	lines = append(lines, "Available games:")
	if len(available) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, name := range available {
			lines = append(lines, "  "+name)
		}
	}

	// Suspended saves.
	if len(saves) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Suspended:")
		teamCount := m.api.State().TeamCount()
		for _, s := range saves {
			teamNote := ""
			if s.TeamCount != teamCount {
				teamNote = fmt.Sprintf("  (lobby has %d teams)", teamCount)
			}
			lines = append(lines, fmt.Sprintf("  %s/%s  (%d teams, %s)%s",
				s.GameName, s.SaveName, s.TeamCount,
				s.SavedAt.Format(domain.TimeFormatShort), teamNote))
		}
	}

	if isAdmin {
		lines = append(lines, "")
		lines = append(lines, "Use /game load|unload|suspend|resume")
	}

	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Games",
		Body:    strings.Join(lines, "\n"),
		Buttons: []string{"Close"},
	})
}

func (m *Model) showPlayerListDialog(title, subdir, ext string) {
	dir := filepath.Join(m.api.DataDir(), subdir)
	items := engine.ListDir(dir, ext)
	body := "(empty)"
	if len(items) > 0 {
		var lines []string
		for _, name := range items {
			lines = append(lines, "  "+name)
		}
		body = strings.Join(lines, "\n")
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   title,
		Body:    body,
		Buttons: []string{"Close"},
	})
}

func (m *Model) showShaderDialog() {
	m.pushShaderDialog()
}

func (m *Model) pushShaderDialog() {
	available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	// Build flat list: active shaders first (in order), then unloaded available ones.
	var items []string
	var tags []string
	for i, name := range m.shaderNames {
		items = append(items, fmt.Sprintf("%d. %s", i+1, name))
		tags = append(tags, "[active]")
	}
	for _, name := range available {
		if !loadedSet[name] {
			items = append(items, name)
			tags = append(tags, "")
		}
	}
	if len(items) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Shaders",
			Body:    "No shaders found in shaders/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(button string) {
				if button == "Add" {
					m.showShaderAddDialog()
				}
			},
		})
		return
	}

	activeCount := len(m.shaderNames)
	m.overlay.PushDialog(domain.DialogRequest{
		Title:    "Shaders",
		ListItems: items,
		ListTags:  tags,
		Buttons:  []string{"Add", "Remove", "Up", "Down", "Close"},
		OnListAction: func(button string, idx int) {
			switch button {
			case "Add":
				m.showShaderAddDialog()
			case "Remove":
				if idx < activeCount {
					name := m.shaderNames[idx]
					m.showShaderRemoveConfirm(name)
				} else {
					m.overlay.PushDialog(domain.DialogRequest{
						Title:   "Remove",
						Body:    "Only active shaders can be removed.",
						Buttons: []string{"OK"},
						OnClose: func(_ string) { m.pushShaderDialog() },
					})
				}
			case "Up":
				if idx > 0 && idx < activeCount {
					m.moveShader(m.shaderNames[idx], -1)
					m.pushShaderDialog()
				}
			case "Down":
				if idx >= 0 && idx < activeCount-1 {
					m.moveShader(m.shaderNames[idx], +1)
					m.pushShaderDialog()
				}
			case "Close", "":
				// done
			}
		},
	})
}

func (m *Model) showShaderAddDialog() {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Shader",
		Body:         "Enter a shader name or URL:",
		InputPrompt:  "Shader",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(button, value string) {
			if button == "Load" && strings.TrimSpace(value) != "" {
				m.handleShaderCommand("/shader load " + strings.TrimSpace(value))
			}
			m.pushShaderDialog()
		},
	})
}

func (m *Model) showShaderRemoveConfirm(name string) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Confirm Remove",
		Body:    fmt.Sprintf("Remove shader '%s'?", name),
		Buttons: []string{"Remove", "Cancel"},
		OnClose: func(button string) {
			if button == "Remove" {
				m.handleShaderCommand("/shader unload " + name)
			}
			m.pushShaderDialog()
		},
	})
}
