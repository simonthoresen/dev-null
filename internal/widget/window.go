package widget

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// Window is a top-level bordered container. It gets a palette based on depth
// (or explicitly for warning dialogs). It renders its border, title, shadow,
// and lays out children in a grid.
type Window struct {
	Title       string
	Children    []GridChild
	FocusIdx    int  // index into Children; -1 = none
	// Computed during Render.
	screenX, screenY int
	width, height    int
	innerW, innerH   int
	cellX, cellY     []int // absolute X/Y of each grid cell
	cellW, cellH     []int // width/height of each grid cell
	gridCols, gridRows int

	// Reusable scratch slices for computeGrid (avoids per-frame allocations).
	scratchColMinW  []int
	scratchRowMinH  []int
	scratchColWeight []float64
	scratchRowWeight []float64
}

// RenderToBuf draws the window into the given buffer at absolute position (x, y).
func (w *Window) RenderToBuf(buf *render.ImageBuffer, x, y, width, height int, layer *theme.Layer) {
	w.screenX = x
	w.screenY = y
	w.width = width
	w.height = height
	w.innerW = max(1, width-2)
	w.innerH = max(1, height-2) // top border + bottom border

	fg := layer.Fg
	bg := layer.Bg
	hlFg := layer.HighlightFg
	hlBg := layer.HighlightBg

	// Fill inner area with base bg.
	buf.Fill(x, y, width, height, ' ', fg, bg, render.AttrNone)

	// Top border row.
	buf.SetChar(x, y, render.RuneOf(layer.OuterTL), fg, bg, render.AttrNone)
	buf.SetChar(x+width-1, y, render.RuneOf(layer.OuterTR), fg, bg, render.AttrNone)
	if w.Title != "" {
		titleText := " " + w.Title + " "
		buf.SetChar(x+1, y, render.RuneOf(layer.OuterH), fg, bg, render.AttrNone)
		n := buf.WriteString(x+2, y, titleText, hlFg, hlBg, render.AttrBold)
		for col := x + 2 + n; col < x+width-1; col++ {
			buf.SetChar(col, y, render.RuneOf(layer.OuterH), fg, bg, render.AttrNone)
		}
	} else {
		for col := x + 1; col < x+width-1; col++ {
			buf.SetChar(col, y, render.RuneOf(layer.OuterH), fg, bg, render.AttrNone)
		}
	}

	// Bottom border row.
	boty := y + height - 1
	buf.SetChar(x, boty, render.RuneOf(layer.OuterBL), fg, bg, render.AttrNone)
	buf.SetChar(x+width-1, boty, render.RuneOf(layer.OuterBR), fg, bg, render.AttrNone)
	for col := x + 1; col < x+width-1; col++ {
		buf.SetChar(col, boty, render.RuneOf(layer.OuterH), fg, bg, render.AttrNone)
	}

	// Left and right border columns.
	vr := render.RuneOf(layer.OuterV)
	startRow := y + 1
	for row := startRow; row < boty; row++ {
		buf.SetChar(x, row, vr, fg, bg, render.AttrNone)
		buf.SetChar(x+width-1, row, vr, fg, bg, render.AttrNone)
	}

	// Compute grid layout.
	w.computeGrid()

	// Render each child directly into the buffer.
	for i, child := range w.Children {
		cx, cy, cw, ch := w.ChildRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		hasFocus := i == w.FocusIdx
		child.Control.Render(buf, cx, cy, cw, ch, hasFocus, layer)
	}

	// Post-process connected dividers — draw junction characters at border intersections.
	for i, child := range w.Children {
		cx, cy, cw, ch := w.ChildRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		switch child.Control.(type) {
		case *HDivider:
			if child.Control.(*HDivider).Connected {
				// Left junction: outer border or inner VDivider?
				if cx == x+1 {
					buf.SetChar(x, cy, render.RuneOf(layer.CrossL), fg, bg, render.AttrNone)
				} else {
					// Upgrade to CrossX if a complementary InnerCrossR is already there.
					ch := render.RuneOf(layer.InnerCrossL)
					if buf.CharAt(cx-1, cy) == render.RuneOf(layer.InnerCrossR) {
						ch = render.RuneOf(layer.CrossX)
					}
					buf.SetChar(cx-1, cy, ch, fg, bg, render.AttrNone)
				}
				// Right junction: outer border or inner VDivider?
				if cx+cw == x+width-1 {
					buf.SetChar(x+width-1, cy, render.RuneOf(layer.CrossR), fg, bg, render.AttrNone)
				} else {
					// Upgrade to CrossX if a complementary InnerCrossL is already there.
					ch := render.RuneOf(layer.InnerCrossR)
					if buf.CharAt(cx+cw, cy) == render.RuneOf(layer.InnerCrossL) {
						ch = render.RuneOf(layer.CrossX)
					}
					buf.SetChar(cx+cw, cy, ch, fg, bg, render.AttrNone)
				}
			}
		case *VDivider:
			if child.Control.(*VDivider).Connected {
				// Top junction: outer border or inner HDivider?
				if cy == y+1 {
					buf.SetChar(cx, y, render.RuneOf(layer.CrossT), fg, bg, render.AttrNone)
				} else {
					// Upgrade to CrossX if a complementary InnerCrossB is already there.
					ch := render.RuneOf(layer.InnerCrossT)
					if buf.CharAt(cx, cy-1) == render.RuneOf(layer.InnerCrossB) {
						ch = render.RuneOf(layer.CrossX)
					}
					buf.SetChar(cx, cy-1, ch, fg, bg, render.AttrNone)
				}
				// Bottom junction: outer border or inner HDivider?
				if cy+ch >= boty {
					buf.SetChar(cx, boty, render.RuneOf(layer.CrossB), fg, bg, render.AttrNone)
				} else {
					// Upgrade to CrossX if a complementary InnerCrossT is already there.
					jch := render.RuneOf(layer.InnerCrossB)
					if buf.CharAt(cx, cy+ch) == render.RuneOf(layer.InnerCrossT) {
						jch = render.RuneOf(layer.CrossX)
					}
					buf.SetChar(cx, cy+ch, jch, fg, bg, render.AttrNone)
				}
			}
		}
	}
}


