package widget

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/domain"
)


// ─── Shortcut-key helpers ──────────────────────────────────────────────────────

// StripAmpersand returns the display label with the "&" removed,
// and the lowercase shortcut character (or 0 if none).
// e.g. "&File" → ("File", 'f'), "E&xit" → ("Exit", 'x'), "Help" → ("Help", 0)
func StripAmpersand(label string) (string, rune) {
	idx := strings.IndexByte(label, '&')
	if idx < 0 || idx >= len(label)-1 {
		return label, 0
	}
	clean := label[:idx] + label[idx+1:]
	shortcut := rune(strings.ToLower(label[idx+1 : idx+2])[0])
	return clean, shortcut
}

// RenderLabel renders a label with the shortcut character underlined.
// base is the normal style; accent highlights the shortcut char.
func RenderLabel(label string, base, accent lipgloss.Style) string {
	idx := strings.IndexByte(label, '&')
	if idx < 0 || idx >= len(label)-1 {
		return base.Render(label)
	}
	before := label[:idx]
	hotkey := label[idx+1 : idx+2]
	after := label[idx+2:]
	return base.Render(before) + accent.Render(hotkey) + base.Render(after)
}

// MenuShortcut returns the shortcut rune for a MenuDef (from its Label).
func MenuShortcut(m domain.MenuDef) rune {
	_, r := StripAmpersand(m.Label)
	return r
}

// ItemShortcut returns the shortcut rune for a MenuItemDef (from its Label).
func ItemShortcut(it domain.MenuItemDef) rune {
	_, r := StripAmpersand(it.Label)
	return r
}

// HotkeyDisplay converts a key binding string (e.g. "ctrl+c") to a display
// string (e.g. "(Ctrl+C)").
func HotkeyDisplay(key string) string {
	parts := strings.Split(key, "+")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return "(" + strings.Join(parts, "+") + ")"
}

// TruncateStyled truncates a styled string to the given visual width.
func TruncateStyled(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "")
}

// ─── Overlay state ─────────────────────────────────────────────────────────────

// OverlayState holds all per-player NC overlay UI state.
type OverlayState struct {
	MenuFocused bool // F10 was pressed; action bar is focused
	MenuCursor  int  // which menu title is highlighted
	OpenMenu    int  // index of open dropdown (-1 = none)
	DropCursor  int  // focused item index in open dropdown

	// Dialog stack — each entry is a materialized NC Window.
	dialogs []*dialogEntry
}

// ShowDialogMsg is sent to a player's Bubble Tea program to push a dialog.
type ShowDialogMsg struct{ Dialog domain.DialogRequest }

func (o *OverlayState) HasDialog() bool { return len(o.dialogs) > 0 }

func (o *OverlayState) TopDialog() *domain.DialogRequest {
	e := o.topEntry()
	if e == nil {
		return nil
	}
	return &e.request
}

func (o *OverlayState) PushDialog(d domain.DialogRequest) {
	entry := o.buildDialogWindow(d)
	o.dialogs = append(o.dialogs, entry)
}

func (o *OverlayState) PopDialog() {
	o.popDialogEntry()
}

// IsActive returns true when any overlay is intercepting input.
func (o *OverlayState) IsActive() bool {
	return o.HasDialog() || o.MenuFocused || o.OpenMenu >= 0
}

// ─── Key handling ──────────────────────────────────────────────────────────────

