package server

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ─── NC Control interface ─────────────────────────────────────────────────────

// NCControl is a widget that can be placed inside an NCPanel.
type NCControl interface {
	// Render returns the styled content for this control.
	// width is the inner content width (excluding panel borders).
	// focused indicates whether this control has keyboard focus.
	// base is the panel's resolved background/foreground style.
	Render(width int, focused bool, base lipgloss.Style) string

	// Height returns how many rows this control occupies.
	// available is the remaining height for flex controls (like NCTextView).
	// Fixed-height controls ignore it and return their fixed size.
	Height(available int) int

	// Focusable returns true if this control can receive keyboard focus.
	Focusable() bool

	// Flex returns true if this control expands to fill available space.
	Flex() bool
}

// ─── NCTextView ───────────────────────────────────────────────────────────────

// NCTextView is a read-only multi-line text area. It is a flex control that
// fills available vertical space. Lines are bottom-aligned (most recent at
// the bottom, empty space at the top).
type NCTextView struct {
	Lines       []string
	BottomAlign bool
	height      int // set by panel layout
}

func (v *NCTextView) Focusable() bool     { return false }
func (v *NCTextView) Flex() bool           { return true }
func (v *NCTextView) Height(avail int) int { v.height = avail; return avail }

func (v *NCTextView) Render(width int, focused bool, base lipgloss.Style) string {
	style := base
	h := v.height
	if h < 1 {
		h = 1
	}

	n := len(v.Lines)
	var rows []string
	if v.BottomAlign && n < h {
		// Pad with empty lines at the top.
		for i := 0; i < h-n; i++ {
			rows = append(rows, style.Width(width).Render(""))
		}
		for _, line := range v.Lines {
			rows = append(rows, style.Width(width).Render(truncateStyled(line, width)))
		}
	} else {
		// Show the last h lines.
		start := 0
		if n > h {
			start = n - h
		}
		for _, line := range v.Lines[start:] {
			rows = append(rows, style.Width(width).Render(truncateStyled(line, width)))
		}
		// Pad to fill height.
		for len(rows) < h {
			rows = append(rows, style.Width(width).Render(""))
		}
	}
	return strings.Join(rows, "\n")
}

// ─── NCTextInput ──────────────────────────────────────────────────────────────

// NCTextInput wraps a textinput.Model as a 1-row focusable control.
type NCTextInput struct {
	Model *textinput.Model
}

func (ti *NCTextInput) Focusable() bool     { return true }
func (ti *NCTextInput) Flex() bool           { return false }
func (ti *NCTextInput) Height(_ int) int     { return 1 }

func (ti *NCTextInput) Render(width int, focused bool, base lipgloss.Style) string {
	style := base
	if focused {
		bg := base.GetBackground()
		fg := base.GetForeground()
		setInputStyle(ti.Model, bg, fg)
		ti.Model.Focus()
	} else {
		ti.Model.Blur()
	}
	return style.Width(width).Render(truncateStyled(ti.Model.View(), width))
}

// ─── NCSeparator ──────────────────────────────────────────────────────────────

// NCSeparator renders an inner horizontal divider line.
type NCSeparator struct{}

func (s *NCSeparator) Focusable() bool     { return false }
func (s *NCSeparator) Flex() bool           { return false }
func (s *NCSeparator) Height(_ int) int     { return 1 }

func (s *NCSeparator) Render(width int, _ bool, _ lipgloss.Style) string {
	// The panel renders the full divider row (with XL/XR junctions),
	// so the separator itself is just the inner horizontal line.
	return strings.Repeat("─", width)
}

// ─── NCPanel ──────────────────────────────────────────────────────────────────

// NCPanel is a bordered container that stacks controls vertically.
// It renders themed borders, a title bar, and manages focus and cursor.
type NCPanel struct {
	Title    string
	Desktop  bool // if true, use desktop-layer colors (blue); otherwise dialog-layer (gray)
	Controls []NCControl
	FocusIdx int // index into Controls; -1 = no focus

	// Computed during Render — used for cursor position and mouse routing.
	screenX, screenY int   // absolute position on screen
	width, height    int   // total panel dimensions
	innerW           int   // content width (width - 2 borders)
	controlY         []int // screen Y of each control's first row
}

