package chrome

import (
	"fmt"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/theme"
)

// invalidateMenuCache forces the next cachedMenus() call to rebuild.
func (m *Model) invalidateMenuCache() {
	m.menuCache = nil
}

// cachedMenus returns the menu tree, rebuilding when the active game or canvas scale changes.
func (m *Model) cachedMenus() []domain.MenuDef {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	canvasScale := m.api.State().CanvasScale
	m.api.State().RUnlock()

	if m.menuCache != nil && m.menuCacheGame == game && m.menuCacheScale == canvasScale {
		return m.menuCache
	}
	m.menuCacheScale = canvasScale

	fileItems := []domain.MenuItemDef{
		{Label: "&Games...", Handler: func(_ string) { m.showGamesDialog() }},
		{Label: "---"},
		{Label: "&Themes...", Handler: func(_ string) { m.pushThemeDialog(0) }},
		{Label: "&Plugins...", Handler: func(_ string) { m.pushPluginDialog(0) }},
		{Label: "&Shaders...", Handler: func(_ string) { m.pushShaderDialog(0) }},
		{Label: "---"},
		{Label: "E&xit", Handler: func(playerID string) {
			m.overlay.PushDialog(domain.DialogRequest{
				Title:   "Exit",
				Body:    "Disconnect from the server?",
				Buttons: []string{"Yes", "No"},
				Warning: true,
				OnClose: func(btn string) {
					if btn == "Yes" {
						go m.api.KickPlayer(playerID)
					}
				},
			})
		}},
	}
	menus := []domain.MenuDef{{Label: "&File", Items: fileItems}}

	// View menu — rendering mode + local rendering toggle.
	viewItems := make([]domain.MenuItemDef, 0, 5)
	for _, mode := range []domain.RenderMode{domain.RenderModeText, domain.RenderModeQuadrant, domain.RenderModeCanvas} {
		mode := mode // capture
		viewItems = append(viewItems, domain.MenuItemDef{
			Label:    mode.Label(),
			Toggle:   true,
			Disabled: !m.canUseRenderMode(mode),
			Checked:  func() bool { return m.renderMode == mode },
			Handler: func(_ string) {
				m.renderMode = mode
			},
		})
	}
	viewItems = append(viewItems,
		domain.MenuItemDef{Label: "---"},
		domain.MenuItemDef{
			Label:    "&Local",
			Toggle:   true,
			Disabled: !m.IsEnhancedClient || m.IsTerminalClient,
			Checked:  func() bool { return m.localRendering },
			Handler: func(_ string) {
				m.localRendering = !m.localRendering
				m.localModeSent = false // re-send mode OSC next frame
				if !m.localRendering {
					m.gameSrcSent = false   // allow re-send if toggled back on
					m.lastStateJSON = ""
				}
			},
		},
	)
	menus = append(menus, domain.MenuDef{Label: "&View", Items: viewItems})

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
		lines = append(lines, fmt.Sprintf("Current: %s (playing)", gameName))
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

// pushThemeDialog opens the Themes dialog with radio-style selection.
// Enter activates the highlighted theme. Add downloads a new theme. No file deletion.
func (m *Model) pushThemeDialog(cursor int) {
	available := theme.ListThemes(m.api.DataDir())
	if len(available) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Themes",
			Body:    "No themes found in themes/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showThemeAddDialog(0)
				}
			},
		})
		return
	}
	tags := make([]string, len(available))
	for i, name := range available {
		if strings.EqualFold(name, m.theme.Name) {
			tags[i] = "(●)"
		} else {
			tags[i] = "(○)"
		}
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:     "Themes",
		ListItems: available,
		ListTags:  tags,
		Buttons:   []string{"Add", "Close"},
		OnListEnter: func(idx int) {
			name := available[idx]
			path := filepath.Join(m.api.DataDir(), "themes", name+".json")
			t, err := theme.Load(path)
			if err != nil {
				return
			}
			m.theme = t
			m.themeName = name
			m.gameWindow = nil
			m.persistClientConfig()
			m.overlay.PopDialog()
			m.pushThemeDialog(0)
			m.overlay.SetTopCursor(idx)
		},
		OnListAction: func(btn string, idx int) {
			if btn == "Add" {
				m.showThemeAddDialog(idx)
			}
		},
	})
	m.overlay.SetTopCursor(cursor)
}

func (m *Model) showThemeAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Theme",
		Body:         "Enter a theme name or URL:",
		InputPrompt:  "Theme",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handleThemeCommand("/theme " + strings.TrimSpace(value))
			}
			m.pushThemeDialog(returnCursor)
		},
	})
}

