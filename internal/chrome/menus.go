package chrome

import (
	"fmt"
	"path/filepath"
	"strings"

	"null-space/internal/domain"
	"null-space/internal/engine"
)

// allMenus returns the full ordered list of menus for the NC action bar:
// the framework "File" menu followed by any game-registered menus.
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
		{Label: "&Resume Game...", Handler: func(_ string) { m.showResumeGameDialog() }},
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

func (m *Model) showResumeGameDialog() {
	saves := m.api.ListSuspends()
	if len(saves) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Resume Game",
			Body:    "No suspended games found.",
			Buttons: []string{"OK"},
		})
		return
	}

	teamCount := m.api.State().TeamCount()

	var lines []string
	var buttons []string
	for i, s := range saves {
		if i >= 9 {
			break // limit to 9 saves in the dialog
		}
		teamNote := ""
		if s.TeamCount != teamCount {
			teamNote = fmt.Sprintf("  (lobby has %d teams)", teamCount)
		}
		lines = append(lines, fmt.Sprintf("  %d. %s/%s  (%d teams, %s)%s",
			i+1, s.GameName, s.SaveName, s.TeamCount, s.SavedAt.Format(domain.TimeFormatShort), teamNote))
		buttons = append(buttons, fmt.Sprintf("%d", i+1))
	}
	buttons = append(buttons, "Cancel")

	body := strings.Join(lines, "\n")

	// Capture saves slice for the OnClose callback.
	capturedSaves := saves
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Resume Game",
		Body:    body,
		Buttons: buttons,
		OnClose: func(button string) {
			if button == "Cancel" || button == "" {
				return
			}
			idx := 0
			fmt.Sscanf(button, "%d", &idx)
			if idx < 1 || idx > len(capturedSaves) {
				return
			}
			s := capturedSaves[idx-1]
			if err := m.api.ResumeGame(s.GameName, s.SaveName); err != nil {
				m.overlay.PushDialog(domain.DialogRequest{
					Title:   "Resume Failed",
					Body:    err.Error(),
					Buttons: []string{"OK"},
				})
			}
		},
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
	available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	var lines []string
	if len(m.shaderNames) > 0 {
		lines = append(lines, "Active (in order):")
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Available:")
	if len(available) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, name := range available {
			tag := ""
			if loadedSet[name] {
				tag = "  [active]"
			}
			lines = append(lines, "  "+name+tag)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Use /shader load|unload|up|down <name>")

	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Shaders",
		Body:    strings.Join(lines, "\n"),
		Buttons: []string{"Close"},
	})
}
