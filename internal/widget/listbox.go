package widget

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// MaxListVisible is the default max visible rows for a ListBox.
const MaxListVisible = 12

// ListBox is a scrollable, selectable list control.
type ListBox struct {
	Items    []string // display text for each item
	Tags     []string // optional right-aligned tags (parallel to Items)
	Cursor   int      // selected item index
	ScrollOff int     // scroll offset (0 = first item at top)
	Navigated bool    // true once the user has pressed a nav key or clicked an item

	height int // computed during Render

	wantTab     bool
	wantBackTab bool
}

func (lb *ListBox) Focusable() bool       { return len(lb.Items) > 0 }
func (lb *ListBox) TabWant() (bool, bool) { return lb.wantTab, lb.wantBackTab }

// OnFocusDir resets the list cursor to the top when focus arrives via Tab/Shift+Tab.
func (lb *ListBox) OnFocusDir(_ int) {
	lb.Cursor = 0
	lb.ScrollOff = 0
}
func (lb *ListBox) MinSize() (int, int) {
	w := 10
	for i, item := range lb.Items {
		iw := ansi.StringWidth(item) + 2 // "► " prefix (1 glyph + 1 space before tag)
		if i < len(lb.Tags) && lb.Tags[i] != "" {
			iw += 2 + ansi.StringWidth(lb.Tags[i])
		}
		if iw > w {
			w = iw
		}
	}
	h := len(lb.Items)
	if h > MaxListVisible {
		h = MaxListVisible
		w++ // scrollbar
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func (lb *ListBox) Update(msg tea.Msg) {
	lb.wantTab = false
	lb.wantBackTab = false
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return
	}
	n := len(lb.Items)
	if n == 0 {
		return
	}
	switch km.String() {
	case "up":
		lb.Navigated = true
		if lb.Cursor > 0 {
			lb.Cursor--
			lb.ensureVisible()
		}
	case "down":
		lb.Navigated = true
		if lb.Cursor < n-1 {
			lb.Cursor++
			lb.ensureVisible()
		}
	case "pgup":
		lb.Navigated = true
		lb.Cursor -= lb.visibleHeight()
		if lb.Cursor < 0 {
			lb.Cursor = 0
		}
		lb.ensureVisible()
	case "pgdown":
		lb.Navigated = true
		lb.Cursor += lb.visibleHeight()
		if lb.Cursor >= n {
			lb.Cursor = n - 1
		}
		lb.ensureVisible()
	case "home":
		lb.Navigated = true
		lb.Cursor = 0
		lb.ensureVisible()
	case "end":
		lb.Navigated = true
		lb.Cursor = n - 1
		lb.ensureVisible()
	case "tab":
		lb.wantTab = true
	case "shift+tab":
		lb.wantBackTab = true
	}
}

func (lb *ListBox) HandleClick(rx, ry int) {
	idx := lb.ScrollOff + ry
	if idx >= 0 && idx < len(lb.Items) {
		lb.Cursor = idx
		lb.Navigated = true
	}
}

func (lb *ListBox) visibleHeight() int {
	h := lb.height
	if h <= 0 {
		h = len(lb.Items)
		if h > MaxListVisible {
			h = MaxListVisible
		}
	}
	return max(1, h)
}

func (lb *ListBox) ensureVisible() {
	vis := lb.visibleHeight()
	if lb.Cursor < lb.ScrollOff {
		lb.ScrollOff = lb.Cursor
	}
	if lb.Cursor >= lb.ScrollOff+vis {
		lb.ScrollOff = lb.Cursor - vis + 1
	}
	if lb.ScrollOff < 0 {
		lb.ScrollOff = 0
	}
}

func (lb *ListBox) Render(buf *render.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	lb.height = height
	lb.ensureVisible()

	fg := layer.Fg
	bg := layer.Bg
	n := len(lb.Items)

	showScrollbar := n > height
	contentW := width
	if showScrollbar {
		contentW = max(1, width-1)
	}

	// Fill background.
	buf.Fill(x, y, width, height, ' ', fg, bg, render.AttrNone)

	for vi := 0; vi < height; vi++ {
		idx := lb.ScrollOff + vi
		if idx >= n {
			break
		}
		item := lb.Items[idx]
		tag := ""
		if idx < len(lb.Tags) {
			tag = lb.Tags[idx]
		}

		isCursor := idx == lb.Cursor
		// Single-char prefix: glyph in monochrome, space in color mode.
		// Color terminals show selection via background highlight alone.
		prefix := " "
		if isCursor && focused && layer.Monochrome {
			prefix = "►"
		} else if isCursor && !focused && layer.Monochrome {
			prefix = "›"
		}

		itemText := prefix + item
		if tag != "" {
			padNeeded := contentW - ansi.StringWidth(itemText) - ansi.StringWidth(tag) - 1
			if padNeeded < 1 {
				padNeeded = 1
			}
			itemText += strings.Repeat(" ", padNeeded) + tag
		}

		rowFg := fg
		rowBg := bg
		attr := render.PixelAttr(render.AttrNone)
		if isCursor && focused {
			rowFg = layer.HighlightFg
			rowBg = layer.HighlightBg
			attr = render.AttrBold
		}

		col := x
		for _, r := range itemText {
			if col >= x+contentW {
				break
			}
			buf.SetChar(col, y+vi, r, rowFg, rowBg, attr)
			col++
		}
		// Fill remaining.
		for col < x+contentW {
			buf.SetChar(col, y+vi, ' ', rowFg, rowBg, render.AttrNone)
			col++
		}
	}

	if showScrollbar {
		RenderScrollbarBuf(buf, x+contentW, y, n, height, lb.scrollOffsetForBar(), fg, bg)
	}
}

// scrollOffsetForBar converts top-based ScrollOff to the bottom-based offset
// that RenderScrollbarBuf expects (matching TextView convention).
func (lb *ListBox) scrollOffsetForBar() int {
	total := len(lb.Items)
	vis := lb.visibleHeight()
	maxOff := total - vis
	if maxOff <= 0 {
		return 0
	}
	return maxOff - lb.ScrollOff
}