// HandleKey routes a key press through the overlay state machine.
// Returns true if the key was consumed and normal chrome should not process it.
func (o *OverlayState) HandleKey(key string, menus []domain.MenuDef, playerID string) bool {
	// Global hotkeys: check all menu items for matching hotkey bindings.
	for _, m := range menus {
		for _, it := range m.Items {
			if it.Hotkey != "" && it.Hotkey == key && !it.Disabled && it.Handler != nil {
				o.OpenMenu = -1
				o.MenuFocused = false
				it.Handler(playerID)
				return true
			}
		}
	}

	if o.HasDialog() {
		// Dialog input is routed via HandleDialogMsg (real tea.Msg)
		// from chrome's Update handler. Consume the key here to prevent
		// it from reaching other handlers.
		return true
	}
	if key == "f10" {
		if o.MenuFocused || o.OpenMenu >= 0 {
			o.MenuFocused = false
			o.OpenMenu = -1
		} else {
			o.MenuFocused = true
			o.MenuCursor = 0
			o.OpenMenu = -1
		}
		return true
	}
	// Alt+letter opens a menu by its shortcut key (e.g. Alt+F for "&File").
	if strings.HasPrefix(key, "alt+") && len(key) == 5 {
		letter := rune(key[4])
		for i, m := range menus {
			if MenuShortcut(m) == letter {
				o.MenuFocused = true
				o.MenuCursor = i
				o.OpenMenu = i
				o.DropCursor = FirstSelectable(menus[i].Items)
				return true
			}
		}
	}
	if o.OpenMenu >= 0 {
		return o.handleDropdownKey(key, menus, playerID)
	}
	if o.MenuFocused {
		return o.handleMenuBarKey(key, menus)
	}
	return false
}

func (o *OverlayState) handleMenuBarKey(key string, menus []domain.MenuDef) bool {
	n := len(menus)
	if n == 0 {
		return true
	}
	switch key {
	case "left":
		if o.MenuCursor > 0 {
			o.MenuCursor--
		} else {
			o.MenuCursor = n - 1
		}
	case "right":
		o.MenuCursor = (o.MenuCursor + 1) % n
	case "down", "enter":
		o.OpenMenu = o.MenuCursor
		o.DropCursor = FirstSelectable(menus[o.MenuCursor].Items)
	case "esc":
		o.MenuFocused = false
	default:
		// Letter key → open menu by shortcut (e.g. "f" for "&File").
		if len(key) == 1 {
			letter := rune(key[0])
			for i, m := range menus {
				if MenuShortcut(m) == letter {
					o.MenuCursor = i
					o.OpenMenu = i
					o.DropCursor = FirstSelectable(menus[i].Items)
					return true
				}
			}
		}
	}
	return true // consume all keys while menu bar is focused
}

func (o *OverlayState) handleDropdownKey(key string, menus []domain.MenuDef, playerID string) bool {
	if o.OpenMenu >= len(menus) {
		return false
	}
	items := menus[o.OpenMenu].Items
	n := len(menus)
	switch key {
	case "up":
		o.DropCursor = PrevSelectable(items, o.DropCursor)
	case "down":
		o.DropCursor = NextSelectable(items, o.DropCursor)
	case "left":
		if o.MenuCursor > 0 {
			o.MenuCursor--
		} else {
			o.MenuCursor = n - 1
		}
		o.OpenMenu = o.MenuCursor
		o.DropCursor = FirstSelectable(menus[o.MenuCursor].Items)
	case "right":
		o.MenuCursor = (o.MenuCursor + 1) % n
		o.OpenMenu = o.MenuCursor
		o.DropCursor = FirstSelectable(menus[o.MenuCursor].Items)
	case "enter":
		if o.DropCursor >= 0 && o.DropCursor < len(items) {
			item := items[o.DropCursor]
			if !item.Disabled && item.Handler != nil {
				o.OpenMenu = -1
				o.MenuFocused = false
				item.Handler(playerID)
			}
		}
	case "esc":
		o.OpenMenu = -1
		// leave MenuFocused = true so user is back on the bar
	default:
		// Letter key → activate item by shortcut (e.g. "s" for "&Save").
		if len(key) == 1 {
			letter := rune(key[0])
			for i, it := range items {
				if !it.Disabled && !IsSeparator(it) && ItemShortcut(it) == letter {
					if it.Handler != nil {
						o.OpenMenu = -1
						o.MenuFocused = false
						it.Handler(playerID)
					}
					_ = i
					return true
				}
			}
		}
	}
	return true
}