// Render draws the panel at the given screen position (x, y) within the
// given width and height. Returns the rendered string (newline-joined rows).
func (p *NCPanel) Render(x, y, width, height int, t *Theme) string {
	p.screenX = x
	p.screenY = y
	p.width = width
	p.height = height
	p.innerW = width - 2
	if p.innerW < 1 {
		p.innerW = 1
	}

	var boxStyle lipgloss.Style
	if p.Desktop {
		boxStyle = lipgloss.NewStyle().Background(t.DesktopBgC()).Foreground(t.DesktopFgC())
	} else {
		boxStyle = lipgloss.NewStyle().Background(t.DialogBgC()).Foreground(t.DialogFgC())
	}
	titleStyle := lipgloss.NewStyle().Background(t.HighlightBgC()).Foreground(t.HighlightFgC()).Bold(true)
	lv := boxStyle.Render(t.OV())
	rv := boxStyle.Render(t.OV())

	var rows []string
	if p.Title != "" {
		// Title row: ┌─ Title ──────────┐
		titleText := " " + p.Title + " "
		titleRendered := titleStyle.Render(titleText)
		titleFill := p.innerW - 1 - ansi.StringWidth(titleText) // -1 for the IH after TL
		if titleFill < 0 {
			titleFill = 0
		}
		topRow := boxStyle.Render(t.OTL()+t.IH()) + titleRendered + boxStyle.Render(strings.Repeat(t.OH(), titleFill)+t.OTR())
		rows = append(rows, topRow)
	} else {
		// No title: plain top border ┌──────────┐
		rows = append(rows, boxStyle.Render(t.OTL()+strings.Repeat(t.OH(), p.innerW)+t.OTR()))
	}

	// Calculate available height for flex controls.
	// Overhead: top border(1) + bottom border(1) + separator between each pair of controls
	overhead := 2 // top + bottom border
	separators := 0
	if len(p.Controls) > 1 {
		separators = len(p.Controls) - 1
	}
	overhead += separators

	fixedH := 0
	flexCount := 0
	for _, c := range p.Controls {
		if c.Flex() {
			flexCount++
		} else {
			fixedH += c.Height(0)
		}
	}
	flexAvail := height - overhead - fixedH
	if flexAvail < 1 {
		flexAvail = 1
	}
	perFlex := flexAvail
	if flexCount > 1 {
		perFlex = flexAvail / flexCount
	}

	// Render controls.
	p.controlY = make([]int, len(p.Controls))
	currentY := y + 1 // +1 for title row

	for i, c := range p.Controls {
		if i > 0 {
			// Separator between controls.
			sep := boxStyle.Render(t.XL() + strings.Repeat(t.IH(), p.innerW) + t.XR())
			rows = append(rows, sep)
			currentY++
		}

		p.controlY[i] = currentY

		avail := perFlex
		ch := c.Height(avail)
		focused := i == p.FocusIdx

		// Use input-layer colors for text input controls.
		style := boxStyle
		if _, isInput := c.(*NCTextInput); isInput {
			style = lipgloss.NewStyle().Background(t.InputBgC()).Foreground(t.InputFgC())
		}
		content := c.Render(p.innerW, focused, style)
		for _, line := range strings.Split(content, "\n") {
			rows = append(rows, lv+line+rv)
			currentY++
		}
		_ = ch // height already accounted for in the rendered lines
	}

	// Bottom border.
	rows = append(rows, boxStyle.Render(t.OBL()+strings.Repeat(t.OH(), p.innerW)+t.OBR()))

	// Pad to fill requested height (in case controls don't fill it all).
	for len(rows) < height {
		rows = append(rows[:len(rows)-1],
			lv+boxStyle.Width(p.innerW).Render("")+rv,
			rows[len(rows)-1])
	}

	return strings.Join(rows, "\n")
}

// CursorPosition returns the absolute screen position for the cursor if a
// focusable NCTextInput control has focus. Returns visible=false otherwise.
func (p *NCPanel) CursorPosition() (cx, cy int, visible bool) {
	if p.FocusIdx < 0 || p.FocusIdx >= len(p.Controls) {
		return 0, 0, false
	}
	c := p.Controls[p.FocusIdx]
	ti, ok := c.(*NCTextInput)
	if !ok || ti.Model == nil {
		return 0, 0, false
	}
	cursor := ti.Model.Cursor()
	if cursor == nil {
		return 0, 0, false
	}
	// X: panel border(1) + cursor position within the input.
	cx = p.screenX + 1 + cursor.Position.X
	// Y: control's starting row.
	cy = p.controlY[p.FocusIdx]
	return cx, cy, true
}

// CycleFocus advances focus to the next focusable control.
func (p *NCPanel) CycleFocus() {
	if len(p.Controls) == 0 {
		return
	}
	start := p.FocusIdx
	for i := 1; i <= len(p.Controls); i++ {
		idx := (start + i) % len(p.Controls)
		if p.Controls[idx].Focusable() {
			p.FocusIdx = idx
			return
		}
	}
}

// FocusFirst sets focus to the first focusable control.
func (p *NCPanel) FocusFirst() {
	for i, c := range p.Controls {
		if c.Focusable() {
			p.FocusIdx = i
			return
		}
	}
	p.FocusIdx = -1
}

// HandleClick processes a mouse click at absolute screen position (mx, my).
// Returns true if the click was inside the panel.
func (p *NCPanel) HandleClick(mx, my int) bool {
	// Check bounds.
	if mx < p.screenX || mx >= p.screenX+p.width {
		return false
	}
	if my < p.screenY || my >= p.screenY+p.height {
		return false
	}
	// Find which control was clicked and focus it.
	for i, c := range p.Controls {
		if !c.Focusable() {
			continue
		}
		cy := p.controlY[i]
		ch := c.Height(0)
		if my >= cy && my < cy+ch {
			p.FocusIdx = i
			return true
		}
	}
	return true // consumed even if no focusable control hit
}
