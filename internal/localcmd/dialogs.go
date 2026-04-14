package localcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/state"
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
	DataDir     string
	Overlay     *widget.OverlayState
	CurrentGame string // name of the currently loaded game, or ""
	TeamCount   int    // current number of teams (for compatibility indicator)
	CanAdd      bool   // show Add button
	CanRemove   bool   // show Remove button (console only)
	OnLoad      func(name string)             // nil means loading not allowed
	OnRemove    func(name string, cursor int) // nil → no Remove button
	Reload      func(cursor int)
}

// PushGameDialog opens the Games dialog on opts.Overlay.
func PushGameDialog(cursor int, opts GameDialogOptions) {
	gamesDir := filepath.Join(opts.DataDir, "games")
	available := engine.ListGames(gamesDir)
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
	items := make([]string, len(available))
	tags := make([]string, len(available))
	for i, name := range available {
		if strings.EqualFold(name, opts.CurrentGame) {
			items[i] = "→ " + name
		} else {
			items[i] = "  " + name
		}
		tr := engine.ProbeGameTeamRange(engine.ResolveGamePath(gamesDir, name))
		tags[i] = formatGameTeamTag(tr, opts.TeamCount)
	}
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:     "Games",
		ListItems: items,
		ListTags:  tags,
		Buttons:   gameButtons(opts, true),
		OnListEnter: func(idx int) {
			if opts.OnLoad == nil {
				return
			}
			name := available[idx]
			if opts.CurrentGame != "" && !strings.EqualFold(opts.CurrentGame, name) {
				opts.Overlay.PopDialog()
				pushGameLoadConfirm(opts, name, idx)
			} else {
				opts.Overlay.PopDialog()
				opts.OnLoad(name)
			}
		},
		OnListAction: func(btn string, idx int) {
			// Dialog already popped by fireDialogCloseEntry before this is called.
			switch btn {
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

// formatGameTeamTag returns the right-aligned tag for a game's team range entry.
// Shows the range (e.g. "2-4", "2+", "≤4") prefixed with "!" when the current
// team count falls outside that range. Returns "" when there is no constraint.
func formatGameTeamTag(tr domain.TeamRange, teamCount int) string {
	if tr.Min == 0 && tr.Max == 0 {
		return ""
	}
	ok := true
	if tr.Min > 0 && teamCount < tr.Min {
		ok = false
	}
	if tr.Max > 0 && teamCount > tr.Max {
		ok = false
	}
	var rng string
	switch {
	case tr.Min > 0 && tr.Max > 0 && tr.Min == tr.Max:
		rng = fmt.Sprintf("%d", tr.Min)
	case tr.Min > 0 && tr.Max > 0:
		rng = fmt.Sprintf("%d-%d", tr.Min, tr.Max)
	case tr.Min > 0:
		rng = fmt.Sprintf("%d+", tr.Min)
	default:
		rng = fmt.Sprintf("≤%d", tr.Max)
	}
	if !ok {
		return "!" + rng
	}
	return rng
}

func gameButtons(opts GameDialogOptions, hasItems bool) []string {
	var btns []string
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

// ─── Save dialog ──────────────────────────────────────────────────────────────

// SaveDialogOptions configures the Saves dialog.
type SaveDialogOptions struct {
	DataDir      string
	Overlay      *widget.OverlayState
	SelectedSave string // "gameName/saveName" of the selected entry, or ""
	CanLoad      bool   // show Load button (chrome admin only)
	CanRemove    bool   // show Remove button (console only)
	OnLoad       func(gameName, saveName string)             // called when Load is confirmed
	OnRemove     func(gameName, saveName string, cursor int) // called when Remove is confirmed
	Reload       func(cursor int)
}

// PushSaveDialog opens the Saves dialog on opts.Overlay.
func PushSaveDialog(cursor int, opts SaveDialogOptions) {
	saves := state.ListSuspends(opts.DataDir, "")
	if len(saves) == 0 {
		opts.Overlay.PushDialog(domain.DialogRequest{
			Title:   "Saves",
			Body:    "No saves found.",
			Buttons: []string{"Close"},
		})
		return
	}
	items := make([]string, len(saves))
	tags := make([]string, len(saves))
	for i, s := range saves {
		key := s.GameName + "/" + s.SaveName
		items[i] = key
		if key == opts.SelectedSave {
			tags[i] = "(●)"
		} else {
			tags[i] = "(○)"
		}
	}
	var btns []string
	if opts.CanLoad && opts.SelectedSave != "" {
		btns = append(btns, "Load")
	}
	if opts.CanRemove && opts.SelectedSave != "" {
		btns = append(btns, "Remove")
	}
	btns = append(btns, "Close")
	opts.Overlay.PushDialog(domain.DialogRequest{
		Title:     "Saves",
		ListItems: items,
		ListTags:  tags,
		Buttons:   btns,
		OnListEnter: func(idx int) {
			opts.SelectedSave = items[idx]
			opts.Overlay.PopDialog()
			PushSaveDialog(idx, opts)
		},
		OnListAction: func(btn string, idx int) {
			s := saves[idx]
			switch btn {
			case "Load":
				if opts.OnLoad != nil && opts.SelectedSave != "" {
					if s2 := saveByKey(saves, opts.SelectedSave); s2 != nil {
						opts.OnLoad(s2.GameName, s2.SaveName)
					} else {
						opts.OnLoad(s.GameName, s.SaveName)
					}
				}
			case "Remove":
				if opts.OnRemove != nil && opts.SelectedSave != "" {
					if s2 := saveByKey(saves, opts.SelectedSave); s2 != nil {
						opts.OnRemove(s2.GameName, s2.SaveName, idx)
					} else {
						opts.OnRemove(s.GameName, s.SaveName, idx)
					}
				}
			}
		},
	})
	opts.Overlay.SetTopCursor(cursor)
}

// saveByKey returns the SuspendInfo for "gameName/saveName", or nil if not found.
func saveByKey(saves []state.SuspendInfo, key string) *state.SuspendInfo {
	for i := range saves {
		if saves[i].GameName+"/"+saves[i].SaveName == key {
			return &saves[i]
		}
	}
	return nil
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

// ─── Standalone "Add..." dialogs for sub-menu use ────────────────────────────

// PushGameAddDialog opens an input dialog for adding a game by name or URL.
func PushGameAddDialog(overlay *widget.OverlayState, dataDir string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Game",
		Body:        "Enter a game name or URL:",
		InputPrompt: "Game",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushThemeAddDialog opens an input dialog for loading a theme by name.
func PushThemeAddDialog(overlay *widget.OverlayState, dataDir string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Theme",
		Body:        "Enter a theme name:",
		InputPrompt: "Theme",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushScriptAddDialog opens an input dialog for adding a plugin or shader.
func PushScriptAddDialog(overlay *widget.OverlayState, noun string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add " + noun,
		Body:        "Enter a " + strings.ToLower(noun) + " name or URL:",
		InputPrompt: noun,
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushSynthAddDialog opens an input dialog for adding a SoundFont.
func PushSynthAddDialog(overlay *widget.OverlayState, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add SoundFont",
		Body:        "Enter a SoundFont name:",
		InputPrompt: "SoundFont",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushFontAddDialog opens an input dialog for adding a Figlet font.
func PushFontAddDialog(overlay *widget.OverlayState, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Font",
		Body:        "Enter a font name:",
		InputPrompt: "Font",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}
