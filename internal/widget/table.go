package widget

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// Table renders a table from row/column data.
type Table struct {
	Rows [][]string
}

func (t *Table) Update(_ tea.Msg)    {}
func (t *Table) Focusable() bool     { return false }
func (t *Table) MinSize() (int, int) { return 1, len(t.Rows) }

func (t *Table) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg

	if len(t.Rows) == 0 {
		return
	}

	// Calculate column widths.
	numCols := 0
	for _, row := range t.Rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	colWidths := make([]int, numCols)
	for _, row := range t.Rows {
		for c, cell := range row {
			w := utf8.RuneCountInString(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	row := y
	for _, dataRow := range t.Rows {
		if row >= y+height {
			break
		}
		col := x
		for c := 0; c < numCols; c++ {
			cell := ""
			if c < len(dataRow) {
				cell = dataRow[c]
			}
			n := buf.WriteString(col, row, cell, fg, bg, render.AttrNone)
			// Pad to column width.
			for i := n; i < colWidths[c]; i++ {
				buf.SetChar(col+i, row, ' ', fg, bg, render.AttrNone)
			}
			col += colWidths[c]
			if c < numCols-1 {
				buf.SetChar(col, row, ' ', fg, bg, render.AttrNone)
				col++
			}
		}
		row++
	}
}
