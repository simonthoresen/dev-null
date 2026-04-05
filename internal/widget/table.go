package widget

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// Table renders a table from row/column data with vertical scrolling when the
// content overflows the allocated height.
type Table struct {
	Rows      [][]string
	ScrollTop int // first visible row index (top-based)
}

func (t *Table) Focusable() bool     { return len(t.Rows) > 0 }
func (t *Table) MinSize() (int, int) { return 1, 1 }

func (t *Table) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			if t.ScrollTop > 0 {
				t.ScrollTop--
			}
		case "down":
			t.ScrollTop++ // clamped in Render
		}
	}
}

func (t *Table) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg

	if len(t.Rows) == 0 {
		return
	}

	n := len(t.Rows)

	// Clamp scroll position.
	maxTop := max(0, n-height)
	if t.ScrollTop > maxTop {
		t.ScrollTop = maxTop
	}
	if t.ScrollTop < 0 {
		t.ScrollTop = 0
	}

	showScrollbar := n > height
	contentW := width
	if showScrollbar {
		contentW = max(1, width-1)
	}

	// Calculate column widths (across all rows, not just visible ones).
	numCols := 0
	for _, row := range t.Rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	colWidths := make([]int, numCols)
	for _, row := range t.Rows {
		for c, cell := range row {
			w := ansi.StringWidth(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	row := y
	for ri := t.ScrollTop; ri < n; ri++ {
		if row >= y+height {
			break
		}
		dataRow := t.Rows[ri]
		col := x
		for c := 0; c < numCols; c++ {
			cell := ""
			if c < len(dataRow) {
				cell = dataRow[c]
			}
			cellEnd := col + colWidths[c]
			// Write cell content using ANSI-aware painting, clipped to contentW.
			availW := x + contentW - col
			if availW > colWidths[c] {
				availW = colWidths[c]
			}
			if availW > 0 {
				buf.PaintANSI(col, row, availW, 1, cell, fg, bg)
			}
			// Pad remaining space in the column.
			cellVis := ansi.StringWidth(cell)
			for i := cellVis; i < colWidths[c]; i++ {
				if col+i >= x+contentW {
					break
				}
				buf.SetChar(col+i, row, ' ', fg, bg, render.AttrNone)
			}
			col = cellEnd
			if c < numCols-1 {
				if col < x+contentW {
					buf.SetChar(col, row, ' ', fg, bg, render.AttrNone)
				}
				col++
			}
		}
		row++
	}

	if showScrollbar {
		offset := maxTop - t.ScrollTop // convert top-based to bottom-based
		RenderScrollbarBuf(buf, x+contentW, y, n, height, offset, fg, bg)
	}
}
