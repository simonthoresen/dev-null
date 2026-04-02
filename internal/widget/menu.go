package widget

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/domain"
	"null-space/internal/theme"
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

	Dialogs     []domain.DialogRequest
	DialogFocus int // focused button in top dialog
}

// ShowDialogMsg is sent to a player's Bubble Tea program to push a dialog.
type ShowDialogMsg struct{ Dialog domain.DialogRequest }

func (o *OverlayState) HasDialog() bool { return len(o.Dialogs) > 0 }

func (o *OverlayState) TopDialog() *domain.DialogRequest {
	if len(o.Dialogs) == 0 {
		return nil
	}
	return &o.Dialogs[len(o.Dialogs)-1]
}

func (o *OverlayState) PushDialog(d domain.DialogRequest) {
	o.Dialogs = append(o.Dialogs, d)
	o.DialogFocus = 0
}

func (o *OverlayState) PopDialog() {
	if len(o.Dialogs) > 0 {
		o.Dialogs = o.Dialogs[:len(o.Dialogs)-1]
		o.DialogFocus = 0
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
				o.OpenMenu = -1
				o.MenuFocused = false
				it.Handler(playerID)
				return true
			}
		}
	}

	if o.HasDialog() {
		return o.HandleDialogKey(key)
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

func (o *OverlayState) HandleDialogKey(key string) bool {
	d := o.TopDialog()
	if d == nil {
		return false
	}
	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}
	switch key {
	case "tab":
		o.DialogFocus = (o.DialogFocus + 1) % len(btns)
	case "left":
		if o.DialogFocus > 0 {
			o.DialogFocus--
		}
	case "right":
		if o.DialogFocus < len(btns)-1 {
			o.DialogFocus++
		}
	case "enter", " ":
		label := btns[o.DialogFocus]
		cb := d.OnClose
		o.PopDialog()
		if cb != nil {
			cb(label)
		}
	case "esc":
		cb := d.OnClose
		o.PopDialog()
		if cb != nil {
			cb("")
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

func (o *OverlayState) HandleDialogClick(x, y, screenW, screenH int) bool {
	d := o.TopDialog()
	if d == nil {
		return false
	}
	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}

	// Recalculate dialog dimensions (mirrors RenderDialog logic)
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

	// Must match RenderDialog's calculation exactly (no shadow offset).
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
				o.PopDialog()
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

// ─── Overlay box ─────────────────────────────────────────────────────────────

// OverlayBox bundles an overlay's rendered content with its position and
// pre-computed dimensions so callers don't need to split the string.
type OverlayBox struct {
	Content       string
	Col, Row      int
	Width, Height int
}

// ─── Menu bar rendering ────────────────────────────────────────────────────────

// RenderMenuBar renders the NC-style action bar row (full terminal width).
func (o *OverlayState) RenderMenuBar(width int, menus []domain.MenuDef, layer *theme.Layer) string {
	barStyle     := layer.BaseStyle()
	activeStyle  := layer.HighlightStyle()
	barAccent    := layer.AccentStyle()
	activeAccent := lipgloss.NewStyle().Background(layer.HighlightBgC()).Foreground(layer.AccentC()).Bold(true).Underline(true)

	var sb strings.Builder
	for i, m := range menus {
		if i > 0 {
			sb.WriteString(barStyle.Render(layer.Sep()))
		}
		focused := (o.MenuFocused || o.OpenMenu >= 0) && i == o.MenuCursor
		if focused {
			sb.WriteString(activeStyle.Render(" "))
			sb.WriteString(RenderLabel(m.Label, activeStyle, activeAccent))
			sb.WriteString(activeStyle.Render(" "))
		} else {
			sb.WriteString(barStyle.Render(" "))
			sb.WriteString(RenderLabel(m.Label, barStyle, barAccent))
			sb.WriteString(barStyle.Render(" "))
		}
	}
	content := sb.String()
	cw := lipgloss.Width(content)
	if cw < width {
		content += barStyle.Width(width - cw).Render("")
	}
	return TruncateStyled(content, width)
}

// MenuBarPositions returns the starting x column of each menu title in the bar.
func MenuBarPositions(menus []domain.MenuDef) []int {
	pos := make([]int, len(menus))
	x := 0
	for i, m := range menus {
		pos[i] = x
		clean, _ := StripAmpersand(m.Label)
		x += 1 + len(clean) + 1 // " label " = 2 + len
		if i < len(menus)-1 {
			x++ // separator
		}
	}
	return pos
}

// ─── Dropdown rendering ────────────────────────────────────────────────────────

// RenderDropdown returns an OverlayBox for PlaceOverlay.
// ncBarRow is the screen row (0-based) of the NC action bar.
func (o *OverlayState) RenderDropdown(menus []domain.MenuDef, ncBarRow int, layer *theme.Layer) OverlayBox {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) {
		return OverlayBox{}
	}
	items := menus[o.OpenMenu].Items
	if len(items) == 0 {
		return OverlayBox{}
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
		if !IsSeparator(it) {
			clean, _ := StripAmpersand(it.Label)
			w := len(clean)
			if it.Hotkey != "" {
				w += 2 + len(HotkeyDisplay(it.Hotkey))
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
		if IsSeparator(it) {
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

		clean, _ := StripAmpersand(it.Label)
		hk := ""
		if it.Hotkey != "" {
			hk = "  " + HotkeyDisplay(it.Hotkey)
		}
		pad := strings.Repeat(" ", max(0, innerW-2-checkW-len(clean)-len(hk)))
		var inner string
		switch {
		case it.Disabled:
			inner = disabledStyle.Width(innerW).Render(" " + check + clean + pad + hk + " ")
		case i == o.DropCursor:
			inner = activeStyle.Render(" "+check) + RenderLabel(it.Label, activeStyle, activeAccent) + activeStyle.Render(pad+hk+" ")
		default:
			inner = menuStyle.Render(" "+check) + RenderLabel(it.Label, menuStyle, menuAccent) + menuStyle.Render(pad+hk+" ")
		}
		lines = append(lines, menuStyle.Render(layer.OV())+inner+menuStyle.Render(layer.OV()))
	}
	lines = append(lines, bottom)

	pos := MenuBarPositions(menus)
	anchorCol := 0
	if o.OpenMenu < len(pos) {
		anchorCol = pos[o.OpenMenu]
	}

	// innerW + 2 border chars = total rendered width.
	totalW := innerW + 2
	return OverlayBox{
		Content: strings.Join(lines, "\n"),
		Col:     anchorCol,
		Row:     ncBarRow + 1,
		Width:   totalW,
		Height:  len(lines),
	}
}

// ─── Dialog rendering ──────────────────────────────────────────────────────────

// RenderDialog returns an OverlayBox for PlaceOverlay, centered in the screen.
func (o *OverlayState) RenderDialog(screenW, screenH int, layer *theme.Layer) OverlayBox {
	d := o.TopDialog()
	if d == nil {
		return OverlayBox{}
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
		if i == o.DialogFocus {
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

	contentStr := strings.Join(lines, "\n")
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

	return OverlayBox{
		Content: contentStr,
		Col:     col,
		Row:     row,
		Width:   totalW,
		Height:  totalH,
	}
}
