package widget

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/domain"
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

// subMenuState tracks one level of open sub-menu.
type subMenuState struct {
	ParentIdx int // index of the item in the parent that opened this sub-menu
	Cursor    int // focused item index within this sub-menu
	ScrollOff int // scroll offset for this sub-menu
}

// OverlayState holds all per-player NC overlay UI state.
type OverlayState struct {
	MenuFocused bool // F10 was pressed; action bar is focused
	MenuCursor  int  // which menu title is highlighted
	OpenMenu    int  // index of open dropdown (-1 = none)

	// Menu stack — SubMenus[0] is the top-level dropdown, SubMenus[1] is its
	// first sub-menu, etc.  All levels use the same data structure and code path.
	SubMenus []subMenuState

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

// PushWindowDialog pushes a pre-built Window as a modal dialog.
// Esc dismisses the dialog; all other interactions are handled by the
// Window's child controls and their OnPress callbacks.
// win.Title is copied into the dialog request so DialogSize can account
// for the title width in its minimum-width calculation.
func (o *OverlayState) PushWindowDialog(win *Window) {
	entry := &dialogEntry{
		request: domain.DialogRequest{Title: win.Title},
		window:  win,
	}
	o.dialogs = append(o.dialogs, entry)
}

func (o *OverlayState) PopDialog() {
	o.popDialogEntry()
}

// SetTopCursor sets the cursor position on the focused control of the top dialog,
// if that control implements CursorPositioner. No-op otherwise.
func (o *OverlayState) SetTopCursor(idx int) {
	e := o.topEntry()
	if e == nil || e.window == nil {
		return
	}
	fi := e.window.FocusIdx
	if fi < 0 || fi >= len(e.window.Children) {
		return
	}
	if cp, ok := e.window.Children[fi].Control.(CursorPositioner); ok {
		cp.SetCursor(idx)
	}
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
				o.closeMenus()
				it.Handler(playerID)
				return true
			}
		}
	}

	if o.HasDialog() {
		return true
	}
	if key == "f10" {
		if o.MenuFocused || o.OpenMenu >= 0 {
			o.closeMenus()
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
				o.openDropdownMenu(menus, i)
				return true
			}
		}
	}
	if o.OpenMenu >= 0 && len(o.SubMenus) > 0 {
		return o.handleMenuKey(key, menus, playerID)
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
		o.openDropdownMenu(menus, o.MenuCursor)
	case "esc":
		o.MenuFocused = false
	default:
		if len(key) == 1 {
			letter := rune(key[0])
			for i, m := range menus {
				if MenuShortcut(m) == letter {
					o.MenuCursor = i
					o.openDropdownMenu(menus, i)
					return true
				}
			}
		}
	}
	return true
}

// openDropdownMenu opens a top-level menu as SubMenus[0].
func (o *OverlayState) openDropdownMenu(menus []domain.MenuDef, idx int) {
	o.MenuCursor = idx
	o.OpenMenu = idx
	o.SubMenus = []subMenuState{{
		ParentIdx: idx, // index into menus (for the dropdown level)
		Cursor:    FirstSelectable(menus[idx].Items),
		ScrollOff: 0,
	}}
}

// pushSubMenu pushes a sub-menu for the item at parentIdx in the given items list.
func (o *OverlayState) pushSubMenu(items []domain.MenuItemDef, parentIdx int) {
	subItems := items[parentIdx].SubItems
	o.SubMenus = append(o.SubMenus, subMenuState{
		ParentIdx: parentIdx,
		Cursor:    FirstSelectable(subItems),
		ScrollOff: 0,
	})
}

