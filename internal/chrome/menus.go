package chrome

import (
	"path/filepath"
	"sort"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/localcmd"
	"dev-null/internal/render"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

// invalidateMenuCache forces the next cachedMenus() call to rebuild.
func (m *Model) invalidateMenuCache() {
	m.menuCache = nil
}

// cachedMenus returns the menu tree, rebuilding when the active game changes.
func (m *Model) cachedMenus() []domain.MenuDef {
	m.api.State().RLock()
	game := m.api.State().ActiveGame
	m.api.State().RUnlock()

	if m.menuCache != nil && m.menuCacheGame == game {
		return m.menuCache
	}

	fileItems := []domain.MenuItemDef{
		{Label: "&Games", SubItems: m.buildGameSubItems()},
		{Label: "&Saves...", Handler: func(_ string) { m.pushSavesDialog(0) }},
		{Label: "---"},
		{Label: "&Themes", SubItems: m.buildThemeSubItems()},
		{Label: "&Plugins", SubItems: m.buildPluginSubItems()},
		{Label: "S&haders", SubItems: m.buildShaderSubItems()},
		{Label: "S&ynths", SubItems: m.buildSynthSubItems()},
		{Label: "&Fonts", SubItems: m.buildFontSubItems()},
		{Label: "---"},
		{Label: "&Invite", SubItems: m.buildInviteSubItems()},
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

	// Graphics menu — always visible; shows current preference as radio toggles.
	// Pixels is disabled for SSH (non-enhanced) clients since they can't render locally.
	graphicsItems := []domain.MenuItemDef{
		{
			Label:   "&Ascii",
			Toggle:  true,
			Checked: func() bool { return m.graphicsPref == domain.RenderModeAscii },
			Handler: func(_ string) { m.dispatchInput("/render-ascii") },
		},
		{
			Label:   "&Blocks",
			Toggle:  true,
			Checked: func() bool { return m.graphicsPref == domain.RenderModeBlocks },
			Handler: func(_ string) { m.dispatchInput("/render-blocks") },
		},
		{
			Label:    "&Pixels",
			Toggle:   true,
			Disabled: !m.IsEnhancedClient,
			Checked:  func() bool { return m.graphicsPref == domain.RenderModePixels },
			Handler:  func(_ string) { m.dispatchInput("/render-pixels") },
		},
	}
	menus = append(menus, domain.MenuDef{Label: "&Graphics", Items: graphicsItems})

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

// ─── Sub-menu builders ───────────────────────────────────────────────────────

func (m *Model) buildGameSubItems() []domain.MenuItemDef {
	m.api.State().RLock()
	currentGame := m.api.State().GameName
	m.api.State().RUnlock()

	gamesDir := filepath.Join(m.api.DataDir(), "games")
	available := engine.ListGames(gamesDir)
	var items []domain.MenuItemDef
	if m.isAdmin() {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: func(_ string) {
				localcmd.PushGameAddDialog(&m.overlay, m.api.DataDir(), func(name string) {
					m.dispatchInput("/game-load " + name)
				})
			}},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name // capture
		label := n
		if strings.EqualFold(n, currentGame) {
			label = "→ " + n
		}
		item := domain.MenuItemDef{
			Label:   label,
			Handler: func(_ string) { m.dispatchInput("/game-load " + n) },
		}
		items = append(items, item)
	}
	return items
}

func (m *Model) buildThemeSubItems() []domain.MenuItemDef {
	available := theme.ListThemes(m.api.DataDir())
	var items []domain.MenuItemDef
	if m.isAdmin() {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: func(_ string) {
				localcmd.PushThemeAddDialog(&m.overlay, m.api.DataDir(), func(name string) {
					m.dispatchInput("/theme-load " + name)
				})
			}},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		items = append(items, domain.MenuItemDef{
			Label:   n,
			Toggle:  true,
			Checked: func() bool { return strings.EqualFold(n, m.themeName) },
			Handler: func(_ string) { m.dispatchInput("/theme-load " + n) },
		})
	}
	return items
}

func (m *Model) buildPluginSubItems() []domain.MenuItemDef {
	return m.buildScriptSubItems("plugins", m.pluginNames, "/plugin-load ", "/plugin-unload ")
}

func (m *Model) buildShaderSubItems() []domain.MenuItemDef {
	return m.buildScriptSubItems("shaders", m.shaderNames, "/shader-load ", "/shader-unload ")
}

func (m *Model) buildScriptSubItems(subDir string, loaded []string, loadCmd, unloadCmd string) []domain.MenuItemDef {
	dir := filepath.Join(m.api.DataDir(), subDir)
	available := engine.ListScripts(dir)
	availableSet := make(map[string]bool)
	for _, n := range available {
		availableSet[n] = true
	}
	// Merge loaded scripts that may not be on disk.
	all := append([]string(nil), available...)
	for _, n := range loaded {
		if !availableSet[n] {
			all = append(all, n)
		}
	}
	sort.Strings(all)

	loadedSet := make(map[string]bool)
	for _, n := range loaded {
		loadedSet[n] = true
	}

	// "plugins" → "Plugin", "shaders" → "Shader"
	noun := strings.TrimSuffix(subDir, "s")
	if len(noun) > 0 {
		noun = strings.ToUpper(noun[:1]) + noun[1:]
	}
	var items []domain.MenuItemDef
	if m.isAdmin() {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: func(_ string) {
				localcmd.PushScriptAddDialog(&m.overlay, noun, func(name string) {
					m.dispatchInput(loadCmd + name)
				})
			}},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range all {
		n := name
		items = append(items, domain.MenuItemDef{
			Label:  n,
			Toggle: true,
			Checked: func() bool { return loadedSet[n] },
			Handler: func(_ string) {
				if loadedSet[n] {
					m.dispatchInput(unloadCmd + n)
				} else {
					m.dispatchInput(loadCmd + n)
				}
			},
		})
	}
	return items
}

func (m *Model) buildSynthSubItems() []domain.MenuItemDef {
	sf2Dir := filepath.Join(m.api.DataDir(), "soundfonts")
	available := engine.ListDir(sf2Dir, ".sf2")
	var items []domain.MenuItemDef
	if m.isAdmin() {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: func(_ string) {
				localcmd.PushSynthAddDialog(&m.overlay, func(name string) {
					m.dispatchInput("/synth-load " + name)
				})
			}},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		items = append(items, domain.MenuItemDef{
			Label:   n,
			Toggle:  true,
			Checked: func() bool { return n == m.synthName },
			Handler: func(_ string) { m.dispatchInput("/synth-load " + n) },
		})
	}
	return items
}

