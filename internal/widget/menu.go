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
	MenuFocused   bool // F10 was pressed; action bar is focused
	MenuCursor    int  // which menu title is highlighted
	OpenMenu      int  // index of open dropdown (-1 = none)
	DropCursor    int  // focused item index in open dropdown
	DropScrollOff int  // scroll offset for the top-level dropdown

	// Sub-menu stack — index 0 is a child of the dropdown, index 1 is a
	// child of index 0, etc.  Supports arbitrary nesting depth.
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
		// Dialog input is routed via HandleDialogMsg (real tea.Msg)
		// from chrome's Update handler. Consume the key here to prevent
		// it from reaching other handlers.
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
				o.closeSubMenus()
				o.MenuFocused = true
				o.MenuCursor = i
				o.OpenMenu = i
				o.DropCursor = FirstSelectable(menus[i].Items)
				o.DropScrollOff = 0
				return true
			}
		}
	}
	if o.OpenMenu >= 0 {
		// Route to deepest open sub-menu first.
		if len(o.SubMenus) > 0 {
			return o.handleSubMenuKey(key, menus, playerID)
		}
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
		o.DropScrollOff = 0
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
					o.DropScrollOff = 0
					return true
				}
			}
		}
	}
	return true // consume all keys while menu bar is focused
}

// openDropdownMenu switches to a different top-level menu and resets sub-menus.
func (o *OverlayState) openDropdownMenu(menus []domain.MenuDef, idx int) {
	o.closeSubMenus()
	o.MenuCursor = idx
	o.OpenMenu = idx
	o.DropCursor = FirstSelectable(menus[idx].Items)
	o.DropScrollOff = 0
}

// openSubMenu pushes a sub-menu for the item at parentIdx in the current deepest menu.
func (o *OverlayState) openSubMenu(items []domain.MenuItemDef, parentIdx int) {
	subItems := items[parentIdx].SubItems
	o.SubMenus = append(o.SubMenus, subMenuState{
		ParentIdx: parentIdx,
		Cursor:    FirstSelectable(subItems),
		ScrollOff: 0,
	})
}

// autoOpenSubMenu opens a sub-menu for the current cursor item if it has SubItems,
// otherwise closes any open sub-menus.
func (o *OverlayState) autoOpenSubMenu(items []domain.MenuItemDef, cursor int) {
	o.closeSubMenus()
	if cursor >= 0 && cursor < len(items) && HasSubMenu(items[cursor]) {
		o.openSubMenu(items, cursor)
	}
}

