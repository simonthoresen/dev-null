package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
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
func (d *HDivider) Render(buf *common.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.FgC()
	bg := layer.BgC()
	ch := common.RuneOf(layer.IH())
	for col := x; col < x+width; col++ {
		buf.SetChar(col, y, ch, fg, bg, common.AttrNone)
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
func (d *VDivider) Render(buf *common.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.FgC()
	bg := layer.BgC()
	ch := common.RuneOf(layer.IV())
	for row := y; row < y+height; row++ {
		buf.SetChar(x, row, ch, fg, bg, common.AttrNone)
	}
}
