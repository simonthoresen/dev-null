package widget

import (
	"image/color"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
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
func (v *TextView) MinSize() (int, int) {
	// Always return 1×1. TextView fills whatever space the layout gives it;
	// returning content dimensions would force the containing column to be
	// as wide as the longest line, overflowing the window border.
	// Callers that need content-aware sizing (e.g. dialog body) must set
	// GridConstraint.MinW/MinH explicitly.
	return 1, 1
}

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

	// Always reserve 1 column for the scrollbar when Scrollable.
	contentW := width
	if v.Scrollable {
		contentW = max(1, width-1)
	}

	// Wrap lines to content width.
	var wrapped []string
	for _, line := range v.Lines {
		wrapped = append(wrapped, render.WrapANSI(line, contentW)...)
	}

	n := len(wrapped)

	// Clamp scroll to wrapped line count.
	maxOff := n - h
	if maxOff < 0 {
		maxOff = 0
	}
	if v.ScrollOffset > maxOff {
		v.ScrollOffset = maxOff
	}
	if v.ScrollOffset < 0 {
		v.ScrollOffset = 0
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
		visibleLines = wrapped[start:end]
	}

	// Render visible lines. Lines may contain ANSI codes (chat messages).
	startRow := y
	if v.BottomAlign && len(visibleLines) < h {
		startRow = y + h - len(visibleLines)
	}
	for i, line := range visibleLines {
		buf.PaintANSI(x, startRow+i, contentW, 1, line, fg, bg)
	}

	// Scrollbar — always shown when Scrollable; color changes with focus.
	if v.Scrollable {
		scrollFg := layer.DisabledFg
		if focused {
			scrollFg = layer.Fg
		}
		if n <= h {
			// Content fits: show a dim track with no thumb.
			for i := 0; i < h; i++ {
				buf.SetChar(x+contentW, y+i, '░', scrollFg, bg, render.AttrNone)
			}
		} else {
			RenderScrollbarBuf(buf, x+contentW, y, n, h, v.ScrollOffset, scrollFg, bg)
		}
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

