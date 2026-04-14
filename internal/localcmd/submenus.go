package localcmd

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

// ─── Game sub-menu ───────────────────────────────────────────────────────────

// GameSubMenuOptions configures the Games sub-menu.
type GameSubMenuOptions struct {
	DataDir     string
	CurrentGame string                     // name of the loaded game, or ""
	OnAdd       func(playerID string)      // nil → no Add item
	OnLoad      func(name string)          // called on Enter
	OnDelete    func(name, playerID string) // nil → no Del support
}

// BuildGameSubItems returns the menu items for the Games sub-menu.
func BuildGameSubItems(opts GameSubMenuOptions) []domain.MenuItemDef {
	gamesDir := filepath.Join(opts.DataDir, "games")
	available := engine.ListGames(gamesDir)
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		label := n
		if strings.EqualFold(n, opts.CurrentGame) {
			label = n + "*"
		}
		item := domain.MenuItemDef{
			Label:   label,
			Handler: func(_ string) { opts.OnLoad(n) },
		}
		if opts.OnDelete != nil {
			item.OnDelete = func(playerID string) { opts.OnDelete(n, playerID) }
		}
		items = append(items, item)
	}
	return items
}

// ─── Theme sub-menu ──────────────────────────────────────────────────────────

// ThemeSubMenuOptions configures the Themes sub-menu.
type ThemeSubMenuOptions struct {
	DataDir      string
	CurrentTheme string                     // file stem of the active theme
	OnAdd        func(playerID string)      // nil → no Add item
	OnLoad       func(name string)          // called on Enter
	OnDelete     func(name, playerID string) // nil → no Del support
}

// BuildThemeSubItems returns the menu items for the Themes sub-menu.
func BuildThemeSubItems(opts ThemeSubMenuOptions) []domain.MenuItemDef {
	available := theme.ListThemes(opts.DataDir)
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		items = append(items, domain.MenuItemDef{
			Label:   n,
			Toggle:  true,
			Checked: func() bool { return strings.EqualFold(n, opts.CurrentTheme) },
			Handler: func(_ string) { opts.OnLoad(n) },
			OnDelete: deleteHandler(opts.OnDelete, n),
		})
	}
	return items
}

// ─── Script sub-menu (plugins / shaders) ─────────────────────────────────────

// ScriptSubMenuOptions configures a Plugins or Shaders sub-menu.
type ScriptSubMenuOptions struct {
	DataDir   string
	SubDir    string   // "plugins" or "shaders"
	Loaded    []string // currently loaded/active names
	OnAdd     func(playerID string)       // nil → no Add item
	OnToggle  func(name string, load bool) // called on Enter (toggle)
	OnDelete  func(name, playerID string)  // nil → no Del support
}

// BuildScriptSubItems returns the menu items for a Plugins or Shaders sub-menu.
func BuildScriptSubItems(opts ScriptSubMenuOptions) []domain.MenuItemDef {
	dir := filepath.Join(opts.DataDir, opts.SubDir)
	available := engine.ListScripts(dir)
	availableSet := make(map[string]bool)
	for _, n := range available {
		availableSet[n] = true
	}
	all := append([]string(nil), available...)
	for _, n := range opts.Loaded {
		if !availableSet[n] {
			all = append(all, n)
		}
	}
	sort.Strings(all)

	loadedSet := make(map[string]bool)
	for _, n := range opts.Loaded {
		loadedSet[n] = true
	}

	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range all {
		n := name
		items = append(items, domain.MenuItemDef{
			Label:   n,
			Toggle:  true,
			Checked: func() bool { return loadedSet[n] },
			Handler: func(_ string) { opts.OnToggle(n, !loadedSet[n]) },
			OnDelete: deleteHandler(opts.OnDelete, n),
		})
	}
	return items
}

// ─── SoundFont sub-menu ──────────────────────────────────────────────────────