// ─── Mouse handling ───────────────────────────────────────────────────────────

// HandleClick processes a left mouse click at screen position (x, y).
// ncBarRow is the screen row of the action bar. screenW/screenH are for dialog centering.
// Returns true if the click was consumed by the overlay.
func (o *OverlayState) HandleClick(x, y, ncBarRow, screenW, screenH int, menus []domain.MenuDef, playerID string) bool {
	// Priority 1: dialog (topmost overlay)
	if o.HasDialog() {
		return o.HandleDialogClick(x, y, screenW, screenH)
	}

	// Priority 2: open dropdown
	if o.OpenMenu >= 0 && o.OpenMenu < len(menus) {
		if o.handleDropdownClick(x, y, ncBarRow, menus, playerID) {
			return true
		}
		// Click outside dropdown — close it
		o.OpenMenu = -1
		o.MenuFocused = false
		// Fall through to check if click was on the bar itself
	}

	// Priority 3: action bar row
	if y == ncBarRow && len(menus) > 0 {
		pos := MenuBarPositions(menus)
		for i, m := range menus {
			clean, _ := StripAmpersand(m.Label)
			startX := pos[i]
			endX := startX + len(clean) + 2 // " label "
			if x >= startX && x < endX {
				o.MenuFocused = true
				o.MenuCursor = i
				o.OpenMenu = i
				o.DropCursor = FirstSelectable(menus[i].Items)
				return true
			}
		}
		// Click on bar but not on a menu title — just activate the bar
		o.MenuFocused = true
		return true
	}

	return false
}

func (o *OverlayState) handleDropdownClick(x, y, ncBarRow int, menus []domain.MenuDef, playerID string) bool {
	items := menus[o.OpenMenu].Items
	if len(items) == 0 {
		return false
	}

	// Dropdown position
	pos := MenuBarPositions(menus)
	ddCol := 0
	if o.OpenMenu < len(pos) {
		ddCol = pos[o.OpenMenu]
	}
	ddRow := ncBarRow + 1 // dropdown starts one row below bar

	// Calculate dropdown dimensions
	maxLW := 0
	for _, it := range items {
		if !IsSeparator(it) {
			clean, _ := StripAmpersand(it.Label)
			if len(clean) > maxLW {
				maxLW = len(clean)
			}
		}
	}
	innerW := maxLW + 2
	if innerW < 14 {
		innerW = 14
	}
	boxW := innerW + 2 // borders

	// Check if click is inside the dropdown box
	relX := x - ddCol
	relY := y - ddRow
	if relX < 0 || relX >= boxW || relY < 0 {
		return false
	}

	// Count rendered lines: top border + items (separators count as 1 line each) + bottom border
	lineIdx := 0
	for i, it := range items {
		lineIdx++ // each item/separator is one line (after top border)
		if relY == lineIdx && !IsSeparator(it) && !it.Disabled {
			if it.Handler != nil {
				o.DropCursor = i
				o.OpenMenu = -1
				o.MenuFocused = false
				it.Handler(playerID)
			}
			return true
		}
	}
	return relY <= lineIdx+1 // consumed if inside box area
}

// ─── Selectable-item helpers ───────────────────────────────────────────────────

func IsSeparator(item domain.MenuItemDef) bool {
	return strings.TrimLeft(item.Label, "-") == ""
}

func FirstSelectable(items []domain.MenuItemDef) int {
	for i, it := range items {
		if !IsSeparator(it) && !it.Disabled {
			return i
		}
	}
	return 0
}

func NextSelectable(items []domain.MenuItemDef, cur int) int {
	for i := cur + 1; i < len(items); i++ {
		if !IsSeparator(items[i]) && !items[i].Disabled {
			return i
		}
	}
	return cur
}

func PrevSelectable(items []domain.MenuItemDef, cur int) int {
	for i := cur - 1; i >= 0; i-- {
		if !IsSeparator(items[i]) && !items[i].Disabled {
			return i
		}
	}
	return cur
}