func (o *OverlayState) handleDropdownKey(key string, menus []domain.MenuDef, playerID string) bool {
	if o.OpenMenu >= len(menus) {
		return false
	}
	items := menus[o.OpenMenu].Items
	n := len(menus)
	switch key {
	case "up":
		prev := PrevSelectable(items, o.DropCursor)
		if prev == o.DropCursor {
			// Already at top — close dropdown, return focus to menu bar.
			o.closeSubMenus()
			o.OpenMenu = -1
			o.DropScrollOff = 0
		} else {
			o.DropCursor = prev
			o.closeSubMenus()
		}
	case "down":
		o.DropCursor = NextSelectable(items, o.DropCursor)
		o.closeSubMenus()
	case "left":
		idx := o.MenuCursor - 1
		if idx < 0 {
			idx = n - 1
		}
		o.openDropdownMenu(menus, idx)
	case "right":
		// If focused item has a sub-menu, open it instead of switching menus.
		if o.DropCursor >= 0 && o.DropCursor < len(items) && HasSubMenu(items[o.DropCursor]) {
			o.openSubMenu(items, o.DropCursor)
			return true
		}
		o.openDropdownMenu(menus, (o.MenuCursor+1)%n)
	case "enter":
		if o.DropCursor >= 0 && o.DropCursor < len(items) {
			item := items[o.DropCursor]
			if HasSubMenu(item) {
				o.openSubMenu(items, o.DropCursor)
			} else if !item.Disabled && item.Handler != nil {
				if !item.Toggle {
					o.closeMenus()
				}
				item.Handler(playerID)
			}
		}
	case "delete":
		if o.DropCursor >= 0 && o.DropCursor < len(items) {
			if items[o.DropCursor].OnDelete != nil {
				cb := items[o.DropCursor].OnDelete
				o.closeMenus()
				cb(playerID)
			}
		}
	case "esc":
		o.closeSubMenus()
		o.OpenMenu = -1
		o.DropScrollOff = 0
		// leave MenuFocused = true so user is back on the bar
	default:
		// Letter key → activate item by shortcut (e.g. "s" for "&Save").
		if len(key) == 1 {
			letter := rune(key[0])
			for _, it := range items {
				if !it.Disabled && !IsSeparator(it) && ItemShortcut(it) == letter {
					if HasSubMenu(it) {
						// Find index and open sub-menu.
						for j, it2 := range items {
							if &it2 == &it || it2.Label == it.Label {
								o.DropCursor = j
								o.openSubMenu(items, j)
								break
							}
						}
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

// handleSubMenuKey handles input for the deepest open sub-menu.
func (o *OverlayState) handleSubMenuKey(key string, menus []domain.MenuDef, playerID string) bool {
	if len(o.SubMenus) == 0 {
		return false
	}
	items := o.resolveSubItems(menus)
	if items == nil {
		o.closeSubMenus()
		return true
	}
	top := &o.SubMenus[len(o.SubMenus)-1]

	switch key {
	case "up":
		prev := PrevSelectable(items, top.Cursor)
		if prev == top.Cursor {
			// At top — stay (don't close sub-menu on up at top)
		} else {
			top.Cursor = prev
		}
	case "down":
		top.Cursor = NextSelectable(items, top.Cursor)
	case "right":
		// If focused item has a sub-menu, open it (deeper nesting).
		if top.Cursor >= 0 && top.Cursor < len(items) && HasSubMenu(items[top.Cursor]) {
			o.openSubMenu(items, top.Cursor)
		}
	case "left":
		// Pop one sub-menu level, return to parent.
		o.SubMenus = o.SubMenus[:len(o.SubMenus)-1]
	case "enter":
		if top.Cursor >= 0 && top.Cursor < len(items) {
			item := items[top.Cursor]
			if HasSubMenu(item) {
				o.openSubMenu(items, top.Cursor)
			} else if !item.Disabled && item.Handler != nil {
				if !item.Toggle {
					o.closeMenus()
				}
				item.Handler(playerID)
			}
		}
	case "delete":
		if top.Cursor >= 0 && top.Cursor < len(items) {
			if items[top.Cursor].OnDelete != nil {
				cb := items[top.Cursor].OnDelete
				o.closeMenus()
				cb(playerID)
			}
		}
	case "esc":
		o.closeSubMenus()
		o.OpenMenu = -1
		o.DropScrollOff = 0
		// leave MenuFocused = true
	default:
		// Letter key → activate item by shortcut.
		if len(key) == 1 {
			letter := rune(key[0])
			for i, it := range items {
				if !it.Disabled && !IsSeparator(it) && ItemShortcut(it) == letter {
					if HasSubMenu(it) {
						top.Cursor = i
						o.openSubMenu(items, i)
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
	// Priority 1: dialog (topmost overlay)
	if o.HasDialog() {
		return o.HandleDialogClick(x, y, screenW, screenH)
	}

	// Priority 2: open dropdown (and any sub-menus)
	if o.OpenMenu >= 0 && o.OpenMenu < len(menus) {
		if o.handleDropdownClick(x, y, ncBarRow, menus, playerID) {
			return true
		}
		// Click outside all menus — close everything
		o.closeMenus()
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
				o.closeSubMenus()
				o.MenuFocused = true
				o.MenuCursor = i
				o.OpenMenu = i
				o.DropCursor = FirstSelectable(menus[i].Items)
				o.DropScrollOff = 0
				return true
			}
		}
		// Click on bar but not on a menu title — just activate the bar
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
			w += 2 // " ►"
		}
		if w > maxLW {
			maxLW = w
		}
	}
	innerW := maxLW + checkW + 2 // padding each side
	if innerW < 14 {
		innerW = 14
	}
	return innerW
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
	ddRow := ncBarRow + 1
	boxW := dropdownInnerWidth(items) + 2 // + borders

	// Check if click is inside the dropdown box.
	relX := x - ddCol
	relY := y - ddRow
	if relX >= 0 && relX < boxW && relY >= 0 {
		lineIdx := 0
		for i, it := range items {
			lineIdx++
			if relY == lineIdx && !IsSeparator(it) && !it.Disabled {
				o.DropCursor = i
				if HasSubMenu(it) {
					o.closeSubMenus()
					o.openSubMenu(items, i)
				} else if it.Handler != nil {
					if !it.Toggle {
						o.closeMenus()
					}
					it.Handler(playerID)
				}
				return true
			}
		}
		return relY <= lineIdx+1
	}

	// TODO: sub-menu click handling (will be refined in rendering step
	// once we know the exact positions of sub-menu boxes).

	return false
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

// resolveSubItems returns the items at the given sub-menu depth.
// depth 0 = the top-level dropdown items, depth 1 = first sub-menu, etc.
func (o *OverlayState) resolveSubItems(menus []domain.MenuDef) []domain.MenuItemDef {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) {
		return nil
	}
	items := menus[o.OpenMenu].Items
	for i := 0; i < len(o.SubMenus); i++ {
		idx := o.SubMenus[i].ParentIdx
		if idx < 0 || idx >= len(items) || !HasSubMenu(items[idx]) {
			return nil
		}
		items = items[idx].SubItems
	}
	return items
}

// resolveSubItemsAt returns the items at a specific depth in the sub-menu stack.
// depth 0 = dropdown items, depth 1 = SubMenus[0]'s children, etc.
func (o *OverlayState) resolveSubItemsAt(menus []domain.MenuDef, depth int) []domain.MenuItemDef {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) {
		return nil
	}
	items := menus[o.OpenMenu].Items
	for i := 0; i < depth && i < len(o.SubMenus); i++ {
		idx := o.SubMenus[i].ParentIdx
		if idx < 0 || idx >= len(items) || !HasSubMenu(items[idx]) {
			return nil
		}
		items = items[idx].SubItems
	}
	return items
}

// closeSubMenus clears all open sub-menus.
func (o *OverlayState) closeSubMenus() {
	o.SubMenus = nil
}

// closeMenus closes everything: sub-menus, dropdown, and menu bar focus.
func (o *OverlayState) closeMenus() {
	o.SubMenus = nil
	o.OpenMenu = -1
	o.DropScrollOff = 0
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