func (m *Model) buildFontSubItems() []domain.MenuItemDef {
	fontsDir := filepath.Join(m.api.DataDir(), "fonts")
	available := engine.ListDir(fontsDir, ".flf")
	var items []domain.MenuItemDef
	if m.isAdmin() {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: func(_ string) {
				localcmd.PushFontAddDialog(&m.overlay, func(name string) {
					// Fonts don't need a "load" command — they're just files.
					// After adding, invalidate menu cache so it shows up.
					m.invalidateMenuCache()
				})
			}},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		items = append(items, domain.MenuItemDef{
			Label: n,
			Handler: func(_ string) {
				m.injectFontTag(n)
			},
		})
	}
	return items
}

func (m *Model) buildInviteSubItems() []domain.MenuItemDef {
	return []domain.MenuItemDef{
		{Label: "&Windows", Handler: func(_ string) {
			winLink, _ := m.api.InviteLinks()
			m.pendingClipboard = winLink
			m.chatLines = append(m.chatLines, m.renderLogoLines(widget.RenderWindowsLogo)...)
			m.chatLines = append(m.chatLines, "Windows invite link copied to clipboard")
		}},
		{Label: "&SSH", Handler: func(_ string) {
			_, sshLink := m.api.InviteLinks()
			m.pendingClipboard = sshLink
			m.chatLines = append(m.chatLines, m.renderLogoLines(widget.RenderSSHLogo)...)
			m.chatLines = append(m.chatLines, "SSH invite link copied to clipboard")
		}},
	}
}

// renderLogoLines renders a logo into ANSI strings suitable for chat display.
func (m *Model) renderLogoLines(renderFn func(*render.ImageBuffer, int, int)) []string {
	buf := render.NewImageBuffer(widget.LogoArtWidth, widget.LogoArtHeight)
	renderFn(buf, 0, 0)
	s := buf.ToString(m.ColorProfile)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// injectFontTag inserts <font=name></font> at the current cursor position in the chat input.
func (m *Model) injectFontTag(fontName string) {
	// Pick the active input based on whether we're in the game view.
	input := m.lobbyInput
	if m.inActiveGame {
		input = m.playingInput
	}
	if input == nil || input.Model == nil {
		return
	}
	openTag := "<font=" + fontName + ">"
	closeTag := "</font>"
	val := input.Model.Value()
	pos := input.Model.Position()
	newVal := val[:pos] + openTag + closeTag + val[pos:]
	input.Model.SetValue(newVal)
	input.Model.SetCursor(pos + len(openTag))
}
