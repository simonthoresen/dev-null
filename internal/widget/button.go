package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// Button is a clickable button: [ Label ].
type Button struct {
	Label   string
	OnPress func()

	WantTab     bool
	WantBackTab bool
}

func (b *Button) Focusable() bool      { return true }
func (b *Button) MinSize() (int, int)  { return len(b.Label) + 4, 1 } // "[ " + label + " ]"
func (b *Button) TabWant() (bool, bool) { return b.WantTab, b.WantBackTab }
func (b *Button) Update(msg tea.Msg) {
	b.WantTab = false
	b.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter", " ":
			if b.OnPress != nil {
				b.OnPress()
			}
		case "tab":
			b.WantTab = true
		case "shift+tab":
			b.WantBackTab = true
		}
	}
}
func (b *Button) Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	fg := layer.FgC()
	bg := layer.BgC()
	attr := common.PixelAttr(common.AttrNone)
	if focused {
		fg = layer.HighlightFgC()
		bg = layer.HighlightBgC()
		attr = common.AttrBold
	}
	label := "[ " + b.Label + " ]"
	col := x
	for _, r := range label {
		if col >= x+width {
			break
		}
		buf.SetChar(col, y, r, fg, bg, attr)
		col++
	}
}
