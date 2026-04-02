package server

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
	"null-space/internal/theme"
)


// ─── Shortcut-key helpers ──────────────────────────────────────────────────────

// stripAmpersand returns the display label with the "&" removed,
// and the lowercase shortcut character (or 0 if none).
// e.g. "&File" → ("File", 'f'), "E&xit" → ("Exit", 'x'), "Help" → ("Help", 0)
func stripAmpersand(label string) (string, rune) {
	idx := strings.IndexByte(label, '&')
	if idx < 0 || idx >= len(label)-1 {
		return label, 0
	}
	clean := label[:idx] + label[idx+1:]
	shortcut := rune(strings.ToLower(label[idx+1 : idx+2])[0])
	return clean, shortcut
}

// renderLabel renders a label with the shortcut character underlined.
// base is the normal style; accent highlights the shortcut char.
func renderLabel(label string, base, accent lipgloss.Style) string {
	idx := strings.IndexByte(label, '&')
	if idx < 0 || idx >= len(label)-1 {
		return base.Render(label)
	}
	before := label[:idx]
	hotkey := label[idx+1 : idx+2]
	after := label[idx+2:]
	return base.Render(before) + accent.Render(hotkey) + base.Render(after)
}

// menuShortcut returns the shortcut rune for a MenuDef (from its Label).
func menuShortcut(m common.MenuDef) rune {
	_, r := stripAmpersand(m.Label)
	return r
}

// itemShortcut returns the shortcut rune for a MenuItemDef (from its Label).
func itemShortcut(it common.MenuItemDef) rune {
	_, r := stripAmpersand(it.Label)
	return r
}

// hotkeyDisplay converts a key binding string (e.g. "ctrl+c") to a display
// string (e.g. "(Ctrl+C)").
func hotkeyDisplay(key string) string {
	parts := strings.Split(key, "+")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return "(" + strings.Join(parts, "+") + ")"
}

// ─── Overlay state ─────────────────────────────────────────────────────────────

// overlayState holds all per-player NC overlay UI state.
type overlayState struct {
	menuFocused bool // F10 was pressed; action bar is focused
	menuCursor  int  // which menu title is highlighted
	openMenu    int  // index of open dropdown (-1 = none)
	dropCursor  int  // focused item index in open dropdown

	dialogs     []common.DialogRequest
	dialogFocus int // focused button in top dialog
}

// showDialogMsg is sent to a player's Bubble Tea program to push a dialog.
type showDialogMsg struct{ dialog common.DialogRequest }

func (o *overlayState) hasDialog() bool { return len(o.dialogs) > 0 }

func (o *overlayState) topDialog() *common.DialogRequest {
	if len(o.dialogs) == 0 {
		return nil
	}
	return &o.dialogs[len(o.dialogs)-1]
}

func (o *overlayState) pushDialog(d common.DialogRequest) {
	o.dialogs = append(o.dialogs, d)
	o.dialogFocus = 0
}

func (o *overlayState) popDialog() {
	if len(o.dialogs) > 0 {
		o.dialogs = o.dialogs[:len(o.dialogs)-1]
		o.dialogFocus = 0
	}
}

// isActive returns true when any overlay is intercepting input.
func (o *overlayState) isActive() bool {
	return o.hasDialog() || o.menuFocused || o.openMenu >= 0
}

// ─── Key handling ──────────────────────────────────────────────────────────────

