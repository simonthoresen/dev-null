package server

import (
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ─── Core interfaces ──────────────────────────────────────────────────────────

// NCControl is the base interface for all NC widgets.
type NCControl interface {
	// Render writes the control's content into buf at position (x, y)
	// within the given (width × height) region. focused is true when this
	// control currently has keyboard focus.
	Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer)
	// Update handles a tea.Msg. Only called when this control has focus.
	Update(msg tea.Msg)
	// MinSize returns the minimum (width, height) this control needs.
	// A dimension of 0 means "no minimum" (flex in that direction).
	MinSize() (int, int)
	// Focusable returns true if this control can receive keyboard focus.
	Focusable() bool
}

// Fill controls how a control expands within its grid cell.
type Fill int

const (
	FillNone       Fill = iota // don't expand
	FillHorizontal             // expand width to fill cell
	FillVertical               // expand height to fill cell
	FillBoth                   // expand both directions
)

// GridChild pairs a control with its layout constraint.
type GridChild struct {
	Control    NCControl
	Constraint GridConstraint
	TabIndex   int // focus order — lower values receive focus first (default 0)
}

// GridConstraint positions a control in the grid layout.
type GridConstraint struct {
	Col, Row         int     // grid position (0-based)
	ColSpan, RowSpan int     // cells spanned (default 1)
	WeightX, WeightY float64 // share of extra space (0 = fixed, >0 = flex)
	Fill             Fill    // how to fill the allocated cell
	MinW, MinH       int     // override minimum size (0 = use control's MinSize)
}

func (c GridConstraint) colSpan() int {
	if c.ColSpan <= 0 {
		return 1
	}
	return c.ColSpan
}
func (c GridConstraint) rowSpan() int {
	if c.RowSpan <= 0 {
		return 1
	}
	return c.RowSpan
}

// ─── NCWindow ─────────────────────────────────────────────────────────────────

// NCWindow is a top-level bordered container. It gets a palette based on depth
// (or explicitly for warning dialogs). It renders its border, title, shadow,
// and lays out children in a grid.
type NCWindow struct {
	Title    string
	Children []GridChild
	FocusIdx int // index into Children; -1 = none

	// Computed during Render.
	screenX, screenY int
	width, height    int
	innerW, innerH   int
	cellX, cellY     []int // absolute X/Y of each grid cell
	cellW, cellH     []int // width/height of each grid cell
	gridCols, gridRows int
}

// Render draws the window at (x, y) with the given dimensions into buf.
// Returns the string representation for backward compatibility with callers
// that have not yet been migrated to use ImageBuffer directly.
func (w *NCWindow) Render(x, y, width, height int, layer *ThemeLayer) string {
	buf := NewImageBuffer(width, height)
	w.RenderToBuf(buf, x, y, width, height, layer)
	return buf.ToString()
}

// RenderToBuf draws the window into the given buffer at absolute position (x, y).
func (w *NCWindow) RenderToBuf(buf *ImageBuffer, x, y, width, height int, layer *ThemeLayer) {
	w.screenX = x
	w.screenY = y
	w.width = width
	w.height = height
	w.innerW = max(1, width-2)
	w.innerH = max(1, height-2) // top border + bottom border

	fg := layer.FgC()
	bg := layer.BgC()
	hlFg := layer.HighlightFgC()
	hlBg := layer.HighlightBgC()

	// Fill inner area with base bg.
	buf.Fill(x, y, width, height, ' ', fg, bg, AttrNone)

	// Top border row.
	buf.SetChar(x, y, runeOf(layer.OTL()), fg, bg, AttrNone)
	buf.SetChar(x+width-1, y, runeOf(layer.OTR()), fg, bg, AttrNone)
	if w.Title != "" {
		titleText := " " + w.Title + " "
		buf.SetChar(x+1, y, runeOf(layer.IH()), fg, bg, AttrNone)
		n := buf.WriteString(x+2, y, titleText, hlFg, hlBg, AttrBold)
		for col := x + 2 + n; col < x+width-1; col++ {
			buf.SetChar(col, y, runeOf(layer.OH()), fg, bg, AttrNone)
		}
	} else {
		for col := x + 1; col < x+width-1; col++ {
			buf.SetChar(col, y, runeOf(layer.OH()), fg, bg, AttrNone)
		}
	}

	// Bottom border row.
	boty := y + height - 1
	buf.SetChar(x, boty, runeOf(layer.OBL()), fg, bg, AttrNone)
	buf.SetChar(x+width-1, boty, runeOf(layer.OBR()), fg, bg, AttrNone)
	for col := x + 1; col < x+width-1; col++ {
		buf.SetChar(col, boty, runeOf(layer.OH()), fg, bg, AttrNone)
	}

	// Left and right border columns.
	vr := runeOf(layer.OV())
	for row := y + 1; row < boty; row++ {
		buf.SetChar(x, row, vr, fg, bg, AttrNone)
		buf.SetChar(x+width-1, row, vr, fg, bg, AttrNone)
	}

	// Compute grid layout.
	w.computeGrid()

	// Render each child directly into the buffer.
	for i, child := range w.Children {
		cx, cy, cw, ch := w.childRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		hasFocus := i == w.FocusIdx
		child.Control.Render(buf, cx, cy, cw, ch, hasFocus, layer)
	}
}

