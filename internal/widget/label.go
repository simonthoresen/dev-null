package widget

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// Label is a static text control, 1 row.
type Label struct {
	Text  string
	Align string // "left" (default), "center", "right"
}

func (l *Label) Update(_ tea.Msg)    {}
func (l *Label) Focusable() bool     { return false }
func (l *Label) MinSize() (int, int) { return ansi.StringWidth(l.Text), 1 }
func (l *Label) Render(buf *render.ImageBuffer, x, y, w, h int, _ bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg
	text := l.Text
	vis := ansi.StringWidth(text)

	startX := x
	switch l.Align {
	case "center":
		if vis < w {
			startX = x + (w-vis)/2
		}
	case "right":
		if vis < w {
			startX = x + w - vis
		}
	}

	buf.PaintANSI(startX, y, w-(startX-x), 1, text, fg, bg)
}