// handleMenuKey is the unified handler for all menu levels (dropdown + sub-menus).
// It operates on the deepest open level in the SubMenus stack.
func (o *OverlayState) handleMenuKey(key string, menus []domain.MenuDef, playerID string) bool {
	depth := len(o.SubMenus) - 1
	items := o.itemsAtDepth(menus, depth)
	if items == nil {
		o.closeMenus()
		return true
	}
	top := &o.SubMenus[depth]
	n := len(menus)

	switch key {
	case "up":
		prev := PrevSelectable(items, top.Cursor)
		if prev == top.Cursor && depth == 0 {
			// At top of dropdown — close, return to menu bar.
			o.SubMenus = nil
			o.OpenMenu = -1
		} else if prev != top.Cursor {
			top.Cursor = prev
		}
	case "down":
		top.Cursor = NextSelectable(items, top.Cursor)
	case "left":
		if depth == 0 {
			// Switch to previous top-level menu.
			idx := o.MenuCursor - 1
			if idx < 0 {
				idx = n - 1
			}
			o.openDropdownMenu(menus, idx)
		} else {
			// Pop one sub-menu level.
			o.SubMenus = o.SubMenus[:depth]
		}
	case "right":
		if top.Cursor >= 0 && top.Cursor < len(items) && HasSubMenu(items[top.Cursor]) {
			o.pushSubMenu(items, top.Cursor)
		} else if depth == 0 {
			// No sub-menu on this item — switch to next top-level menu.
			o.openDropdownMenu(menus, (o.MenuCursor+1)%n)
		}
	case "enter":
		if top.Cursor >= 0 && top.Cursor < len(items) {
			item := items[top.Cursor]
			if HasSubMenu(item) {
				o.pushSubMenu(items, top.Cursor)
			} else if !item.Disabled && item.Handler != nil {
				if !item.Toggle {
					o.closeMenus()
				}
				item.Handler(playerID)
			}
		}
	case "delete":
		if top.Cursor >= 0 && top.Cursor < len(items) && items[top.Cursor].OnDelete != nil {
			cb := items[top.Cursor].OnDelete
			o.closeMenus()
			cb(playerID)
		}
	case "esc":
		o.SubMenus = nil
		o.OpenMenu = -1
		// leave MenuFocused = true
	default:
		if len(key) == 1 {
			letter := rune(key[0])
			for i, it := range items {
				if !it.Disabled && !IsSeparator(it) && ItemShortcut(it) == letter {
					top.Cursor = i
					if HasSubMenu(it) {
						o.pushSubMenu(items, i)
					} else if it.Handler != nil {
						if !it.Toggle {
							o.closeMenus()
						}
						it.Handler(playerID)
					}
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
	if o.HasDialog() {
		return o.HandleDialogClick(x, y, screenW, screenH)
	}

	// Check open menus (all levels, deepest first).
	if o.OpenMenu >= 0 && len(o.SubMenus) > 0 {
		if o.handleMenuClick(x, y, ncBarRow, screenW, menus, playerID) {
			return true
		}
		o.closeMenus()
		// Fall through to check bar
	}

	// Action bar row.
	if y == ncBarRow && len(menus) > 0 {
		pos := MenuBarPositions(menus)
		for i, m := range menus {
			clean, _ := StripAmpersand(m.Label)
			if x >= pos[i] && x < pos[i]+len(clean)+2 {
				o.MenuFocused = true
				o.openDropdownMenu(menus, i)
				return true
			}
		}
		o.MenuFocused = true
		return true
	}
	return false
}

// dropdownInnerWidth computes the inner width for a set of menu items.
func dropdownInnerWidth(items []domain.MenuItemDef) int {
	hasToggles := false
	for _, it := range items {
		if it.Toggle {
			hasToggles = true
			break
		}
	}
	checkW := 0
	if hasToggles {
		checkW = 2
	}
	maxLW := 0
	for _, it := range items {
		if IsSeparator(it) {
			continue
		}
		clean, _ := StripAmpersand(it.Label)
		w := len(clean)
		if it.Hotkey != "" {
			w += 2 + len(HotkeyDisplay(it.Hotkey))
		}
		if HasSubMenu(it) {
			w += 2
		}
		if w > maxLW {
			maxLW = w
		}
	}
	innerW := maxLW + checkW + 2
	if innerW < 14 {
		innerW = 14
	}
	return innerW
}

// handleMenuClick checks all open menu levels (deepest first) for a click hit.
func (o *OverlayState) handleMenuClick(x, y, ncBarRow, screenW int, menus []domain.MenuDef, playerID string) bool {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) || len(o.SubMenus) == 0 {
		return false
	}

	// Compute positions for each level (same as rendering).
	type levelInfo struct {
		col, row, boxW int
		items          []domain.MenuItemDef
	}
	var levels []levelInfo

	pos := MenuBarPositions(menus)
	col := 0
	if o.OpenMenu < len(pos) {
		col = pos[o.OpenMenu]
	}
	items := menus[o.OpenMenu].Items
	row := ncBarRow + 1
	boxW := dropdownInnerWidth(items) + 2
	if col+boxW > screenW {
		col = max(0, screenW-boxW)
	}
	levels = append(levels, levelInfo{col: col, row: row, boxW: boxW, items: items})

	for i := 1; i < len(o.SubMenus); i++ {
		sm := &o.SubMenus[i]
		if sm.ParentIdx < 0 || sm.ParentIdx >= len(items) || !HasSubMenu(items[sm.ParentIdx]) {
			break
		}
		subItems := items[sm.ParentIdx].SubItems
		subW := dropdownInnerWidth(subItems) + 2
		prevLv := levels[len(levels)-1]
		subCol := prevLv.col + prevLv.boxW
		subRow := prevLv.row + (sm.ParentIdx - o.SubMenus[i-1].ScrollOff) + 1
		if subCol+subW > screenW {
			subCol = max(0, prevLv.col-subW)
		}
		levels = append(levels, levelInfo{col: subCol, row: subRow, boxW: subW, items: subItems})
		items = subItems
	}

	// Check deepest to shallowest.
	for depth := len(levels) - 1; depth >= 0; depth-- {
		lv := levels[depth]
		hit := clickInMenu(x, y, lv.col, lv.row, lv.boxW, lv.items, func(i int, it domain.MenuItemDef) {
			o.SubMenus[depth].Cursor = i
			if HasSubMenu(it) {
				o.SubMenus = o.SubMenus[:depth+1]
				o.pushSubMenu(lv.items, i)
			} else if it.Handler != nil {
				if !it.Toggle {
					o.closeMenus()
				}
				it.Handler(playerID)
			}
		})
		if hit {
			return true
		}
	}
	return false
}

// clickInMenu tests if (x,y) hits an item in a menu box at (col,row) with width boxW.
func clickInMenu(x, y, col, row, boxW int, items []domain.MenuItemDef, onHit func(int, domain.MenuItemDef)) bool {
	relX := x - col
	relY := y - row
	if relX < 0 || relX >= boxW || relY < 0 {
		return false
	}
	lineIdx := 0
	for i, it := range items {
		lineIdx++
		if relY == lineIdx && !IsSeparator(it) && !it.Disabled {
			onHit(i, it)
			return true
		}
	}
	return relY <= lineIdx+1
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

// HasSubMenu returns true if the item has a non-empty SubItems slice.
func HasSubMenu(item domain.MenuItemDef) bool {
	return len(item.SubItems) > 0 && !IsSeparator(item)
}

// ─── Sub-menu helpers ─────────────────────────────────────────────────────────

// itemsAtDepth returns the menu items at the given depth in the SubMenus stack.
// depth 0 = the top-level dropdown items, depth 1+ = sub-menu items.
func (o *OverlayState) itemsAtDepth(menus []domain.MenuDef, depth int) []domain.MenuItemDef {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) || depth < 0 || depth >= len(o.SubMenus) {
		return nil
	}
	// Depth 0 = dropdown items.
	items := menus[o.OpenMenu].Items
	// Walk the stack from depth 1 onward.
	for i := 1; i <= depth; i++ {
		idx := o.SubMenus[i].ParentIdx
		if idx < 0 || idx >= len(items) || !HasSubMenu(items[idx]) {
			return nil
		}
		items = items[idx].SubItems
	}
	return items
}

// closeMenus closes everything: sub-menus, dropdown, and menu bar focus.
func (o *OverlayState) closeMenus() {
	o.SubMenus = nil
	o.OpenMenu = -1
	o.MenuFocused = false
}

// ensureMenuVisible adjusts scrollOff so cursor is within [scrollOff, scrollOff+maxVisible).
func ensureMenuVisible(cursor, scrollOff, maxVisible int) int {
	if maxVisible <= 0 {
		return 0
	}
	if cursor < scrollOff {
		return cursor
	}
	if cursor >= scrollOff+maxVisible {
		return cursor - maxVisible + 1
	}
	return scrollOff
}

// maxDropdownItems returns the max visible items for a dropdown anchored at anchorRow.
// Leaves 2 rows for top/bottom border and 1 row margin at screen bottom.
func maxDropdownItems(screenH, anchorRow int) int {
	avail := screenH - anchorRow - 3
	if avail < 3 {
		avail = 3
	}
	return avail
}

