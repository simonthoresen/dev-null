package chrome

import (
	"os"
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
		{Label: "&Games...", Handler: func(_ string) { m.pushGamesDialog(0) }},
		{Label: "&Saves...", Handler: func(_ string) { m.pushSavesDialog(0) }},
		{Label: "---"},
		{Label: "&Themes...", Handler: func(_ string) { m.pushThemeDialog(0) }},
		{Label: "&Plugins...", Handler: func(_ string) { m.pushPluginDialog(0) }},
		{Label: "&Shaders...", Handler: func(_ string) { m.pushShaderDialog(0) }},
		{Label: "S&ynths...", Handler: func(_ string) { m.pushSynthDialog(0) }},
		{Label: "---"},
		{Label: "E&xit", Hotkey: "ctrl+q", Handler: func(playerID string) {
			m.overlay.PushDialog(domain.DialogRequest{
				Title:   "Exit",
				Body:    "Disconnect from the server?",
				Buttons: []string{"Yes", "No"},
				Warning: true,
				OnClose: func(btn string) {
					if btn == "Yes" {
						// Kick SSH session (no-op in direct/local mode) and
						// send quit to the player's program/backend.
						go func() {
							m.api.KickPlayer(playerID)          //nolint:errcheck
							m.api.SendToPlayer(playerID, domain.QuitRequestMsg{})
						}()
					}
				},
			})
		}},
	}
	menus := []domain.MenuDef{{Label: "&File", Items: fileItems}}

	// Graphics menu — HD toggle for enhanced clients with a canvas game.
	// Shown in both lobby and playing views whenever a canvas game is loaded.
	if m.IsEnhancedClient && m.canUseRenderMode(domain.RenderModeCanvasHD) {
		viewItems := []domain.MenuItemDef{
			{
				Label:   "Quadrant",
				Toggle:  true,
				Checked: func() bool { return m.renderMode != domain.RenderModeCanvasHD },
				Handler: func(_ string) { m.dispatchInput("/render-quadrant") },
			},
			{
				Label:   "Canvas &HD",
				Toggle:  true,
				Checked: func() bool { return m.renderMode == domain.RenderModeCanvasHD },
				Handler: func(_ string) { m.dispatchInput("/render-canvas-hd") },
			},
		}
		menus = append(menus, domain.MenuDef{Label: "&Graphics", Items: viewItems})
	}

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

func (m *Model) pushGamesDialog(cursor int) {
	m.api.State().RLock()
	currentGame := m.api.State().GameName
	m.api.State().RUnlock()

	localcmd.PushGameDialog(cursor, localcmd.GameDialogOptions{
		DataDir:     m.api.DataDir(),
		Overlay:     &m.overlay,
		CurrentGame: currentGame,
		CanLoad:     m.isAdmin(),
		CanAdd:      m.isAdmin(),
		OnLoad: func(name string) {
			m.dispatchInput("/game-load " + name)
		},
		Reload: m.pushGamesDialog,
	})
}

func (m *Model) pushSavesDialog(cursor int) {
	localcmd.PushSaveDialog(cursor, localcmd.SaveDialogOptions{
		DataDir:  m.api.DataDir(),
		Overlay:  &m.overlay,
		CanLoad:  m.isAdmin(),
		OnLoad: func(gameName, saveName string) {
			m.dispatchInput("/game-resume " + gameName + "/" + saveName)
		},
		Reload: m.pushSavesDialog,
	})
}

func (m *Model) isAdmin() bool {
	m.api.State().RLock()
	p := m.api.State().Players[m.playerID]
	m.api.State().RUnlock()
	return p != nil && p.IsAdmin
}

func (m *Model) pushThemeDialog(cursor int) {
	localcmd.PushThemeDialog(cursor, localcmd.ThemeDialogOptions{
		DataDir:          m.api.DataDir(),
		Overlay:          &m.overlay,
		CurrentThemeName: m.themeName,
		CanAdd:           m.isAdmin(),
		OnSelect: func(name string, _ *theme.Theme) {
			m.dispatchInput("/theme-load " + name)
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
		CanAdd:  m.isAdmin(),
		OnToggle: func(name string, load bool) {
			if load {
				m.dispatchInput("/plugin-load " + name)
			} else {
				m.dispatchInput("/plugin-unload " + name)
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
		CanAdd:  m.isAdmin(),
		OnToggle: func(name string, load bool) {
			if load {
				m.dispatchInput("/shader-load " + name)
			} else {
				m.dispatchInput("/shader-unload " + name)
			}
		},
		Reload: m.pushShaderDialog,
	})
}

func (m *Model) pushSynthDialog(cursor int) {
	sf2Dir := filepath.Join(m.api.DataDir(), "soundfonts")
	entries, _ := os.ReadDir(sf2Dir)
	var names []string
	var tags []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sf2") {
			name := strings.TrimSuffix(e.Name(), ".sf2")
			names = append(names, name)
			if name == m.synthName {
				tags = append(tags, "(●)")
			} else {
				tags = append(tags, "(○)")
			}
		}
	}
	if len(names) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "SoundFonts",
			Body:    "No SoundFonts found in soundfonts/",
			Buttons: []string{"Close"},
		})
		return
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:     "SoundFonts",
		ListItems: names,
		ListTags:  tags,
		Buttons:   []string{"Close"},
		OnListEnter: func(idx int) {
			m.dispatchInput("/synth-load " + names[idx])
			m.overlay.PopDialog()
			m.pushSynthDialog(idx)
		},
	})
	m.overlay.SetTopCursor(cursor)
}
