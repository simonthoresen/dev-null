package widget

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/render"
	"null-space/internal/theme"
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
	fg := layer.FgC()
	bg := layer.BgC()
	text := l.Text
	vis := utf8.RuneCountInString(text)

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

	col := startX
	for _, r := range text {
		if col >= x+w {
			break
		}
		if col >= x {
			buf.SetChar(col, y, r, fg, bg, render.AttrNone)
		}
		col++
	}
}
