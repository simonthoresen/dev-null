package server

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ─── Core interfaces ──────────────────────────────────────────────────────────

// NCControl is the base interface for all NC widgets.
type NCControl interface {
	// Render returns the styled content for this control at the given size.
	Render(width, height int, pal *Palette, t *Theme) string
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

// Render draws the window at (x, y) with the given dimensions.
func (w *NCWindow) Render(x, y, width, height int, pal *Palette, t *Theme) string {
	w.screenX = x
	w.screenY = y
	w.width = width
	w.height = height
	w.innerW = max(1, width-2)
	w.innerH = max(1, height-2) // top border + bottom border

	boxStyle := pal.BaseStyle()
	titleStyle := pal.HighlightStyle()
	lv := boxStyle.Render(t.OV())
	rv := boxStyle.Render(t.OV())

	// Top border / title row.
	var topRow string
	if w.Title != "" {
		titleText := " " + w.Title + " "
		titleRendered := titleStyle.Render(titleText)
		titleFill := max(0, w.innerW-1-ansi.StringWidth(titleText))
		topRow = boxStyle.Render(t.OTL()+t.IH()) + titleRendered + boxStyle.Render(strings.Repeat(t.OH(), titleFill)+t.OTR())
	} else {
		topRow = boxStyle.Render(t.OTL() + strings.Repeat(t.OH(), w.innerW) + t.OTR())
	}

	// Compute grid layout.
	w.computeGrid(pal, t)

	// Render each row of the inner area.
	innerRows := make([]string, w.innerH)
	for i := range innerRows {
		innerRows[i] = boxStyle.Width(w.innerW).Render("")
	}

	// Render each child into its cell position.
	for i, child := range w.Children {
		cx, cy, cw, ch := w.childRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		focused := i == w.FocusIdx
		_ = focused
		content := child.Control.Render(cw, ch, pal, t)
		contentLines := strings.Split(content, "\n")
		for j, line := range contentLines {
			ry := cy - (y + 1) + j // relative to inner area
			if ry >= 0 && ry < w.innerH {
				rx := cx - (x + 1) // relative to inner area
				innerRows[ry] = overlayAt(innerRows[ry], rx, line, boxStyle)
			}
		}
	}

	// Assemble rows with borders.
	var rows []string
	rows = append(rows, topRow)
	for _, ir := range innerRows {
		rows = append(rows, lv+ir+rv)
	}
	rows = append(rows, boxStyle.Render(t.OBL()+strings.Repeat(t.OH(), w.innerW)+t.OBR()))

	// Render connected dividers (HDivider, VDivider) with proper junctions.
	// TODO: junction rendering for connected dividers

	return strings.Join(rows, "\n")
}

// computeGrid determines column widths and row heights.
func (w *NCWindow) computeGrid(pal *Palette, t *Theme) {
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

// overlayAt places overlay text at column offset rx within a row string.
func overlayAt(row string, rx int, overlay string, baseStyle lipgloss.Style) string {
	rowW := ansi.StringWidth(row)
	overW := ansi.StringWidth(overlay)
	if rx < 0 || rx >= rowW {
		return row
	}

	left := ansi.Truncate(row, rx, "")
	leftW := lipgloss.Width(left)
	if leftW < rx {
		left += baseStyle.Render(strings.Repeat(" ", rx-leftW))
	}

	right := ""
	end := rx + overW
	if end < rowW {
		right = ansiSkipColumns(row, end)
	}

	return left + overlay + right
}

// ─── Focus management ─────────────────────────────────────────────────────────

// HandleUpdate routes a tea.Msg to the focused child control.
func (w *NCWindow) HandleUpdate(msg tea.Msg) {
	if w.FocusIdx >= 0 && w.FocusIdx < len(w.Children) {
		if w.Children[w.FocusIdx].Control.Focusable() {
			w.Children[w.FocusIdx].Control.Update(msg)
		}
	}
}

// CycleFocus advances focus to the next focusable control.
func (w *NCWindow) CycleFocus() {
	if len(w.Children) == 0 {
		return
	}
	start := w.FocusIdx
	for i := 1; i <= len(w.Children); i++ {
		idx := (start + i) % len(w.Children)
		if w.Children[idx].Control.Focusable() {
			w.FocusIdx = idx
			return
		}
	}
}

// FocusFirst sets focus to the first focusable control.
func (w *NCWindow) FocusFirst() {
	for i, child := range w.Children {
		if child.Control.Focusable() {
			w.FocusIdx = i
			return
		}
	}
	w.FocusIdx = -1
}

// CursorPosition returns the absolute cursor position if a text input has focus.
func (w *NCWindow) CursorPosition() (cx, cy int, visible bool) {
	if w.FocusIdx < 0 || w.FocusIdx >= len(w.Children) {
		return 0, 0, false
	}
	c := w.Children[w.FocusIdx].Control
	// Check for NCTextInput.
	if ti, ok := c.(*NCTextInput); ok && ti.Model != nil {
		cursor := ti.Model.Cursor()
		if cursor == nil {
			return 0, 0, false
		}
		rx, ry, _, _ := w.childRect(w.FocusIdx)
		// +1 for the "[" bracket
		cx = rx + 1 + cursor.Position.X
		cy = ry
		return cx, cy, true
	}
	return 0, 0, false
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
