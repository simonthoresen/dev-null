package localcmd

import (
	"path/filepath"
	"sort"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/state"
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
// Items are grouped by source and emitted in display order
// Core > Create > Shared. Each item passes its canonical qualified id
// "<source>:<name>" to the load/delete callbacks so duplicate names
// across sources resolve unambiguously.
func BuildGameSubItems(opts GameSubMenuOptions) []domain.MenuItemDef {
	available := engine.ListAllGames(opts.DataDir)
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	displayOrder := []engine.Source{engine.SourceCore, engine.SourceCreate, engine.SourceShared}
	for _, src := range displayOrder {
		var first = true
		for _, item := range available {
			if item.Source != src {
				continue
			}
			if first {
				items = append(items, sectionHeader(item.Source))
				first = false
			}
			id := engine.QualifiedName(item.Source, item.Name)
			label := item.Name
			if strings.EqualFold(id, opts.CurrentGame) {
				label = item.Name + "*"
			}
			menuItem := domain.MenuItemDef{
				Label:   label,
				Handler: func(_ string) { opts.OnLoad(id) },
			}
			if opts.OnDelete != nil {
				menuItem.OnDelete = func(playerID string) { opts.OnDelete(id, playerID) }
			}
			items = append(items, menuItem)
		}
	}
	return items
}

// sectionHeader returns a disabled menu item that labels a source group.
// Format: "── <Label> ──". Used to group sub-menu items by source.
func sectionHeader(src engine.Source) domain.MenuItemDef {
	return domain.MenuItemDef{
		Label:    "── " + src.Label() + " ──",
		Disabled: true,
	}
}

// ─── Load-game sub-menu ──────────────────────────────────────────────────────

// LoadGameSubMenuOptions configures the Load game sub-menu.
type LoadGameSubMenuOptions struct {
	DataDir  string
	OnLoad   func(gameName, saveName string) // called on Enter
	OnDelete func(gameName, saveName, playerID string) // nil → no Del support
}

// BuildLoadGameSubItems returns the menu items for the Load game sub-menu.
// Each save appears as one item labeled "gameName/saveName". If no saves
// exist the sub-menu shows a single disabled "(no saves)" item so the
// user sees something rather than an empty dropdown.
func BuildLoadGameSubItems(opts LoadGameSubMenuOptions) []domain.MenuItemDef {
	saves := state.ListSuspends(opts.DataDir, "")
	if len(saves) == 0 {
		return []domain.MenuItemDef{
			{Label: "(no saves)", Disabled: true},
		}
	}
	items := make([]domain.MenuItemDef, 0, len(saves))
	for _, s := range saves {
		gn, sn := s.GameName, s.SaveName
		item := domain.MenuItemDef{
			Label:   gn + "/" + sn,
			Handler: func(_ string) { opts.OnLoad(gn, sn) },
		}
		if opts.OnDelete != nil {
			item.OnDelete = func(playerID string) { opts.OnDelete(gn, sn, playerID) }
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

// BuildThemeSubItems returns the menu items for the Themes sub-menu. The
// "default" entry represents the built-in theme (active when CurrentTheme is
// empty); file-based themes follow it and are grouped by source (Create/Shared/Core).
func BuildThemeSubItems(opts ThemeSubMenuOptions) []domain.MenuItemDef {
	available := engine.ListAllThemes(opts.DataDir)
	var items []domain.MenuItemDef
	if opts.OnAdd != nil {
		items = append(items,
			domain.MenuItemDef{Label: "&Add...", Handler: opts.OnAdd},
			domain.MenuItemDef{Label: "---"},
		)
	}
	items = append(items, domain.MenuItemDef{
		Label:   "default",
		Toggle:  true,
		Checked: func() bool { return opts.CurrentTheme == "" },
		Handler: func(_ string) { opts.OnLoad("default") },
	})
	if len(available) > 0 {
		items = append(items, domain.MenuItemDef{Label: "---"})
	}
	lastSource := engine.Source(-1)
	for _, item := range available {
		if item.Source != lastSource {
			items = append(items, sectionHeader(item.Source))
			lastSource = item.Source
		}
		id := engine.QualifiedName(item.Source, item.Name)
		display := item.Name
		items = append(items, domain.MenuItemDef{
			Label:    display,
			Toggle:   true,
			Checked:  func() bool { return strings.EqualFold(id, opts.CurrentTheme) },
			Handler:  func(_ string) { opts.OnLoad(id) },
			OnDelete: deleteHandler(opts.OnDelete, id),
		})
	}
	return items
}

// ─── Script sub-menu (plugins / shaders) ─────────────────────────────────────

// ScriptSubMenuOptions configures a Plugins or Shaders sub-menu.
type ScriptSubMenuOptions struct {
	DataDir   string
	SubDir    string   // "Plugins" or "Shaders"
	Loaded    []string // currently loaded/active names
	OnAdd     func(playerID string)       // nil → no Add item
	OnToggle  func(name string, load bool) // called on Enter (toggle)
	OnDelete  func(name, playerID string)  // nil → no Del support
}

// BuildScriptSubItems returns the menu items for a Plugins or Shaders sub-menu.
// Items are grouped by source (Create > Shared > Core) with section headers.
// Each item passes its canonical qualified id to the toggle/delete callbacks.
// Loaded names not present in any source are appended at the bottom (unsourced).
func BuildScriptSubItems(opts ScriptSubMenuOptions) []domain.MenuItemDef {
	available := engine.ListAllScripts(opts.SubDir, opts.DataDir)
	availableSet := make(map[string]bool, len(available))
	for _, item := range available {
		availableSet[engine.QualifiedName(item.Source, item.Name)] = true
	}

	loadedSet := make(map[string]bool, len(opts.Loaded))
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
	lastSource := engine.Source(-1)
	for _, item := range available {
		if item.Source != lastSource {
			items = append(items, sectionHeader(item.Source))
			lastSource = item.Source
		}
		id := engine.QualifiedName(item.Source, item.Name)
		display := item.Name
		items = append(items, domain.MenuItemDef{
			Label:    display,
			Toggle:   true,
			Checked:  func() bool { return loadedSet[id] },
			Handler:  func(_ string) { opts.OnToggle(id, !loadedSet[id]) },
			OnDelete: deleteHandler(opts.OnDelete, id),
		})
	}
	// Loaded items that aren't present in any source (e.g. legacy paths).
	var orphans []string
	for _, n := range opts.Loaded {
		if !availableSet[n] {
			orphans = append(orphans, n)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		items = append(items, domain.MenuItemDef{Label: "── Other ──", Disabled: true})
		for _, name := range orphans {
			n := name
			items = append(items, domain.MenuItemDef{
				Label:    n,
				Toggle:   true,
				Checked:  func() bool { return loadedSet[n] },
				Handler:  func(_ string) { opts.OnToggle(n, !loadedSet[n]) },
				OnDelete: deleteHandler(opts.OnDelete, n),
			})
		}
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
	sf2Dir := filepath.Join(opts.DataDir, "SoundFonts")
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
	fontsDir := filepath.Join(opts.DataDir, "Fonts")
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

// deleteHandler wraps an OnDelete callback with a captured name, or returns nil.
func deleteHandler(fn func(name, playerID string), name string) func(playerID string) {
	if fn == nil {
		return nil
	}
	return func(playerID string) { fn(name, playerID) }
}
