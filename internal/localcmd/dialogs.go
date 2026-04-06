package localcmd

import (
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

// ─── Theme dialog ─────────────────────────────────────────────────────────────

// ThemeDialogOptions configures the Themes dialog.
// Set OnRemove non-nil to show the Remove button (only when a theme is active).
type ThemeDialogOptions struct {
	DataDir          string
	Overlay          *widget.OverlayState
	CurrentThemeName string              // file stem of the active theme, or ""
	CanAdd           bool
	OnSelect         func(name string, t *theme.Theme) // called when a theme is activated
	OnRemove         func(name string, returnCursor int) // nil → no Remove button
	Reload           func(cursor int)
}

// PushThemeDialog opens the Themes dialog on opts.Overlay.
func PushThemeDialog(cursor int, opts ThemeDialogOptions) {
	available := theme.ListThemes(opts.DataDir)
	if len(available) == 0 {
		opts.Overlay.PushDialog(domain.DialogRequest{
			Title:   "Themes",
			Body:    "No themes found in themes/",
			Buttons: themeButtons(opts),
			OnClose: func(btn string) {
				if btn == "Add" {
					pushThemeAddDialog(opts, 0)
				}
			},
		})
		return
	}
	tags := make([]string, len(available))
	for i, name := range available {
		if strings.EqualFold(name, opts.CurrentThemeName) {
			tags[i] = "(●)"
		} else {
			tags[i] = "(○)"
		}
	}
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:     "Themes",
		ListItems: available,
		ListTags:  tags,
		Buttons:   themeButtons(opts),
		OnListEnter: func(idx int) {
			name := available[idx]
			t, err := theme.Load(filepath.Join(opts.DataDir, "themes", name+".json"))
			if err != nil {
				return
			}
			opts.OnSelect(name, t)
			opts.Overlay.PopDialog()
			opts.Reload(0)
			opts.Overlay.SetTopCursor(idx)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Add":
				pushThemeAddDialog(opts, idx)
			case "Remove":
				opts.OnRemove(opts.CurrentThemeName, idx)
			}
		},
	})
	opts.Overlay.SetTopCursor(cursor)
}

func themeButtons(opts ThemeDialogOptions) []string {
	canRemove := opts.OnRemove != nil && opts.CurrentThemeName != ""
	switch {
	case opts.CanAdd && canRemove:
		return []string{"Add", "Remove", "Close"}
	case opts.CanAdd:
		return []string{"Add", "Close"}
	case canRemove:
		return []string{"Remove", "Close"}
	default:
		return []string{"Close"}
	}
}

func pushThemeAddDialog(opts ThemeDialogOptions, returnCursor int) {
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Theme",
		Body:         "Enter a theme name:",
		InputPrompt:  "Theme",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" {
				if t, err := theme.Load(filepath.Join(opts.DataDir, "themes", value+".json")); err == nil {
					opts.OnSelect(value, t)
				}
			}
			opts.Reload(returnCursor)
		},
	})
}

// ─── Game dialog ──────────────────────────────────────────────────────────────

// GameDialogOptions configures the Games dialog.
type GameDialogOptions struct {
	DataDir      string
	Overlay      *widget.OverlayState
	CurrentGame  string // name of the currently loaded game, or ""
	SelectedGame string // name selected in the list (drives Load/Remove), or ""
	CanLoad      bool   // show Load button (chrome admin only)
	CanAdd       bool   // show Add button
	CanRemove    bool   // show Remove button (console only)
	OnLoad       func(name string)             // called when Load is pressed or Add input confirmed
	OnRemove     func(name string, cursor int) // nil → no Remove button
	Reload       func(cursor int)
}

// PushGameDialog opens the Games dialog on opts.Overlay.
func PushGameDialog(cursor int, opts GameDialogOptions) {
	available := engine.ListGames(filepath.Join(opts.DataDir, "games"))
	if len(available) == 0 {
		opts.Overlay.PushDialog(domain.DialogRequest{
			Title:   "Games",
			Body:    "No games found in games/",
			Buttons: gameButtons(opts, false),
			OnClose: func(btn string) {
				if btn == "Add" {
					pushGameAddDialog(opts, 0)
				}
			},
		})
		return
	}
	tags := make([]string, len(available))
	for i, name := range available {
		switch {
		case strings.EqualFold(name, opts.SelectedGame):
			tags[i] = "(●)"
		case strings.EqualFold(name, opts.CurrentGame):
			tags[i] = "(◉)" // loaded but not selected
		default:
			tags[i] = "(○)"
		}
	}
	// Buttons that require list navigation to become active.
	var requireNav []string
	if opts.CanLoad {
		requireNav = append(requireNav, "Load")
	}
	if opts.CanRemove {
		requireNav = append(requireNav, "Remove")
	}
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:                 "Games",
		ListItems:             available,
		ListTags:              tags,
		Buttons:               gameButtons(opts, true),
		RequireListNavigation: requireNav,
		OnListEnter: func(idx int) {
			// Mark the entered item as selected and reload.
			opts.SelectedGame = available[idx]
			opts.Overlay.PopDialog()
			PushGameDialog(idx, opts)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Load":
				name := available[idx]
				if opts.OnLoad != nil && opts.CurrentGame != "" && !strings.EqualFold(opts.CurrentGame, name) {
					pushGameLoadConfirm(opts, name, idx)
				} else if opts.OnLoad != nil {
					opts.OnLoad(name)
				}
			case "Add":
				pushGameAddDialog(opts, idx)
			case "Remove":
				if opts.OnRemove != nil {
					opts.OnRemove(available[idx], idx)
				}
			}
		},
	})
	opts.Overlay.SetTopCursor(cursor)
}