// computeGrid determines column widths and row heights.
func (w *Window) computeGrid() {
	// Determine grid dimensions.
	maxCol, maxRow := 0, 0
	for _, child := range w.Children {
		endCol := child.Constraint.Col + child.Constraint.ColSpanVal()
		endRow := child.Constraint.Row + child.Constraint.RowSpanVal()
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

	// Reuse scratch slices for minimum sizes and weights.
	w.scratchColMinW = reuseIntSlice(w.scratchColMinW, maxCol)
	w.scratchRowMinH = reuseIntSlice(w.scratchRowMinH, maxRow)
	w.scratchColWeight = reuseFloatSlice(w.scratchColWeight, maxCol)
	w.scratchRowWeight = reuseFloatSlice(w.scratchRowWeight, maxRow)
	colMinW := w.scratchColMinW
	rowMinH := w.scratchRowMinH
	colWeight := w.scratchColWeight
	rowWeight := w.scratchRowWeight

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
		if c.ColSpanVal() == 1 && minW > colMinW[c.Col] {
			colMinW[c.Col] = minW
		}
		if c.RowSpanVal() == 1 && minH > rowMinH[c.Row] {
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
	w.cellW = DistributeSpaceInto(w.cellW, colMinW, colWeight, w.innerW)
	w.cellH = DistributeSpaceInto(w.cellH, rowMinH, rowWeight, w.innerH)

	// Compute absolute positions.
	w.cellX = reuseIntSlice(w.cellX, maxCol)
	w.cellY = reuseIntSlice(w.cellY, maxRow)
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

// ChildRect returns the absolute (x, y, w, h) for child at index i.
func (w *Window) ChildRect(i int) (int, int, int, int) {
	c := w.Children[i].Constraint
	if c.Col >= w.gridCols || c.Row >= w.gridRows {
		return 0, 0, 0, 0
	}
	x := w.cellX[c.Col]
	y := w.cellY[c.Row]
	cw := 0
	for j := c.Col; j < c.Col+c.ColSpanVal() && j < w.gridCols; j++ {
		cw += w.cellW[j]
	}
	ch := 0
	for j := c.Row; j < c.Row+c.RowSpanVal() && j < w.gridRows; j++ {
		ch += w.cellH[j]
	}
	return x, y, cw, ch
}

// ─── Focus management ─────────────────────────────────────────────────────────

// HandleUpdate routes a tea.Msg to the focused child control.
// If the control signals WantTab/WantBackTab, cycles focus forward/backward.
// Returns a tea.Cmd when focus changes to a text input (for cursor blink).
func (w *Window) HandleUpdate(msg tea.Msg) tea.Cmd {
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
func (w *Window) focusOrder() []int {
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
func (w *Window) CycleFocus() tea.Cmd {
	return w.cycleFocusDir(+1)
}

// CycleFocusBack moves focus to the previous focusable control by TabIndex order.
func (w *Window) CycleFocusBack() tea.Cmd {
	return w.cycleFocusDir(-1)
}

func (w *Window) cycleFocusDir(dir int) tea.Cmd {
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
	wrapped := false
	if cur < 0 {
		// Current focus not in order — jump to first.
		w.FocusIdx = order[0]
	} else {
		linear := cur + dir
		next := (linear + len(order)) % len(order)
		wrapped = linear < 0 || linear >= len(order)
		w.FocusIdx = order[next]
	}
	if oldIdx != w.FocusIdx {
		w.blurTextInput(oldIdx)
		if fdr, ok := w.Children[w.FocusIdx].Control.(FocusDirReceiver); ok {
			fdr.OnFocusDir(dir)
		}
		return w.activateTextInput(w.FocusIdx)
	}
	// Even when focus stays on the same control (single focusable child),
	// a wrap occurred — notify the control so it can reset internal state.
	if wrapped {
		if fdr, ok := w.Children[w.FocusIdx].Control.(FocusDirReceiver); ok {
			fdr.OnFocusDir(dir)
		}
	}
	return nil
}

// activateTextInput calls Focus() on a textinput.Model if the child at idx is
// a TextInput or CommandInput. Returns the tea.Cmd from Focus() for cursor blink.
func (w *Window) activateTextInput(idx int) tea.Cmd {
	if idx < 0 || idx >= len(w.Children) {
		return nil
	}
	switch ti := w.Children[idx].Control.(type) {
	case *TextInput:
		return ti.Model.Focus()
	case *CommandInput:
		return ti.Model.Focus()
	}
	return nil
}

// blurTextInput calls Blur() on a textinput.Model if the child at idx is
// a TextInput or CommandInput.
func (w *Window) blurTextInput(idx int) {
	if idx < 0 || idx >= len(w.Children) {
		return
	}
	switch ti := w.Children[idx].Control.(type) {
	case *TextInput:
		ti.Model.Blur()
	case *CommandInput:
		ti.Model.Blur()
	}
}

// FocusFirst sets focus to the focusable control with the lowest TabIndex.
// Returns a tea.Cmd if the focused control is a text input (for cursor blink).
func (w *Window) FocusFirst() tea.Cmd {
	order := w.focusOrder()
	if len(order) == 0 {
		w.FocusIdx = -1
		return nil
	}
	w.FocusIdx = order[0]
	if fdr, ok := w.Children[w.FocusIdx].Control.(FocusDirReceiver); ok {
		fdr.OnFocusDir(+1)
	}
	return w.activateTextInput(w.FocusIdx)
}

// CursorPosition returns the absolute cursor position if a text input has focus.
func (w *Window) CursorPosition() (cx, cy int, visible bool) {
	if w.FocusIdx < 0 || w.FocusIdx >= len(w.Children) {
		return 0, 0, false
	}
	c := w.Children[w.FocusIdx].Control

	// Extract the textinput.Model from either TextInput or CommandInput.
	var model *textinput.Model
	switch ti := c.(type) {
	case *TextInput:
		model = ti.Model
	case *CommandInput:
		model = ti.Model
	}
	if model == nil {
		return 0, 0, false
	}
	cursor := model.Cursor()
	if cursor == nil {
		return 0, 0, false
	}
	rx, ry, _, _ := w.ChildRect(w.FocusIdx)
	cx = rx + 1 + cursor.Position.X // +1 for "[" bracket
	cy = ry
	return cx, cy, true
}

// FocusedControl returns the control that currently has focus, or nil.
func (w *Window) FocusedControl() Control {
	if w.FocusIdx < 0 || w.FocusIdx >= len(w.Children) {
		return nil
	}
	return w.Children[w.FocusIdx].Control
}

// HandleClick routes a mouse click to the correct child.
// If the child implements Clickable, it receives the click with relative coords.
func (w *Window) HandleClick(mx, my int) bool {
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
		cx, cy, cw, ch := w.ChildRect(i)
		if mx >= cx && mx < cx+cw && my >= cy && my < cy+ch {
			w.FocusIdx = i
			if cl, ok := child.Control.(Clickable); ok {
				cl.HandleClick(mx-cx, my-cy)
			}
			return true
		}
	}
	return true
}