// runeOf returns the first rune from a string, or ' ' if empty.
func runeOf(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || r == 0 {
		return ' '
	}
	return r
}

// computeGrid determines column widths and row heights.
func (w *NCWindow) computeGrid() {
	// Determine grid dimensions.
	maxCol, maxRow := 0, 0
	for _, child := range w.Children {
		endCol := child.Constraint.Col + child.Constraint.colSpan()
		endRow := child.Constraint.Row + child.Constraint.rowSpan()
		if endCol > maxCol {
			maxCol = endCol
		}
		if endRow > maxRow {
			maxRow = endRow
		}
	}
	w.gridCols = maxCol
	w.gridRows = maxRow
	if maxCol == 0 || maxRow == 0 {
		return
	}

	// Collect minimum sizes and weights per column/row.
	colMinW := make([]int, maxCol)
	rowMinH := make([]int, maxRow)
	colWeight := make([]float64, maxCol)
	rowWeight := make([]float64, maxRow)

	for _, child := range w.Children {
		c := child.Constraint
		minW, minH := child.Control.MinSize()
		if c.MinW > 0 {
			minW = c.MinW
		}
		if c.MinH > 0 {
			minH = c.MinH
		}
		// For single-span cells, contribute to column/row minimums.
		if c.colSpan() == 1 && minW > colMinW[c.Col] {
			colMinW[c.Col] = minW
		}
		if c.rowSpan() == 1 && minH > rowMinH[c.Row] {
			rowMinH[c.Row] = minH
		}
		if c.WeightX > colWeight[c.Col] {
			colWeight[c.Col] = c.WeightX
		}
		if c.WeightY > rowWeight[c.Row] {
			rowWeight[c.Row] = c.WeightY
		}
	}

	// Distribute extra space by weight.
	w.cellW = distributeSpace(colMinW, colWeight, w.innerW)
	w.cellH = distributeSpace(rowMinH, rowWeight, w.innerH)

	// Compute absolute positions.
	w.cellX = make([]int, maxCol)
	w.cellY = make([]int, maxRow)
	cx := w.screenX + 1 // +1 for left border
	for i := range w.cellX {
		w.cellX[i] = cx
		cx += w.cellW[i]
	}
	cy := w.screenY + 1 // +1 for top border
	for i := range w.cellY {
		w.cellY[i] = cy
		cy += w.cellH[i]
	}
}

// childRect returns the absolute (x, y, w, h) for child at index i.
func (w *NCWindow) childRect(i int) (int, int, int, int) {
	c := w.Children[i].Constraint
	if c.Col >= w.gridCols || c.Row >= w.gridRows {
		return 0, 0, 0, 0
	}
	x := w.cellX[c.Col]
	y := w.cellY[c.Row]
	cw := 0
	for j := c.Col; j < c.Col+c.colSpan() && j < w.gridCols; j++ {
		cw += w.cellW[j]
	}
	ch := 0
	for j := c.Row; j < c.Row+c.rowSpan() && j < w.gridRows; j++ {
		ch += w.cellH[j]
	}
	return x, y, cw, ch
}

// distributeSpace allocates space to cells based on minimums and weights.
func distributeSpace(mins []int, weights []float64, total int) []int {
	n := len(mins)
	sizes := make([]int, n)
	copy(sizes, mins)

	used := 0
	for _, s := range sizes {
		used += s
	}
	extra := total - used
	if extra <= 0 {
		return sizes
	}

	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}
	if totalWeight == 0 {
		// No weights — give all extra to the last cell.
		sizes[n-1] += extra
		return sizes
	}

	// Distribute proportionally by weight.
	distributed := 0
	for i, w := range weights {
		if w > 0 {
			share := int(float64(extra) * w / totalWeight)
			sizes[i] += share
			distributed += share
		}
	}
	// Give remainder to the first weighted cell.
	remainder := extra - distributed
	for i, w := range weights {
		if w > 0 && remainder > 0 {
			sizes[i] += remainder
			break
		}
		_ = w
	}
	return sizes
}


// ─── Focus management ─────────────────────────────────────────────────────────

// TabWanter is implemented by controls that signal tab/shift-tab to the window.
type TabWanter interface {
	TabWant() (wantTab, wantBackTab bool)
}

