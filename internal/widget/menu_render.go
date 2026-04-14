package widget

import (
	"strings"

	"charm.land/lipgloss/v2"

	"dev-null/internal/domain"
	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// ─── Overlay box ─────────────────────────────────────────────────────────────

// OverlayBox bundles an overlay's rendered content with its position and
// pre-computed dimensions so callers don't need to split the string.
type OverlayBox struct {
	Content       string
	Col, Row      int
	Width, Height int
}

// SubMenuBox is a rendered sub-menu buffer with its screen position.
type SubMenuBox struct {
	Buf      *render.ImageBuffer
	Col, Row int
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
			// ► only when bar is focused with no open dropdown AND the terminal
			// is monochrome; color terminals use background highlight alone.
			barCursor := o.OpenMenu < 0 && layer.Monochrome
			marker := " "
			if barCursor {
				marker = "►"
			}
			sb.WriteString(activeStyle.Render(marker))
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

// ─── Shared dropdown renderer ─────────────────────────────────────────────────

// renderMenuDropdown renders a list of MenuItemDefs into a bordered buffer.
// Handles: toggle checkmarks, shortcut underlines, hotkeys, "►" for sub-items,
// scroll offset, scrollbar, cursor highlight.
func renderMenuDropdown(
	items []domain.MenuItemDef, cursor, scrollOff, maxVisible int,
	layer *theme.Layer,
) *render.ImageBuffer {
	if len(items) == 0 {
		return nil
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

	// Compute inner width (widest item + padding).
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
	innerW := maxLW + checkW + 2 // +1 padding each side
	if innerW < 14 {
		innerW = 14
	}

	// Determine how many items to show and whether we need a scrollbar.
	totalItems := len(items)
	visibleCount := totalItems
	needScroll := false
	if maxVisible > 0 && totalItems > maxVisible {
		visibleCount = maxVisible
		needScroll = true
	}

	// Clamp scrollOff.
	if scrollOff > totalItems-visibleCount {
		scrollOff = totalItems - visibleCount
	}
	if scrollOff < 0 {
		scrollOff = 0
	}

	// Scrollbar takes 1 column from inner width.
	scrollW := 0
	if needScroll {
		scrollW = 1
	}
	totalW := innerW + 2 // +2 for border columns
	totalH := visibleCount + 2 // +2 for top/bottom border rows
	buf := render.NewImageBuffer(totalW, totalH)

	fg := layer.Fg
	bg := layer.Bg
	hlFg := layer.HighlightFg
	hlBg := layer.HighlightBg
	accentFg := layer.Accent
	disabledFg := layer.DisabledFg

	// Fill background.
	buf.Fill(0, 0, totalW, totalH, ' ', fg, bg, render.AttrNone)

	// Draw borders.
	buf.SetChar(0, 0, []rune(layer.OuterTL)[0], fg, bg, render.AttrNone)
	buf.SetChar(totalW-1, 0, []rune(layer.OuterTR)[0], fg, bg, render.AttrNone)
	buf.SetChar(0, totalH-1, []rune(layer.OuterBL)[0], fg, bg, render.AttrNone)
	buf.SetChar(totalW-1, totalH-1, []rune(layer.OuterBR)[0], fg, bg, render.AttrNone)
	hChar := []rune(layer.OuterH)[0]
	vChar := []rune(layer.OuterV)[0]
	for x := 1; x < totalW-1; x++ {
		buf.SetChar(x, 0, hChar, fg, bg, render.AttrNone)
		buf.SetChar(x, totalH-1, hChar, fg, bg, render.AttrNone)
	}
	for y := 1; y < totalH-1; y++ {
		buf.SetChar(0, y, vChar, fg, bg, render.AttrNone)
		buf.SetChar(totalW-1, y, vChar, fg, bg, render.AttrNone)
	}

	// Render visible items.
	sepChar := []rune(layer.InnerH)[0]
	for vi := 0; vi < visibleCount; vi++ {
		idx := scrollOff + vi
		if idx >= totalItems {
			break
		}
		it := items[idx]
		row := vi + 1 // +1 for top border

		if IsSeparator(it) {
			for x := 1; x < totalW-1-scrollW; x++ {
				buf.SetChar(x, row, sepChar, fg, bg, render.AttrNone)
			}
			continue
		}

		isCursor := idx == cursor
		rowFg := fg
		rowBg := bg
		rowAttr := render.PixelAttr(render.AttrNone)
		if isCursor {
			rowFg = hlFg
			rowBg = hlBg
			rowAttr = render.AttrBold
		}

		// Fill row background.
		for x := 1; x < totalW-1-scrollW; x++ {
			buf.SetChar(x, row, ' ', rowFg, rowBg, render.AttrNone)
		}

		col := 1 // start after left border

		// Cursor marker.
		if isCursor && layer.Monochrome {
			buf.SetChar(col, row, '►', rowFg, rowBg, rowAttr)
		}
		col++

		// Checkmark column.
		if hasToggles {
			if it.Toggle && it.Checked != nil && it.Checked() {
				buf.SetChar(col, row, '√', rowFg, rowBg, rowAttr)
			}
			col += checkW
		}

		// Label with shortcut underline.
		ampIdx := strings.IndexByte(it.Label, '&')
		clean, _ := StripAmpersand(it.Label)
		if it.Disabled {
			rowFg = disabledFg
			rowAttr = render.AttrNone
		}
		for ci, ch := range clean {
			if col >= totalW-1-scrollW {
				break
			}
			isShortcut := ampIdx >= 0 && ci == ampIdx
			cflag := rowAttr
			cfgc := rowFg
			if isShortcut && !it.Disabled {
				cfgc = accentFg
				cflag = render.AttrBold | render.AttrUnderline
			}
			buf.SetChar(col, row, ch, cfgc, rowBg, cflag)
			col++
		}

		// Right-aligned suffix: hotkey or sub-menu arrow.
		suffix := ""
		if HasSubMenu(it) {
			suffix = "►"
		} else if it.Hotkey != "" {
			suffix = HotkeyDisplay(it.Hotkey)
		}
		if suffix != "" {
			suffixStart := totalW - 1 - scrollW - 1 - len([]rune(suffix))
			sc := suffixStart
			sfg := rowFg
			if it.Disabled {
				sfg = disabledFg
			}
			for _, ch := range suffix {
				if sc >= 1 && sc < totalW-1-scrollW {
					buf.SetChar(sc, row, ch, sfg, rowBg, render.AttrNone)
				}
				sc++
			}
		}
	}

	// Scrollbar.
	if needScroll {
		scrollCol := totalW - 2 // inside right border
		scrollableItems := totalItems - visibleCount
		bottomOff := scrollableItems - scrollOff
		if bottomOff < 0 {
			bottomOff = 0
		}
		RenderScrollbarBuf(buf, scrollCol, 1, totalItems, visibleCount, bottomOff, fg, bg)
	}

	return buf
}

// ─── Dropdown rendering ────────────────────────────────────────────────────────

// RenderDropdownBuf renders the top-level dropdown into a buffer.
// Returns the buffer and its screen position, or (nil, 0, 0) if no dropdown is open.
func (o *OverlayState) RenderDropdownBuf(
	menus []domain.MenuDef, ncBarRow, screenW, screenH int, layer *theme.Layer,
) (*render.ImageBuffer, int, int) {
	if o.OpenMenu < 0 || o.OpenMenu >= len(menus) {
		return nil, 0, 0
	}
	items := menus[o.OpenMenu].Items
	if len(items) == 0 {
		return nil, 0, 0
	}

	maxVis := maxDropdownItems(screenH, ncBarRow+1)
	o.DropScrollOff = ensureMenuVisible(o.DropCursor, o.DropScrollOff, maxVis)

	ddBuf := renderMenuDropdown(items, o.DropCursor, o.DropScrollOff, maxVis, layer)
	if ddBuf == nil {
		return nil, 0, 0
	}

	pos := MenuBarPositions(menus)
	col := 0
	if o.OpenMenu < len(pos) {
		col = pos[o.OpenMenu]
	}
	// Clamp to screen width.
	if col+ddBuf.Width > screenW {
		col = max(0, screenW-ddBuf.Width)
	}
	return ddBuf, col, ncBarRow + 1
}

// RenderSubMenusBuf renders all open sub-menu levels into separate buffers.
// parentCol and parentW are the position/width of the top-level dropdown.
func (o *OverlayState) RenderSubMenusBuf(
	menus []domain.MenuDef, ncBarRow, parentCol, parentW, screenW, screenH int,
	layer *theme.Layer,
) []SubMenuBox {
	if len(o.SubMenus) == 0 || o.OpenMenu < 0 || o.OpenMenu >= len(menus) {
		return nil
	}

	prevCol := parentCol
	prevW := parentW
	prevRow := ncBarRow + 1 // top-level dropdown row
	prevScrollOff := o.DropScrollOff

	var result []SubMenuBox
	items := menus[o.OpenMenu].Items

	for i := range o.SubMenus {
		sm := &o.SubMenus[i]
		if sm.ParentIdx < 0 || sm.ParentIdx >= len(items) || !HasSubMenu(items[sm.ParentIdx]) {
			break
		}
		subItems := items[sm.ParentIdx].SubItems

		maxVis := maxDropdownItems(screenH, prevRow)
		sm.ScrollOff = ensureMenuVisible(sm.Cursor, sm.ScrollOff, maxVis)

		subBuf := renderMenuDropdown(subItems, sm.Cursor, sm.ScrollOff, maxVis, layer)
		if subBuf == nil {
			break
		}

		// Position: right of parent, aligned with the triggering item's row.
		col := prevCol + prevW
		row := prevRow + (sm.ParentIdx - prevScrollOff) + 1 // +1 for top border row of parent

		// Flip to left if would overflow right edge.
		if col+subBuf.Width > screenW {
			col = prevCol - subBuf.Width
			if col < 0 {
				col = 0
			}
		}
		// Shift up if would overflow bottom edge.
		if row+subBuf.Height > screenH {
			row = max(0, screenH-subBuf.Height)
		}

		result = append(result, SubMenuBox{Buf: subBuf, Col: col, Row: row})

		// For next level, this sub-menu is the parent.
		prevCol = col
		prevW = subBuf.Width
		prevRow = row
		prevScrollOff = sm.ScrollOff
		items = subItems
	}
	return result
}

// ─── Legacy dropdown rendering (ANSI string) ─────────────────────────────────

// RenderDropdown returns an OverlayBox for PlaceOverlay (legacy callers).
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
			if HasSubMenu(it) {
				w += 2 // " ►"
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

		// Right-aligned suffix: sub-menu arrow or hotkey.
		suffix := ""
		if HasSubMenu(it) {
			suffix = " ►"
		} else if it.Hotkey != "" {
			suffix = "  " + HotkeyDisplay(it.Hotkey)
		}
		pad := strings.Repeat(" ", max(0, innerW-2-checkW-len(clean)-len(suffix)))
		var inner string
		switch {
		case it.Disabled:
			inner = disabledStyle.Width(innerW).Render(" " + check + clean + pad + suffix + " ")
		case i == o.DropCursor:
			dropMarker := " "
			if layer.Monochrome {
				dropMarker = "►"
			}
			inner = activeStyle.Render(dropMarker+check) + RenderLabel(it.Label, activeStyle, activeAccent) + activeStyle.Render(pad+suffix+" ")
		default:
			inner = menuStyle.Render(" "+check) + RenderLabel(it.Label, menuStyle, menuAccent) + menuStyle.Render(pad+suffix+" ")
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

// Dialog rendering is handled by RenderDialogBuf in dialog.go using NC Windows.