// handleKey routes a key press through the overlay state machine.
// Returns true if the key was consumed and normal chrome should not process it.
func (o *overlayState) handleKey(key string, menus []common.MenuDef, playerID string) bool {
	// Global hotkeys: check all menu items for matching hotkey bindings.
	for _, m := range menus {
		for _, it := range m.Items {
			if it.Hotkey != "" && it.Hotkey == key && !it.Disabled && it.Handler != nil {
				o.openMenu = -1
				o.menuFocused = false
				it.Handler(playerID)
				return true
			}
		}
	}

	if o.hasDialog() {
		return o.handleDialogKey(key)
	}
	if key == "f10" {
		if o.menuFocused || o.openMenu >= 0 {
			o.menuFocused = false
			o.openMenu = -1
		} else {
			o.menuFocused = true
			o.menuCursor = 0
			o.openMenu = -1
		}
		return true
	}
	// Alt+letter opens a menu by its shortcut key (e.g. Alt+F for "&File").
	if strings.HasPrefix(key, "alt+") && len(key) == 5 {
		letter := rune(key[4])
		for i, m := range menus {
			if menuShortcut(m) == letter {
				o.menuFocused = true
				o.menuCursor = i
				o.openMenu = i
				o.dropCursor = firstSelectable(menus[i].Items)
				return true
			}
		}
	}
	if o.openMenu >= 0 {
		return o.handleDropdownKey(key, menus, playerID)
	}
	if o.menuFocused {
		return o.handleMenuBarKey(key, menus)
	}
	return false
}

func (o *overlayState) handleMenuBarKey(key string, menus []common.MenuDef) bool {
	n := len(menus)
	if n == 0 {
		return true
	}
	switch key {
	case "left":
		if o.menuCursor > 0 {
			o.menuCursor--
		} else {
			o.menuCursor = n - 1
		}
	case "right":
		o.menuCursor = (o.menuCursor + 1) % n
	case "down", "enter":
		o.openMenu = o.menuCursor
		o.dropCursor = firstSelectable(menus[o.menuCursor].Items)
	case "esc":
		o.menuFocused = false
	default:
		// Letter key → open menu by shortcut (e.g. "f" for "&File").
		if len(key) == 1 {
			letter := rune(key[0])
			for i, m := range menus {
				if menuShortcut(m) == letter {
					o.menuCursor = i
					o.openMenu = i
					o.dropCursor = firstSelectable(menus[i].Items)
					return true
				}
			}
		}
	}
	return true // consume all keys while menu bar is focused
}

