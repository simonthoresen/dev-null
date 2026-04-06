package chrome

import (
	"fmt"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/localcmd"
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

func (m *Model) pushThemeDialog(cursor int) {
	localcmd.PushThemeDialog(cursor, localcmd.ThemeDialogOptions{
		DataDir:          m.api.DataDir(),
		Overlay:          &m.overlay,
		CurrentThemeName: m.themeName,
		CanAdd:           true,
		OnSelect: func(name string, t *theme.Theme) {
			m.theme = t
			m.themeName = name
			m.gameWindow = nil
			m.persistClientConfig()
		},
		Reload: m.pushThemeDialog,
	})
}

func (m *Model) pushPluginDialog(cursor int) {
	localcmd.PushScriptDialog(cursor, localcmd.ScriptDialogOptions{
		Title:   "Plugins",
		SubDir:  "plugins",
		DataDir: m.api.DataDir(),
		Overlay: &m.overlay,
		Loaded:  m.pluginNames,
		CanAdd:  true,
		OnToggle: func(name string, load bool) {
			if load {
				m.handlePluginCommand("/plugin load " + name)
			} else {
				m.handlePluginCommand("/plugin unload " + name)
			}
		},
		Reload: m.pushPluginDialog,
	})
}

func (m *Model) pushShaderDialog(cursor int) {
	localcmd.PushScriptDialog(cursor, localcmd.ScriptDialogOptions{
		Title:   "Shaders",
		SubDir:  "shaders",
		DataDir: m.api.DataDir(),
		Overlay: &m.overlay,
		Loaded:  m.shaderNames,
		CanAdd:  true,
		OnToggle: func(name string, load bool) {
			if load {
				m.handleShaderCommand("/shader load " + name)
			} else {
				m.handleShaderCommand("/shader unload " + name)
			}
		},
		Reload: m.pushShaderDialog,
	})
}
