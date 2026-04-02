package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// Panel is a bordered sub-container within a window. It implements Control
// and can contain its own grid of children. It inherits the palette from its
// parent window.
type Panel struct {
	Title    string
	Children []GridChild
	FocusIdx int

	screenX, screenY int
	innerW, innerH   int
}

func (p *Panel) Focusable() bool     { return false } // panels aren't directly focusable; their children are
func (p *Panel) MinSize() (int, int) { return 4, 3 }  // min border box
func (p *Panel) Update(msg tea.Msg)  {}               // updates go to children directly

func (p *Panel) Render(buf *common.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
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

	// Render children stacked vertically in the inner area.
	cy := y + 1
	for _, child := range p.Children {
		_, minH := child.Control.MinSize()
		ch := minH
		if child.Constraint.WeightY > 0 {
			ch = max(minH, (y+1+p.innerH)-cy)
		}
		if cy+ch > y+1+p.innerH {
			ch = y + 1 + p.innerH - cy
		}
		if ch <= 0 {
			continue
		}
		child.Control.Render(buf, x+1, cy, p.innerW, ch, false, layer)
		cy += ch
	}
}