// pushPluginDialog opens the Plugins dialog: loaded plugins first (with order
// numbers), then unloaded ones. Enter toggles load/unload.
func (m *Model) pushPluginDialog(cursor int) {
	available := engine.ListScripts(filepath.Join(m.api.DataDir(), "plugins"))
	loadedSet := make(map[string]bool)
	for _, n := range m.pluginNames {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(m.pluginNames) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Plugins",
			Body:    "No plugins found in plugins/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showPluginAddDialog(0)
				}
			},
		})
		return
	}

	activeCount := len(m.pluginNames)
	numWidth := len(fmt.Sprintf("%d", max(activeCount, 1)))
	inactivePad := strings.Repeat(" ", numWidth+2) // matches "N. " width

	var items []string
	var tags []string
	for i, name := range m.pluginNames {
		items = append(items, fmt.Sprintf("%*d. %s", numWidth, i+1, name))
		tags = append(tags, "[✓]")
	}
	var inactive []string
	for _, name := range available {
		if !loadedSet[name] {
			items = append(items, inactivePad+name)
			tags = append(tags, "[ ]")
			inactive = append(inactive, name)
		}
	}

	m.overlay.PushDialog(domain.DialogRequest{
		Title:     "Plugins",
		ListItems: items,
		ListTags:  tags,
		Buttons:   []string{"Add", "Close"},
		OnListEnter: func(idx int) {
			var newCursor int
			if idx < activeCount {
				toggledName := m.pluginNames[idx]
				m.handlePluginCommand("/plugin unload " + toggledName)
				newActiveSet := make(map[string]bool)
				for _, n := range m.pluginNames {
					newActiveSet[n] = true
				}
				pos := 0
				for _, n := range available {
					if !newActiveSet[n] {
						if strings.EqualFold(n, toggledName) {
							break
						}
						pos++
					}
				}
				newCursor = len(m.pluginNames) + pos
			} else {
				inactiveIdx := idx - activeCount
				if inactiveIdx < len(inactive) {
					m.handlePluginCommand("/plugin load " + inactive[inactiveIdx])
					newCursor = len(m.pluginNames) - 1
				}
			}
			m.overlay.PopDialog()
			m.pushPluginDialog(0)
			m.overlay.SetTopCursor(newCursor)
		},
		OnListAction: func(btn string, idx int) {
			if btn == "Add" {
				m.showPluginAddDialog(idx)
			}
		},
	})
	m.overlay.SetTopCursor(cursor)
}

func (m *Model) showPluginAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Plugin",
		Body:         "Enter a plugin name or URL:",
		InputPrompt:  "Plugin",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handlePluginCommand("/plugin load " + strings.TrimSpace(value))
			}
			m.pushPluginDialog(returnCursor)
		},
	})
}

// pushShaderDialog opens the Shaders dialog with checkbox-style selection.
// Active shaders are listed first (in order), then inactive ones.
// Enter toggles load/unload. Up/Down reorders active shaders. Add downloads a new shader.
// No file deletion from the player client.
func (m *Model) pushShaderDialog(cursor int) {
	available := engine.ListScripts(filepath.Join(m.api.DataDir(), "shaders"))
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	activeCount := len(m.shaderNames)
	numWidth := len(fmt.Sprintf("%d", max(activeCount, 1)))
	inactivePad := strings.Repeat(" ", numWidth+2) // matches "N. " width

	var items []string
	var tags []string
	for i, name := range m.shaderNames {
		items = append(items, fmt.Sprintf("%*d. %s", numWidth, i+1, name))
		tags = append(tags, "[✓]")
	}
	var inactive []string
	for _, name := range available {
		if !loadedSet[name] {
			items = append(items, inactivePad+name)
			tags = append(tags, "[ ]")
			inactive = append(inactive, name)
		}
	}

	if len(items) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Shaders",
			Body:    "No shaders found in shaders/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showShaderAddDialog(0)
				}
			},
		})
		return
	}

	m.overlay.PushDialog(domain.DialogRequest{
		Title:     "Shaders",
		ListItems: items,
		ListTags:  tags,
		Buttons:   []string{"Add", "Close"},
		OnListEnter: func(idx int) {
			var newCursor int
			if idx < activeCount {
				toggledName := m.shaderNames[idx]
				m.handleShaderCommand("/shader unload " + toggledName)
				// Find where the unloaded shader landed in the new inactive list.
				newActiveSet := make(map[string]bool)
				for _, n := range m.shaderNames {
					newActiveSet[n] = true
				}
				pos := 0
				for _, n := range available {
					if !newActiveSet[n] {
						if strings.EqualFold(n, toggledName) {
							break
						}
						pos++
					}
				}
				newCursor = len(m.shaderNames) + pos
			} else {
				inactiveIdx := idx - activeCount
				if inactiveIdx < len(inactive) {
					m.handleShaderCommand("/shader load " + inactive[inactiveIdx])
					newCursor = len(m.shaderNames) - 1
				}
			}
			m.overlay.PopDialog()
			m.pushShaderDialog(0)
			m.overlay.SetTopCursor(newCursor)
		},
		OnListAction: func(btn string, idx int) {
			if btn == "Add" {
				m.showShaderAddDialog(idx)
			}
		},
	})
	m.overlay.SetTopCursor(cursor)
}

func (m *Model) showShaderAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Shader",
		Body:         "Enter a shader name or URL:",
		InputPrompt:  "Shader",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handleShaderCommand("/shader load " + strings.TrimSpace(value))
			}
			m.pushShaderDialog(returnCursor)
		},
	})
}