func gameButtons(opts GameDialogOptions, hasItems bool) []string {
	var btns []string
	if opts.CanLoad && hasItems {
		btns = append(btns, "Load")
	}
	if opts.CanAdd {
		btns = append(btns, "Add")
	}
	if opts.CanRemove && hasItems {
		btns = append(btns, "Remove")
	}
	return append(btns, "Close")
}

func pushGameLoadConfirm(opts GameDialogOptions, name string, returnCursor int) {
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:   "Load Game",
		Body:    "A game is already running:\n\n  " + opts.CurrentGame + "\n\nUnload it and load " + name + "?",
		Buttons: []string{"Load", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Load" && opts.OnLoad != nil {
				opts.OnLoad(name)
			} else {
				opts.Overlay.PopDialog()
				PushGameDialog(returnCursor, opts)
			}
		},
	})
}

func pushGameAddDialog(opts GameDialogOptions, returnCursor int) {
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Game",
		Body:         "Enter a game name or URL:",
		InputPrompt:  "Game",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && opts.OnLoad != nil {
				opts.OnLoad(value)
			}
			opts.Reload(returnCursor)
		},
	})
}

// ─── Script dialog (plugins / shaders) ────────────────────────────────────────

// ScriptDialogOptions configures a Plugins or Shaders dialog.
// Set OnRemove non-nil to show the Remove button (only when items are loaded).
type ScriptDialogOptions struct {
	Title    string              // "Plugins" or "Shaders"
	SubDir   string              // "plugins" or "shaders"
	DataDir  string
	Overlay  *widget.OverlayState
	Loaded   []string            // currently loaded/active names
	CanAdd   bool
	OnToggle func(name string, load bool)           // load or unload a script by name
	OnRemove func(names []string, returnCursor int) // nil → no Remove button
	Reload   func(cursor int)
}

// PushScriptDialog opens a Plugins or Shaders dialog on opts.Overlay.
func PushScriptDialog(cursor int, opts ScriptDialogOptions) {
	available := engine.ListScripts(filepath.Join(opts.DataDir, opts.SubDir))
	availableSet := make(map[string]bool)
	for _, n := range available {
		availableSet[n] = true
	}
	items := append([]string(nil), available...)
	for _, n := range opts.Loaded {
		if !availableSet[n] {
			items = append(items, n)
		}
	}
	loadedSet := make(map[string]bool)
	for _, n := range opts.Loaded {
		loadedSet[n] = true
	}

	noun := strings.TrimSuffix(opts.Title, "s") // "Plugins" → "Plugin"

	if len(items) == 0 {
		opts.Overlay.PushDialog(domain.DialogRequest{
			Title:   opts.Title,
			Body:    "No " + strings.ToLower(opts.Title) + " found in " + opts.SubDir + "/",
			Buttons: scriptButtons(opts, false),
			OnClose: func(btn string) {
				if btn == "Add" {
					pushScriptAddDialog(opts, noun, 0)
				}
			},
		})
		return
	}

	tags := make([]string, len(items))
	for i, name := range items {
		if loadedSet[name] {
			tags[i] = "[✓]"
		} else {
			tags[i] = "[ ]"
		}
	}
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:     opts.Title,
		ListItems: items,
		ListTags:  tags,
		Buttons:   scriptButtons(opts, len(opts.Loaded) > 0),
		OnListEnter: func(idx int) {
			name := items[idx]
			opts.OnToggle(name, !loadedSet[name])
			opts.Overlay.PopDialog()
			opts.Reload(0)
			opts.Overlay.SetTopCursor(idx)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Add":
				pushScriptAddDialog(opts, noun, idx)
			case "Remove":
				opts.OnRemove(append([]string(nil), opts.Loaded...), idx)
			}
		},
	})
	opts.Overlay.SetTopCursor(cursor)
}

func scriptButtons(opts ScriptDialogOptions, hasLoaded bool) []string {
	canRemove := opts.OnRemove != nil && hasLoaded
	switch {
	case opts.CanAdd && canRemove:
		return []string{"Add", "Remove", "Close"}
	case opts.CanAdd:
		return []string{"Add", "Close"}
	case canRemove:
		return []string{"Remove", "Close"}
	default:
		return []string{"Close"}
	}
}

func pushScriptAddDialog(opts ScriptDialogOptions, noun string, returnCursor int) {
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:        "Add " + noun,
		Body:         "Enter a " + strings.ToLower(noun) + " name or URL:",
		InputPrompt:  noun,
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" {
				opts.OnToggle(value, true)
			}
			opts.Reload(returnCursor)
		},
	})
}