// HandleUpdate routes a tea.Msg to the focused child control.
// If the control signals WantTab/WantBackTab, cycles focus forward/backward.
// Returns a tea.Cmd when focus changes to a text input (for cursor blink).
func (w *NCWindow) HandleUpdate(msg tea.Msg) tea.Cmd {
	if w.FocusIdx < 0 || w.FocusIdx >= len(w.Children) {
		return nil
	}
	c := w.Children[w.FocusIdx].Control
	if !c.Focusable() {
		return nil
	}
	c.Update(msg)

	if tw, ok := c.(TabWanter); ok {
		fwd, back := tw.TabWant()
		if fwd {
			return w.CycleFocus()
		} else if back {
			return w.CycleFocusBack()
		}
	}
	return nil
}

// focusOrder returns child indices sorted by TabIndex (stable, preserving
// declaration order within the same TabIndex).
func (w *NCWindow) focusOrder() []int {
	n := len(w.Children)
	order := make([]int, 0, n)
	for i := range w.Children {
		if w.Children[i].Control.Focusable() {
			order = append(order, i)
		}
	}
	// Stable sort by TabIndex.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && w.Children[order[j]].TabIndex < w.Children[order[j-1]].TabIndex; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order
}

// CycleFocus advances focus to the next focusable control by TabIndex order.
func (w *NCWindow) CycleFocus() tea.Cmd {
	return w.cycleFocusDir(+1)
}

// CycleFocusBack moves focus to the previous focusable control by TabIndex order.
func (w *NCWindow) CycleFocusBack() tea.Cmd {
	return w.cycleFocusDir(-1)
}

func (w *NCWindow) cycleFocusDir(dir int) tea.Cmd {
	order := w.focusOrder()
	if len(order) == 0 {
		return nil
	}
	oldIdx := w.FocusIdx
	// Find current position in tab order.
	cur := -1
	for i, idx := range order {
		if idx == w.FocusIdx {
			cur = i
			break
		}
	}
	if cur < 0 {
		// Current focus not in order — jump to first.
		w.FocusIdx = order[0]
	} else {
		next := (cur + dir + len(order)) % len(order)
		w.FocusIdx = order[next]
	}
	if oldIdx != w.FocusIdx {
		w.blurTextInput(oldIdx)
		return w.activateTextInput(w.FocusIdx)
	}
	return nil
}

// activateTextInput calls Focus() on a textinput.Model if the child at idx is
// an NCTextInput or NCCommandInput. Returns the tea.Cmd from Focus() for cursor blink.
func (w *NCWindow) activateTextInput(idx int) tea.Cmd {
	if idx < 0 || idx >= len(w.Children) {
		return nil
	}
	switch ti := w.Children[idx].Control.(type) {
	case *NCTextInput:
		return ti.Model.Focus()
	case *NCCommandInput:
		return ti.Model.Focus()
	}
	return nil
}

// blurTextInput calls Blur() on a textinput.Model if the child at idx is
// an NCTextInput or NCCommandInput.
func (w *NCWindow) blurTextInput(idx int) {
	if idx < 0 || idx >= len(w.Children) {
		return
	}
	switch ti := w.Children[idx].Control.(type) {
	case *NCTextInput:
		ti.Model.Blur()
	case *NCCommandInput:
		ti.Model.Blur()
	}
}

// FocusFirst sets focus to the focusable control with the lowest TabIndex.
// Returns a tea.Cmd if the focused control is a text input (for cursor blink).
func (w *NCWindow) FocusFirst() tea.Cmd {
	order := w.focusOrder()
	if len(order) == 0 {
		w.FocusIdx = -1
		return nil
	}
	w.FocusIdx = order[0]
	return w.activateTextInput(w.FocusIdx)
}

// CursorPosition returns the absolute cursor position if a text input has focus.
func (w *NCWindow) CursorPosition() (cx, cy int, visible bool) {
	if w.FocusIdx < 0 || w.FocusIdx >= len(w.Children) {
		return 0, 0, false
	}
	c := w.Children[w.FocusIdx].Control

	// Extract the textinput.Model from either NCTextInput or NCCommandInput.
	var model *textinput.Model
	switch ti := c.(type) {
	case *NCTextInput:
		model = ti.Model
	case *NCCommandInput:
		model = ti.Model
	}
	if model == nil {
		return 0, 0, false
	}
	cursor := model.Cursor()
	if cursor == nil {
		return 0, 0, false
	}
	rx, ry, _, _ := w.childRect(w.FocusIdx)
	cx = rx + 1 + cursor.Position.X // +1 for "[" bracket
	cy = ry
	return cx, cy, true
}

// HandleClick routes a mouse click to the correct child.
func (w *NCWindow) HandleClick(mx, my int) bool {
	if mx < w.screenX || mx >= w.screenX+w.width {
		return false
	}
	if my < w.screenY || my >= w.screenY+w.height {
		return false
	}
	for i, child := range w.Children {
		if !child.Control.Focusable() {
			continue
		}
		cx, cy, cw, ch := w.childRect(i)
		if mx >= cx && mx < cx+cw && my >= cy && my < cy+ch {
			w.FocusIdx = i
			return true
		}
	}
	return true
}
