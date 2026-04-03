package widget

import (
	"image/color"

	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// TextView is a read-only, scrollable, multi-line text area.
type TextView struct {
	Lines        []string
	BottomAlign  bool
	Scrollable   bool
	ScrollOffset int
	height       int

	// WantTab/WantBackTab are set by Update when tab should cycle focus.
	WantTab     bool
	WantBackTab bool
}

func (v *TextView) TabWant() (bool, bool) { return v.WantTab, v.WantBackTab }

func (v *TextView) Focusable() bool     { return v.Scrollable }
func (v *TextView) MinSize() (int, int) { return 1, 1 }

// SetHeight sets the internal height (used for scroll clamping in tests).
func (v *TextView) SetHeight(h int) { v.height = h }

// ClampScroll clamps ScrollOffset to valid range.
func (v *TextView) ClampScroll() { v.clampScroll() }

func (v *TextView) Update(msg tea.Msg) {
	v.WantTab = false
	v.WantBackTab = false
	if !v.Scrollable {
		return
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			v.WantTab = true
			return
		case "shift+tab":
			v.WantBackTab = true
			return
		case "pgup":
			v.ScrollOffset += v.height
			v.clampScroll()
		case "pgdown":
			v.ScrollOffset -= v.height
			v.clampScroll()
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			v.ScrollOffset += 3
			v.clampScroll()
		case tea.MouseWheelDown:
			v.ScrollOffset -= 3
			v.clampScroll()
		}
	}
}

func (v *TextView) clampScroll() {
	maxOff := len(v.Lines) - v.height
	if maxOff < 0 {
		maxOff = 0
	}
	if v.ScrollOffset > maxOff {
		v.ScrollOffset = maxOff
	}
	if v.ScrollOffset < 0 {
		v.ScrollOffset = 0
	}
}

func (v *TextView) Render(buf *render.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg
	v.height = height
	h := max(1, height)
	v.clampScroll()

	n := len(v.Lines)
	contentW := width
	showScrollbar := v.Scrollable && n > h
	if showScrollbar {
		contentW = max(1, width-1)
	}

	// Fill background.
	buf.Fill(x, y, width, height, ' ', fg, bg, render.AttrNone)

	// Determine visible slice.
	var visibleLines []string
	if n > 0 {
		end := n - v.ScrollOffset
		if end < 0 {
			end = 0
		}
		start := end - h
		if start < 0 {
			start = 0
		}
		visibleLines = v.Lines[start:end]
	}

	// Render visible lines. Lines may contain ANSI codes (chat messages).
	startRow := y
	if v.BottomAlign && len(visibleLines) < h {
		startRow = y + h - len(visibleLines)
	}
	for i, line := range visibleLines {
		buf.PaintANSI(x, startRow+i, contentW, 1, line, fg, bg)
	}

	// Scrollbar.
	if showScrollbar {
		RenderScrollbarBuf(buf, x+contentW, y, n, h, v.ScrollOffset, fg, bg)
	}
}

// RenderScrollbarBuf writes a scrollbar track directly into the buffer.
func RenderScrollbarBuf(buf *render.ImageBuffer, x, y, total, visible, offset int, fg, bg color.Color) {
	if visible <= 0 {
		return
	}
	if total <= visible {
		for i := 0; i < visible; i++ {
			buf.SetChar(x, y+i, ' ', fg, bg, render.AttrNone)
		}
		return
	}
	thumbSize := max(1, visible*visible/total)
	scrollRange := total - visible
	topOffset := scrollRange - offset
	if topOffset < 0 {
		topOffset = 0
	}
	thumbPos := 0
	if scrollRange > 0 {
		thumbPos = topOffset * (visible - thumbSize) / scrollRange
	}
	for i := 0; i < visible; i++ {
		ch := '░'
		if i >= thumbPos && i < thumbPos+thumbSize {
			ch = '█'
		}
		buf.SetChar(x, y+i, ch, fg, bg, render.AttrNone)
	}
}

// RenderScrollbar returns styled string slices for a scrollbar (legacy).
func RenderScrollbar(total, visible, offset int, style interface{ Render(strs ...string) string }) []string {
	if visible <= 0 {
		return nil
	}
	track := make([]string, visible)
	if total <= visible {
		for i := range track {
			track[i] = style.Render(" ")
		}
		return track
	}
	thumbSize := max(1, visible*visible/total)
	scrollRange := total - visible
	topOffset := scrollRange - offset
	if topOffset < 0 {
		topOffset = 0
	}
	thumbPos := 0
	if scrollRange > 0 {
		thumbPos = topOffset * (visible - thumbSize) / scrollRange
	}
	for i := range track {
		if i >= thumbPos && i < thumbPos+thumbSize {
			track[i] = style.Render("█")
		} else {
			track[i] = style.Render("░")
		}
	}
	return track
}
