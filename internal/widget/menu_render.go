package widget

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/domain"
	"null-space/internal/theme"
)

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
	activeAccent := lipgloss.NewStyle().Background(layer.HighlightBg).Foreground(layer.Accent).Bold(true).Underline(true)

	var sb strings.Builder
	for i, m := range menus {
		if i > 0 {
			sb.WriteString(barStyle.Render(layer.BarSep))
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

	top    := menuStyle.Render(layer.OuterTL + strings.Repeat(layer.OuterH, innerW) + layer.OuterTR)
	bottom := menuStyle.Render(layer.OuterBL + strings.Repeat(layer.OuterH, innerW) + layer.OuterBR)
	// Menu separators don't connect to the outer border (unlike panel dividers).
	sepRow := menuStyle.Render(layer.OuterV + strings.Repeat(layer.InnerH, innerW) + layer.OuterV)

	var lines []string
	lines = append(lines, top)

	menuAccent  := layer.AccentStyle()
	activeAccent := lipgloss.NewStyle().Background(layer.HighlightBg).Foreground(layer.Accent).Bold(true).Underline(true)

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
		lines = append(lines, menuStyle.Render(layer.OuterV)+inner+menuStyle.Render(layer.OuterV))
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
	lb := boxStyle.Render(layer.OuterV)
	rb := boxStyle.Render(layer.OuterV)

	var lines []string
	lines = append(lines, hbar(layer.OuterTL, layer.OuterH, layer.OuterTR))

	// Title: full-width blue bar.
	titlePad := " " + d.Title + strings.Repeat(" ", max(0, innerW-1-ansi.StringWidth(d.Title)))
	lines = append(lines, lb+titleStyle.Width(innerW).Render(titlePad)+rb)

	lines = append(lines, hbar(layer.CrossL, layer.InnerH, layer.CrossR))

	// Body rows.
	for _, bl := range bodyLines {
		if bl == "" {
			lines = append(lines, lb+boxStyle.Width(innerW).Render("")+rb)
		} else {
			lines = append(lines, lb+boxStyle.Width(innerW).Render(" "+bl)+rb)
		}
	}

	lines = append(lines, hbar(layer.CrossL, layer.InnerH, layer.CrossR))

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

	lines = append(lines, hbar(layer.OuterBL, layer.OuterH, layer.OuterBR))

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