// SynthSubMenuOptions configures the Synths sub-menu.
type SynthSubMenuOptions struct {
	DataDir      string
	CurrentSynth string                     // name of the active soundfont, or ""
	OnAdd        func(playerID string)      // nil → no Add item
	OnLoad       func(name string)          // nil → items are not activatable
	OnDelete     func(name, playerID string) // nil → no Del support
}

// BuildSynthSubItems returns the menu items for the Synths sub-menu.
func BuildSynthSubItems(opts SynthSubMenuOptions) []domain.MenuItemDef {
	sf2Dir := filepath.Join(opts.DataDir, "soundfonts")
	available := engine.ListDir(sf2Dir, ".sf2")
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		item := domain.MenuItemDef{Label: n, OnDelete: deleteHandler(opts.OnDelete, n)}
		if opts.OnLoad != nil && opts.CurrentSynth != "" {
			item.Toggle = true
			item.Checked = func() bool { return n == opts.CurrentSynth }
			item.Handler = func(_ string) { opts.OnLoad(n) }
		} else if opts.OnLoad != nil {
			item.Handler = func(_ string) { opts.OnLoad(n) }
		}
		items = append(items, item)
	}
	return items
}

// ─── Font sub-menu ───────────────────────────────────────────────────────────

// FontSubMenuOptions configures the Fonts sub-menu.
type FontSubMenuOptions struct {
	DataDir  string
	OnAdd    func(playerID string)       // nil → no Add item
	OnSelect func(name string)           // nil → items are not activatable
	OnDelete func(name, playerID string) // nil → no Del support
}

// BuildFontSubItems returns the menu items for the Fonts sub-menu.
func BuildFontSubItems(opts FontSubMenuOptions) []domain.MenuItemDef {
	fontsDir := filepath.Join(opts.DataDir, "fonts")
	available := engine.ListDir(fontsDir, ".flf")
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	for _, name := range available {
		n := name
		item := domain.MenuItemDef{Label: n, OnDelete: deleteHandler(opts.OnDelete, n)}
		if opts.OnSelect != nil {
			item.Handler = func(_ string) { opts.OnSelect(n) }
		}
		items = append(items, item)
	}
	return items
}

// ─── Invite sub-menu ─────────────────────────────────────────────────────────

// InviteSubMenuOptions configures the Invite sub-menu.
type InviteSubMenuOptions struct {
	WinLink      string
	SSHLink      string
	ColorProfile colorprofile.Profile
	OnCopy       func(link string) // sets clipboard
	OnOutput     func(lines []string) // writes lines to chat (chrome) or console log (server)
}

// BuildInviteSubItems returns the menu items for the Invite sub-menu.
func BuildInviteSubItems(opts InviteSubMenuOptions) []domain.MenuItemDef {
	return []domain.MenuItemDef{
		{Label: "&Windows", Handler: func(_ string) {
			if opts.OnOutput != nil {
				opts.OnOutput(renderLogoLines(widget.RenderWindowsLogo, opts.ColorProfile))
				opts.OnOutput([]string{"Windows invite link copied to clipboard"})
			}
			if opts.OnCopy != nil {
				opts.OnCopy(opts.WinLink)
			}
		}},
		{Label: "&SSH", Handler: func(_ string) {
			if opts.OnOutput != nil {
				opts.OnOutput(renderLogoLines(widget.RenderSSHLogo, opts.ColorProfile))
				opts.OnOutput([]string{"SSH invite link copied to clipboard"})
			}
			if opts.OnCopy != nil {
				opts.OnCopy(opts.SSHLink)
			}
		}},
	}
}

// renderLogoLines renders a logo into ANSI strings.
func renderLogoLines(renderFn func(*render.ImageBuffer, int, int), profile colorprofile.Profile) []string {
	buf := render.NewImageBuffer(widget.LogoArtWidth, widget.LogoArtHeight)
	renderFn(buf, 0, 0)
	s := buf.ToString(profile)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// deleteHandler wraps an OnDelete callback with a captured name, or returns nil.
func deleteHandler(fn func(name, playerID string), name string) func(playerID string) {
	if fn == nil {
		return nil
	}
	return func(playerID string) { fn(name, playerID) }
}