func (o *overlayState) handleDropdownKey(key string, menus []common.MenuDef, playerID string) bool {
	if o.openMenu >= len(menus) {
		return false
	}
	items := menus[o.openMenu].Items
	n := len(menus)
	switch key {
	case "up":
		o.dropCursor = prevSelectable(items, o.dropCursor)
	case "down":
		o.dropCursor = nextSelectable(items, o.dropCursor)
	case "left":
		if o.menuCursor > 0 {
			o.menuCursor--
		} else {
			o.menuCursor = n - 1
		}
		o.openMenu = o.menuCursor
		o.dropCursor = firstSelectable(menus[o.menuCursor].Items)
	case "right":
		o.menuCursor = (o.menuCursor + 1) % n
		o.openMenu = o.menuCursor
		o.dropCursor = firstSelectable(menus[o.menuCursor].Items)
	case "enter":
		if o.dropCursor >= 0 && o.dropCursor < len(items) {
			item := items[o.dropCursor]
			if !item.Disabled && item.Handler != nil {
				o.openMenu = -1
				o.menuFocused = false
				item.Handler(playerID)
			}
		}
	case "esc":
		o.openMenu = -1
		// leave menuFocused = true so user is back on the bar
	default:
		// Letter key → activate item by shortcut (e.g. "s" for "&Save").
		if len(key) == 1 {
			letter := rune(key[0])
			for i, it := range items {
				if !it.Disabled && !isSeparator(it) && itemShortcut(it) == letter {
					if it.Handler != nil {
						o.openMenu = -1
						o.menuFocused = false
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

func (o *overlayState) handleDialogKey(key string) bool {
	d := o.topDialog()
	if d == nil {
		return false
	}
	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}
	switch key {
	case "tab":
		o.dialogFocus = (o.dialogFocus + 1) % len(btns)
	case "left":
		if o.dialogFocus > 0 {
			o.dialogFocus--
		}
	case "right":
		if o.dialogFocus < len(btns)-1 {
			o.dialogFocus++
		}
	case "enter", " ":
		label := btns[o.dialogFocus]
		cb := d.OnClose
		o.popDialog()
		if cb != nil {
			cb(label)
		}
	case "esc":
		cb := d.OnClose
		o.popDialog()
		if cb != nil {
			cb("")
		}
	}
	return true
}

// ─── Mouse handling ───────────────────────────────────────────────────────────

// handleClick processes a left mouse click at screen position (x, y).
// ncBarRow is the screen row of the action bar. screenW/screenH are for dialog centering.
// Returns true if the click was consumed by the overlay.
func (o *overlayState) handleClick(x, y, ncBarRow, screenW, screenH int, menus []common.MenuDef, playerID string) bool {
	// Priority 1: dialog (topmost overlay)
	if o.hasDialog() {
		return o.handleDialogClick(x, y, screenW, screenH)
	}

	// Priority 2: open dropdown
	if o.openMenu >= 0 && o.openMenu < len(menus) {
		if o.handleDropdownClick(x, y, ncBarRow, menus, playerID) {
			return true
		}
		// Click outside dropdown — close it
		o.openMenu = -1
		o.menuFocused = false
		// Fall through to check if click was on the bar itself
	}

	// Priority 3: action bar row
	if y == ncBarRow && len(menus) > 0 {
		pos := ncBarMenuPositions(menus)
		for i, m := range menus {
			clean, _ := stripAmpersand(m.Label)
			startX := pos[i]
			endX := startX + len(clean) + 2 // " label "
			if x >= startX && x < endX {
				o.menuFocused = true
				o.menuCursor = i
				o.openMenu = i
				o.dropCursor = firstSelectable(menus[i].Items)
				return true
			}
		}
		// Click on bar but not on a menu title — just activate the bar
		o.menuFocused = true
		return true
	}

	return false
}

func (o *overlayState) handleDropdownClick(x, y, ncBarRow int, menus []common.MenuDef, playerID string) bool {
	items := menus[o.openMenu].Items
	if len(items) == 0 {
		return false
	}

	// Dropdown position
	pos := ncBarMenuPositions(menus)
	ddCol := 0
	if o.openMenu < len(pos) {
		ddCol = pos[o.openMenu]
	}
	ddRow := ncBarRow + 1 // dropdown starts one row below bar

	// Calculate dropdown dimensions
	maxLW := 0
	for _, it := range items {
		if !isSeparator(it) {
			clean, _ := stripAmpersand(it.Label)
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
		if relY == lineIdx && !isSeparator(it) && !it.Disabled {
			if it.Handler != nil {
				o.dropCursor = i
				o.openMenu = -1
				o.menuFocused = false
				it.Handler(playerID)
			}
			return true
		}
	}
	return relY <= lineIdx+1 // consumed if inside box area
}

func (o *overlayState) handleDialogClick(x, y, screenW, screenH int) bool {
	d := o.topDialog()
	if d == nil {
		return false
	}
	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}

	// Recalculate dialog dimensions (mirrors renderDialog logic)
	bodyLines := strings.Split(d.Body, "\n")
	maxBodyW := 0
	for _, l := range bodyLines {
		w := ansi.StringWidth(l)
		if w > maxBodyW {
			maxBodyW = w
		}
	}
	btnW := 0
	for _, b := range btns {
		btnW += len(b) + 4 // "[ b ]"
	}
	btnW += (len(btns) - 1) * 2

	innerW := maxBodyW + 2
	if ansi.StringWidth(d.Title)+2 > innerW {
		innerW = ansi.StringWidth(d.Title) + 2
	}
	if btnW+2 > innerW {
		innerW = btnW + 2
	}
	if innerW < 22 {
		innerW = 22
	}

	// Must match renderDialog's calculation exactly (no shadow offset).
	totalW := innerW + 2
	totalH := 3 + len(bodyLines) + 3 // top + title + sep + body + sep + buttons + bottom
	col := (screenW - totalW) / 2
	row := (screenH - totalH) / 2
	if row < 2 {
		row = 2
	}

	// Check if click is on the button row
	// Button row is: top(1) + title(1) + sep(1) + body(len) + sep(1) = 4 + len(bodyLines)
	btnRow := row + 4 + len(bodyLines)
	if y == btnRow {
		// Calculate button positions within the row
		lpad := (innerW - btnW) / 2
		bx := col + 1 + lpad // col + left border + left padding
		for i, b := range btns {
			bw := len(b) + 4 // "[ b ]"
			if x >= bx && x < bx+bw {
				label := btns[i]
				cb := d.OnClose
				o.popDialog()
				if cb != nil {
					cb(label)
				}
				return true
			}
			bx += bw + 2 // button width + gap
		}
	}

	// Any click inside the dialog area (including shadow) consumes the event.
	relX := x - col
	relY := y - row
	return relX >= 0 && relX < totalW+1 && relY >= 0 && relY < totalH+1
}

// ─── Selectable-item helpers ───────────────────────────────────────────────────

func isSeparator(item common.MenuItemDef) bool {
	return strings.TrimLeft(item.Label, "-") == ""
}

func firstSelectable(items []common.MenuItemDef) int {
	for i, it := range items {
		if !isSeparator(it) && !it.Disabled {
			return i
		}
	}
	return 0
}

func nextSelectable(items []common.MenuItemDef, cur int) int {
	for i := cur + 1; i < len(items); i++ {
		if !isSeparator(items[i]) && !items[i].Disabled {
			return i
		}
	}
	return cur
}

func prevSelectable(items []common.MenuItemDef, cur int) int {
	for i := cur - 1; i >= 0; i-- {
		if !isSeparator(items[i]) && !items[i].Disabled {
			return i
		}
	}
	return cur
}

// ─── Overlay box ─────────────────────────────────────────────────────────────

// overlayBox bundles an overlay's rendered content with its position and
// pre-computed dimensions so callers don't need to split the string.
type overlayBox struct {
	content       string
	col, row      int
	width, height int
}

// ─── Menu bar rendering ────────────────────────────────────────────────────────

// renderNCBar renders the NC-style action bar row (full terminal width).
func (o *overlayState) renderNCBar(width int, menus []common.MenuDef, layer *theme.Layer) string {
	barStyle     := layer.BaseStyle()
	activeStyle  := layer.HighlightStyle()
	barAccent    := layer.AccentStyle()
	activeAccent := lipgloss.NewStyle().Background(layer.HighlightBgC()).Foreground(layer.AccentC()).Bold(true).Underline(true)

	var sb strings.Builder
	for i, m := range menus {
		if i > 0 {
			sb.WriteString(barStyle.Render(layer.Sep()))
		}
		focused := (o.menuFocused || o.openMenu >= 0) && i == o.menuCursor
		if focused {
			sb.WriteString(activeStyle.Render(" "))
			sb.WriteString(renderLabel(m.Label, activeStyle, activeAccent))
			sb.WriteString(activeStyle.Render(" "))
		} else {
			sb.WriteString(barStyle.Render(" "))
			sb.WriteString(renderLabel(m.Label, barStyle, barAccent))
			sb.WriteString(barStyle.Render(" "))
		}
	}
	content := sb.String()
	cw := lipgloss.Width(content)
	if cw < width {
		content += barStyle.Width(width - cw).Render("")
	}
	return truncateStyled(content, width)
}

// ncBarMenuPositions returns the starting x column of each menu title in the bar.
func ncBarMenuPositions(menus []common.MenuDef) []int {
	pos := make([]int, len(menus))
	x := 0
	for i, m := range menus {
		pos[i] = x
		clean, _ := stripAmpersand(m.Label)
		x += 1 + len(clean) + 1 // " label " = 2 + len
		if i < len(menus)-1 {
			x++ // separator
		}
	}
	return pos
}

// ─── Dropdown rendering ────────────────────────────────────────────────────────

// renderDropdown returns (overlayString, col, row) for PlaceOverlay.
// ncBarRow is the screen row (0-based) of the NC action bar.
func (o *overlayState) renderDropdown(menus []common.MenuDef, ncBarRow int, layer *theme.Layer) overlayBox {
	if o.openMenu < 0 || o.openMenu >= len(menus) {
		return overlayBox{}
	}
	items := menus[o.openMenu].Items
	if len(items) == 0 {
		return overlayBox{}
	}

	// Check if any item is a toggle (need checkmark column).
	hasToggles := false
	for _, it := range items {
		if it.Toggle {
			hasToggles = true
			break
		}
	}
	checkW := 0
	if hasToggles {
		checkW = 2 // "√ " or "  "
	}

	maxLW := 0
	for _, it := range items {
		if !isSeparator(it) {
			clean, _ := stripAmpersand(it.Label)
			w := len(clean)
			if it.Hotkey != "" {
				w += 2 + len(hotkeyDisplay(it.Hotkey))
			}
			if w > maxLW {
				maxLW = w
			}
		}
	}
	innerW := maxLW + checkW + 2 // checkmark + 1-space padding each side
	if innerW < 14 {
		innerW = 14
	}

	menuStyle     := layer.BaseStyle()
	activeStyle   := layer.HighlightStyle()
	disabledStyle := layer.DisabledStyle()

	top    := menuStyle.Render(layer.OTL() + strings.Repeat(layer.OH(), innerW) + layer.OTR())
	bottom := menuStyle.Render(layer.OBL() + strings.Repeat(layer.OH(), innerW) + layer.OBR())
	// Menu separators don't connect to the outer border (unlike panel dividers).
	sepRow := menuStyle.Render(layer.OV() + strings.Repeat(layer.IH(), innerW) + layer.OV())

	var lines []string
	lines = append(lines, top)

	menuAccent  := layer.AccentStyle()
	activeAccent := lipgloss.NewStyle().Background(layer.HighlightBgC()).Foreground(layer.AccentC()).Bold(true).Underline(true)

	for i, it := range items {
		if isSeparator(it) {
			lines = append(lines, sepRow)
			continue
		}

		// Checkmark prefix for toggle items.
		check := ""
		if hasToggles {
			if it.Toggle && it.Checked != nil && it.Checked() {
				check = "√ "
			} else {
				check = "  "
			}
		}

		clean, _ := stripAmpersand(it.Label)
		hk := ""
		if it.Hotkey != "" {
			hk = "  " + hotkeyDisplay(it.Hotkey)
		}
		pad := strings.Repeat(" ", max(0, innerW-2-checkW-len(clean)-len(hk)))
		var inner string
		switch {
		case it.Disabled:
			inner = disabledStyle.Width(innerW).Render(" " + check + clean + pad + hk + " ")
		case i == o.dropCursor:
			inner = activeStyle.Render(" "+check) + renderLabel(it.Label, activeStyle, activeAccent) + activeStyle.Render(pad+hk+" ")
		default:
			inner = menuStyle.Render(" "+check) + renderLabel(it.Label, menuStyle, menuAccent) + menuStyle.Render(pad+hk+" ")
		}
		lines = append(lines, menuStyle.Render(layer.OV())+inner+menuStyle.Render(layer.OV()))
	}
	lines = append(lines, bottom)

	pos := ncBarMenuPositions(menus)
	anchorCol := 0
	if o.openMenu < len(pos) {
		anchorCol = pos[o.openMenu]
	}

	// innerW + 2 border chars = total rendered width.
	totalW := innerW + 2
	return overlayBox{
		content: strings.Join(lines, "\n"),
		col:     anchorCol,
		row:     ncBarRow + 1,
		width:   totalW,
		height:  len(lines),
	}
}

// ─── Dialog rendering ──────────────────────────────────────────────────────────

// renderDialog returns an overlayBox for PlaceOverlay, centered in the screen.
func (o *overlayState) renderDialog(screenW, screenH int, layer *theme.Layer) overlayBox {
	d := o.topDialog()
	if d == nil {
		return overlayBox{}
	}
	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}

	bodyLines := strings.Split(d.Body, "\n")
	maxBodyW := 0
	for _, l := range bodyLines {
		w := ansi.StringWidth(l)
		if w > maxBodyW {
			maxBodyW = w
		}
	}

	btnW := 0
	for _, b := range btns {
		btnW += len(b) + 4 // "[ b ]"
	}
	btnW += (len(btns) - 1) * 2 // gaps between buttons

	innerW := maxBodyW + 2
	if ansi.StringWidth(d.Title)+2 > innerW {
		innerW = ansi.StringWidth(d.Title) + 2
	}
	if btnW+2 > innerW {
		innerW = btnW + 2
	}
	if innerW < 22 {
		innerW = 22
	}

	boxStyle    := layer.BaseStyle()
	titleStyle  := layer.HighlightStyle()

	hbar := func(l, f, r string) string {
		return boxStyle.Render(l + strings.Repeat(f, innerW) + r)
	}
	lb := boxStyle.Render(layer.OV())
	rb := boxStyle.Render(layer.OV())

	var lines []string
	lines = append(lines, hbar(layer.OTL(), layer.OH(), layer.OTR()))

	// Title: full-width blue bar.
	titlePad := " " + d.Title + strings.Repeat(" ", max(0, innerW-1-ansi.StringWidth(d.Title)))
	lines = append(lines, lb+titleStyle.Width(innerW).Render(titlePad)+rb)

	lines = append(lines, hbar(layer.XL(), layer.IH(), layer.XR()))

	// Body rows.
	for _, bl := range bodyLines {
		if bl == "" {
			lines = append(lines, lb+boxStyle.Width(innerW).Render("")+rb)
		} else {
			lines = append(lines, lb+boxStyle.Width(innerW).Render(" "+bl)+rb)
		}
	}

	lines = append(lines, hbar(layer.XL(), layer.IH(), layer.XR()))

	// Button row.
	btnActiveSt := layer.ActiveStyle()
	var btnParts []string
	for i, b := range btns {
		label := "[ " + b + " ]"
		if i == o.dialogFocus {
			btnParts = append(btnParts, btnActiveSt.Render(label))
		} else {
			btnParts = append(btnParts, boxStyle.Render(label))
		}
	}
	// Join buttons with styled gap.
	var btnSB strings.Builder
	for i, p := range btnParts {
		if i > 0 {
			btnSB.WriteString(boxStyle.Render("  "))
		}
		btnSB.WriteString(p)
	}
	btnContent := btnSB.String()
	bw := lipgloss.Width(btnContent)
	lpad := (innerW - bw) / 2
	if lpad < 0 {
		lpad = 0
	}
	rpad := innerW - bw - lpad
	if rpad < 0 {
		rpad = 0
	}
	btnRow := boxStyle.Render(strings.Repeat(" ", lpad)) + btnContent + boxStyle.Render(strings.Repeat(" ", rpad))
	lines = append(lines, lb+btnRow+rb)

	lines = append(lines, hbar(layer.OBL(), layer.OH(), layer.OBR()))

	content := strings.Join(lines, "\n")
	totalW := innerW + 2
	totalH := len(lines)

	col := (screenW - totalW) / 2
	if col < 0 {
		col = 0
	}
	row := (screenH - totalH) / 2
	if row < 2 {
		row = 2
	}

	return overlayBox{
		content: content,
		col:     col,
		row:     row,
		width:   totalW,
		height:  totalH,
	}
}
