package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// Panel is a bordered sub-container within a window. It implements Control
// and can contain its own grid of children. Uses the same grid-bag layout
// algorithm as Window for positioning children.
type Panel struct {
	Title    string
	Children []GridChild
	FocusIdx int

	screenX, screenY int
	innerW, innerH   int
	// Computed grid layout (mirrors Window).
	cellX, cellY       []int
	cellW, cellH       []int
	gridCols, gridRows int
}

func (p *Panel) Focusable() bool     { return false } // panels aren't directly focusable; their children are
func (p *Panel) MinSize() (int, int) { return 4, 3 }  // min border box
func (p *Panel) Update(msg tea.Msg)  {}               // updates go to children directly

func (p *Panel) Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	p.innerW = max(1, width-2)
	p.innerH = max(1, height-2)
	p.screenX = x
	p.screenY = y

	fg := layer.FgC()
	bg := layer.BgC()
	hlFg := layer.HighlightFgC()
	hlBg := layer.HighlightBgC()

	// Fill with background.
	buf.Fill(x, y, width, height, ' ', fg, bg, common.AttrNone)

	// Top border with optional title (same pattern as Window).
	buf.SetChar(x, y, common.RuneOf(layer.OTL()), fg, bg, common.AttrNone)
	buf.SetChar(x+width-1, y, common.RuneOf(layer.OTR()), fg, bg, common.AttrNone)
	if p.Title != "" {
		titleText := " " + p.Title + " "
		buf.SetChar(x+1, y, common.RuneOf(layer.IH()), fg, bg, common.AttrNone)
		n := buf.WriteString(x+2, y, titleText, hlFg, hlBg, common.AttrBold)
		for col := x + 2 + n; col < x+width-1; col++ {
			buf.SetChar(col, y, common.RuneOf(layer.OH()), fg, bg, common.AttrNone)
		}
	} else {
		for col := x + 1; col < x+width-1; col++ {
			buf.SetChar(col, y, common.RuneOf(layer.OH()), fg, bg, common.AttrNone)
		}
	}

	// Bottom border.
	boty := y + height - 1
	buf.SetChar(x, boty, common.RuneOf(layer.OBL()), fg, bg, common.AttrNone)
	buf.SetChar(x+width-1, boty, common.RuneOf(layer.OBR()), fg, bg, common.AttrNone)
	for col := x + 1; col < x+width-1; col++ {
		buf.SetChar(col, boty, common.RuneOf(layer.OH()), fg, bg, common.AttrNone)
	}

	// Left/right borders.
	vr := common.RuneOf(layer.OV())
	for row := y + 1; row < boty; row++ {
		buf.SetChar(x, row, vr, fg, bg, common.AttrNone)
		buf.SetChar(x+width-1, row, vr, fg, bg, common.AttrNone)
	}

	// Compute grid layout and render children (same algorithm as Window).
	p.computeGrid(x+1, y+1)
	for i, child := range p.Children {
		cx, cy, cw, ch := p.childRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		hasFocus := focused && i == p.FocusIdx
		child.Control.Render(buf, cx, cy, cw, ch, hasFocus, layer)
	}

	// Post-process connected dividers.
	for i, child := range p.Children {
		cx, cy, cw, ch := p.childRect(i)
		if cw <= 0 || ch <= 0 {
			continue
		}
		switch child.Control.(type) {
		case *HDivider:
			if child.Control.(*HDivider).Connected {
				buf.SetChar(x, cy, common.RuneOf(layer.XL()), fg, bg, common.AttrNone)
				buf.SetChar(x+width-1, cy, common.RuneOf(layer.XR()), fg, bg, common.AttrNone)
			}
		case *VDivider:
			if child.Control.(*VDivider).Connected {
				buf.SetChar(cx, y, common.RuneOf(layer.XT()), fg, bg, common.AttrNone)
				buf.SetChar(cx, boty, common.RuneOf(layer.XB()), fg, bg, common.AttrNone)
			}
		}
	}
}

func (p *Panel) computeGrid(innerX, innerY int) {
	maxCol, maxRow := 0, 0
	for _, child := range p.Children {
		endCol := child.Constraint.Col + child.Constraint.ColSpanVal()
		endRow := child.Constraint.Row + child.Constraint.RowSpanVal()
		if endCol > maxCol {
			maxCol = endCol
		}
		if endRow > maxRow {
			maxRow = endRow
		}
	}
	p.gridCols = maxCol
	p.gridRows = maxRow
	if maxCol == 0 || maxRow == 0 {
		return
	}

	colMinW := make([]int, maxCol)
	rowMinH := make([]int, maxRow)
	colWeight := make([]float64, maxCol)
	rowWeight := make([]float64, maxRow)

	for _, child := range p.Children {
		c := child.Constraint
		minW, minH := child.Control.MinSize()
		if c.MinW > 0 {
			minW = c.MinW
		}
		if c.MinH > 0 {
			minH = c.MinH
		}
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

	p.cellW = DistributeSpace(colMinW, colWeight, p.innerW)
	p.cellH = DistributeSpace(rowMinH, rowWeight, p.innerH)

	p.cellX = make([]int, maxCol)
	p.cellY = make([]int, maxRow)
	cx := innerX
	for i := range p.cellX {
		p.cellX[i] = cx
		cx += p.cellW[i]
	}
	cy := innerY
	for i := range p.cellY {
		p.cellY[i] = cy
		cy += p.cellH[i]
	}
}

func (p *Panel) childRect(i int) (int, int, int, int) {
	c := p.Children[i].Constraint
	if c.Col >= p.gridCols || c.Row >= p.gridRows {
		return 0, 0, 0, 0
	}
	x := p.cellX[c.Col]
	y := p.cellY[c.Row]
	cw := 0
	for j := c.Col; j < c.Col+c.ColSpanVal() && j < p.gridCols; j++ {
		cw += p.cellW[j]
	}
	ch := 0
	for j := c.Row; j < c.Row+c.RowSpanVal() && j < p.gridRows; j++ {
		ch += p.cellH[j]
	}
	return x, y, cw, ch
}
