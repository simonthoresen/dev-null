package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// HDivider is a horizontal divider line.
// Connected=true means junctions connect to the parent's outer frame (╟──╢).
// Connected=false means the divider floats inside (║──║).
type HDivider struct {
	Connected bool
}

func (d *HDivider) Update(_ tea.Msg)     {}
func (d *HDivider) Focusable() bool      { return false }
func (d *HDivider) MinSize() (int, int)  { return 1, 1 }
func (d *HDivider) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg
	ch := render.RuneOf(layer.InnerH)
	for col := x; col < x+width; col++ {
		buf.SetChar(col, y, ch, fg, bg, render.AttrNone)
	}
}

// VDivider is a vertical divider line.
// Connected=true means junctions connect to the parent's outer frame (╤ at top, ╧ at bottom).
// Connected=false means the divider floats inside.
type VDivider struct {
	Connected bool
}

func (d *VDivider) Update(_ tea.Msg)     {}
func (d *VDivider) Focusable() bool      { return false }
func (d *VDivider) MinSize() (int, int)  { return 1, 1 }
func (d *VDivider) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg
	ch := render.RuneOf(layer.InnerV)
	for row := y; row < y+height; row++ {
		buf.SetChar(x, row, ch, fg, bg, render.AttrNone)
	}
}
