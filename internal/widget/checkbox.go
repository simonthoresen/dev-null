package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// Checkbox is a toggleable [x] Label control.
type Checkbox struct {
	Label    string
	Checked  bool
	OnToggle func(checked bool)

	WantTab     bool
	WantBackTab bool
}

func (cb *Checkbox) Focusable() bool      { return true }
func (cb *Checkbox) MinSize() (int, int)  { return 4 + len(cb.Label), 1 } // "[x] " + label
func (cb *Checkbox) TabWant() (bool, bool) { return cb.WantTab, cb.WantBackTab }
func (cb *Checkbox) Update(msg tea.Msg) {
	cb.WantTab = false
	cb.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter", " ":
			cb.Checked = !cb.Checked
			if cb.OnToggle != nil {
				cb.OnToggle(cb.Checked)
			}
		case "tab":
			cb.WantTab = true
		case "shift+tab":
			cb.WantBackTab = true
		}
	}
}
func (cb *Checkbox) Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	fg := layer.FgC()
	bg := layer.BgC()
	attr := common.PixelAttr(common.AttrNone)
	if focused {
		fg = layer.HighlightFgC()
		bg = layer.HighlightBgC()
		attr = common.AttrBold
	}
	mark := ' '
	if cb.Checked {
		mark = 'x'
	}
	text := "[" + string(mark) + "] " + cb.Label
	col := x
	for _, r := range text {
		if col >= x+width {
			break
		}
		buf.SetChar(col, y, r, fg, bg, attr)
		col++
	}
}
