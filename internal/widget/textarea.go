package widget

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// TextArea is a multi-line editable text area with NC-style [·····] per line.
type TextArea struct {
	Lines      []string
	CursorRow  int
	CursorCol  int
	ScrollTop  int // first visible row
	height     int

	OnSubmit func(lines []string) // called on Ctrl+Enter

	WantTab     bool
	WantBackTab bool
}

func (a *TextArea) Focusable() bool            { return true }
func (a *TextArea) MinSize() (int, int)        { return 4, 1 }
func (a *TextArea) TabWant() (bool, bool)      { return a.WantTab, a.WantBackTab }

func (a *TextArea) Update(msg tea.Msg) {
	a.WantTab = false
	a.WantBackTab = false
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			a.WantTab = true
			return
		case "shift+tab":
			a.WantBackTab = true
			return
		case "up":
			if a.CursorRow > 0 {
				a.CursorRow--
			}
		case "down":
			if a.CursorRow < len(a.Lines)-1 {
				a.CursorRow++
			}
		case "left":
			if a.CursorCol > 0 {
				a.CursorCol--
			}
		case "right":
			if a.CursorRow < len(a.Lines) && a.CursorCol < len(a.Lines[a.CursorRow]) {
				a.CursorCol++
			}
		case "enter":
			// Split line at cursor.
			if a.CursorRow < len(a.Lines) {
				line := a.Lines[a.CursorRow]
				before := line[:min(a.CursorCol, len(line))]
				after := ""
				if a.CursorCol < len(line) {
					after = line[a.CursorCol:]
				}
				a.Lines[a.CursorRow] = before
				rest := append([]string{after}, a.Lines[a.CursorRow+1:]...)
				a.Lines = append(a.Lines[:a.CursorRow+1], rest...)
				a.CursorRow++
				a.CursorCol = 0
			}
		case "backspace":
			if a.CursorCol > 0 && a.CursorRow < len(a.Lines) {
				line := a.Lines[a.CursorRow]
				a.Lines[a.CursorRow] = line[:a.CursorCol-1] + line[a.CursorCol:]
				a.CursorCol--
			} else if a.CursorCol == 0 && a.CursorRow > 0 {
				// Merge with previous line.
				prev := a.Lines[a.CursorRow-1]
				a.CursorCol = len(prev)
				a.Lines[a.CursorRow-1] = prev + a.Lines[a.CursorRow]
				a.Lines = append(a.Lines[:a.CursorRow], a.Lines[a.CursorRow+1:]...)
				a.CursorRow--
			}
		default:
			// Type character.
			key := msg.String()
			if len(key) == 1 && key[0] >= 32 {
				if len(a.Lines) == 0 {
					a.Lines = []string{""}
				}
				line := a.Lines[a.CursorRow]
				if a.CursorCol > len(line) {
					a.CursorCol = len(line)
				}
				a.Lines[a.CursorRow] = line[:a.CursorCol] + key + line[a.CursorCol:]
				a.CursorCol++
			}
		}
	}
}

func (a *TextArea) Render(buf *render.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	a.height = height
	baseFg := layer.Fg
	baseBg := layer.Bg
	inputFg := layer.InputFg
	inputBg := layer.InputBg

	// Ensure at least one line.
	if len(a.Lines) == 0 {
		a.Lines = []string{""}
	}

	// Scroll to keep cursor visible.
	if a.CursorRow < a.ScrollTop {
		a.ScrollTop = a.CursorRow
	}
	if a.CursorRow >= a.ScrollTop+height {
		a.ScrollTop = a.CursorRow - height + 1
	}

	n := len(a.Lines)
	showScrollbar := n > height
	// -2 for "[" and "]"; -1 more when scrollbar occupies the last content col.
	fieldW := max(1, width-2)
	if showScrollbar {
		fieldW = max(1, fieldW-1)
	}

	for i := 0; i < height; i++ {
		lineIdx := a.ScrollTop + i
		row := y + i

		// Brackets.
		buf.SetChar(x, row, '[', baseFg, baseBg, render.AttrNone)
		buf.SetChar(x+width-1, row, ']', baseFg, baseBg, render.AttrNone)

		var lineContent string
		if lineIdx < len(a.Lines) {
			lineContent = a.Lines[lineIdx]
		}

		if lineContent != "" {
			col := 0
			for _, r := range lineContent {
				if col >= fieldW {
					break
				}
				buf.SetChar(x+1+col, row, r, inputFg, inputBg, render.AttrNone)
				col++
			}
			// Dots for remaining content columns.
			for col < fieldW {
				buf.SetChar(x+1+col, row, '·', inputFg, inputBg, render.AttrFaint)
				col++
			}
		} else {
			for col := 0; col < fieldW; col++ {
				buf.SetChar(x+1+col, row, '·', inputFg, inputBg, render.AttrFaint)
			}
		}
	}

	if showScrollbar {
		maxTop := n - height
		offset := maxTop - a.ScrollTop // convert top-based to bottom-based
		RenderScrollbarBuf(buf, x+1+fieldW, y, n, height, offset, baseFg, baseBg)
	}
}
