package server

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ─── NC Control interface ─────────────────────────────────────────────────────

// NCControl is a widget that can be placed inside an NCPanel.
type NCControl interface {
	// Render returns the styled content for this control.
	Render(width int, focused bool, base lipgloss.Style) string
	// Update handles a tea.Msg (key events, etc.). Only called when focused.
	Update(msg tea.Msg)
	// Height returns how many rows this control occupies.
	Height(available int) int
	Focusable() bool
	Flex() bool
}

// ─── Scrollbar helper ─────────────────────────────────────────────────────────

// renderScrollbar returns a 1-char-wide scrollbar track for the given viewport.
// total = total content lines, visible = visible lines, offset = scroll offset
// from end (0 = bottom). Returns an array of single-char strings, one per row.
func renderScrollbar(total, visible, offset int, style lipgloss.Style) []string {
	if visible <= 0 {
		return nil
	}
	track := make([]string, visible)
	if total <= visible {
		// No scrolling needed — render empty track.
		for i := range track {
			track[i] = style.Render(" ")
		}
		return track
	}

	// Thumb position and size.
	thumbSize := max(1, visible*visible/total)
	// offset=0 means bottom; scrollable range = total - visible
	scrollRange := total - visible
	topOffset := scrollRange - offset // lines scrolled from top
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

// ─── NCTextView ───────────────────────────────────────────────────────────────

// NCTextView is a read-only, scrollable, multi-line text area.
// It is a flex control that fills available vertical space.
// When Scrollable is true, it responds to PgUp/PgDn/mouse wheel
// and shows a scrollbar on the right edge.
type NCTextView struct {
	Lines        []string
	BottomAlign  bool // when true and content < height, content sticks to bottom
	Scrollable   bool // enable PgUp/PgDn/wheel scrolling
	ScrollOffset int  // lines scrolled up from bottom (0 = bottom)
	height       int  // set by panel layout
}

func (v *NCTextView) Focusable() bool     { return v.Scrollable }
func (v *NCTextView) Flex() bool           { return true }
func (v *NCTextView) Height(avail int) int { v.height = avail; return avail }

func (v *NCTextView) Update(msg tea.Msg) {
	if !v.Scrollable {
		return
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
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

func (v *NCTextView) clampScroll() {
	maxOffset := len(v.Lines) - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.ScrollOffset > maxOffset {
		v.ScrollOffset = maxOffset
	}
	if v.ScrollOffset < 0 {
		v.ScrollOffset = 0
	}
}

func (v *NCTextView) Render(width int, focused bool, base lipgloss.Style) string {
	style := base
	h := max(1, v.height)

	// Auto-scroll to bottom when new content arrives and user hasn't scrolled up.
	v.clampScroll()

	// Determine which lines to show.
	n := len(v.Lines)
	contentW := width
	showScrollbar := v.Scrollable && n > h
	if showScrollbar {
		contentW = width - 1 // reserve 1 char for scrollbar
	}
	if contentW < 1 {
		contentW = 1
	}

	var visibleLines []string
	if n == 0 {
		// Empty — fill with blanks.
		for i := 0; i < h; i++ {
			visibleLines = append(visibleLines, "")
		}
	} else {
		// End = last line index to show (exclusive).
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

	// Bottom-align: pad top with empty lines if content < height.
	var rows []string
	if v.BottomAlign && len(visibleLines) < h {
		for i := 0; i < h-len(visibleLines); i++ {
			rows = append(rows, style.Width(contentW).Render(""))
		}
	}
	for _, line := range visibleLines {
		rows = append(rows, style.Width(contentW).Render(truncateStyled(line, contentW)))
	}
	// Pad to fill height.
	for len(rows) < h {
		rows = append(rows, style.Width(contentW).Render(""))
	}

	// Add scrollbar if needed.
	if showScrollbar {
		sb := renderScrollbar(n, h, v.ScrollOffset, style)
		for i := range rows {
			if i < len(sb) {
				rows[i] = rows[i] + sb[i]
			}
		}
	}

	return strings.Join(rows, "\n")
}

// ─── NCTextInput ──────────────────────────────────────────────────────────────

// NCTextInput is a single-line editable text input control.
// It owns the textinput.Model and handles Enter, history, and tab completion
// via callbacks. The owning screen sets these callbacks to wire behavior.
type NCTextInput struct {
	Model *textinput.Model
	bg    color.Color // set by panel during render
	fg    color.Color // set by panel during render

	// Callbacks (set by the owning screen).
	OnSubmit func(text string)                       // called on Enter with trimmed text; input is auto-cleared
	OnTab    func(current string) (result string, ok bool) // called on Tab; returns replacement text
	OnEsc    func()                                  // called on Esc

	// History state (managed internally when OnHistory is set).
	History      []string // append-only; managed by the owning screen
	MaxHistory   int      // max entries (0 = 50)
	historyIdx   int      // -1 = not browsing
	historyDraft string   // saved input before browsing started
}

func (ti *NCTextInput) Focusable() bool { return true }
func (ti *NCTextInput) Flex() bool      { return false }
func (ti *NCTextInput) Height(_ int) int { return 1 }

// Value returns the current text.
func (ti *NCTextInput) Value() string { return ti.Model.Value() }

// SetValue sets the current text.
func (ti *NCTextInput) SetValue(s string) { ti.Model.SetValue(s) }

// AddHistory appends text to the history buffer.
func (ti *NCTextInput) AddHistory(text string) {
	maxH := ti.MaxHistory
	if maxH <= 0 {
		maxH = 50
	}
	if len(ti.History) == 0 || ti.History[len(ti.History)-1] != text {
		ti.History = append(ti.History, text)
		if len(ti.History) > maxH {
			ti.History = ti.History[1:]
		}
	}
}

func (ti *NCTextInput) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(ti.Model.Value())
			ti.Model.SetValue("")
			ti.historyIdx = -1
			ti.historyDraft = ""
			if text != "" && ti.OnSubmit != nil {
				ti.AddHistory(text)
				ti.OnSubmit(text)
			}
			return
		case "esc":
			ti.Model.SetValue("")
			ti.historyIdx = -1
			ti.historyDraft = ""
			if ti.OnEsc != nil {
				ti.OnEsc()
			}
			return
		case "up":
			if len(ti.History) == 0 {
				return
			}
			if ti.historyIdx == -1 {
				ti.historyDraft = ti.Model.Value()
				ti.historyIdx = len(ti.History) - 1
			} else if ti.historyIdx > 0 {
				ti.historyIdx--
			}
			ti.Model.SetValue(ti.History[ti.historyIdx])
			ti.Model.CursorEnd()
			return
		case "down":
			if ti.historyIdx == -1 {
				return
			}
			if ti.historyIdx < len(ti.History)-1 {
				ti.historyIdx++
				ti.Model.SetValue(ti.History[ti.historyIdx])
			} else {
				ti.historyIdx = -1
				ti.Model.SetValue(ti.historyDraft)
			}
			ti.Model.CursorEnd()
			return
		case "tab":
			if ti.OnTab != nil {
				if result, ok := ti.OnTab(ti.Model.Value()); ok {
					ti.Model.SetValue(result)
					ti.Model.CursorEnd()
				}
			}
			return
		}
	}
	// Forward all other messages to the underlying textinput.
	updated, _ := ti.Model.Update(msg)
	*ti.Model = updated
}

func (ti *NCTextInput) Render(width int, focused bool, base lipgloss.Style) string {
	bg := ti.bg
	fg := ti.fg

	// Layout: [text·····dots·····]
	// Brackets use parent (base) colors. Field area uses input colors.
	// Both focused and unfocused fields have input bg — only cursor differs.
	fieldW := max(1, width-2) // -2 for "[" and "]"

	bracketStyle := base // brackets match parent window
	inputStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	dotStyle := lipgloss.NewStyle().Background(bg).Foreground(fg).Faint(true)

	// Configure the underlying textinput.
	ti.Model.Prompt = ""
	ti.Model.Placeholder = ""
	ti.Model.SetWidth(fieldW + 1) // +1 so cursor can sit after last char
	s := ti.Model.Styles()
	s.Focused.Prompt = lipgloss.NewStyle()
	s.Focused.Text = inputStyle
	s.Focused.Placeholder = lipgloss.NewStyle()
	s.Cursor.Color = fg
	s.Cursor.Blink = true
	ti.Model.SetStyles(s)
	ti.Model.SetVirtualCursor(false)

	if focused {
		ti.Model.Focus()

		// Render the textinput's View() for cursor support, then fill dots.
		view := ti.Model.View()
		viewW := ansi.StringWidth(view)
		remaining := max(0, fieldW-viewW)
		fill := dotStyle.Render(strings.Repeat("·", remaining))
		return bracketStyle.Render("[") + view + fill + bracketStyle.Render("]")
	}

	// Unfocused: input bg, text + dots, no cursor.
	ti.Model.Blur()
	val := ti.Model.Value()
	textW := ansi.StringWidth(val)
	dotsW := max(0, fieldW-textW)

	if val == "" {
		// All dots on input bg.
		return bracketStyle.Render("[") +
			dotStyle.Render(strings.Repeat("·", fieldW)) +
			bracketStyle.Render("]")
	}
	text := inputStyle.Render(truncateStyled(val, fieldW))
	dots := dotStyle.Render(strings.Repeat("·", dotsW))
	return bracketStyle.Render("[") + text + dots + bracketStyle.Render("]")
}

// ─── NCSeparator ──────────────────────────────────────────────────────────────

// NCSeparator renders an inner horizontal divider line.
type NCSeparator struct{}

func (s *NCSeparator) Update(_ tea.Msg) {}
func (s *NCSeparator) Focusable() bool  { return false }
func (s *NCSeparator) Flex() bool       { return false }
func (s *NCSeparator) Height(_ int) int { return 1 }

func (s *NCSeparator) Render(width int, _ bool, _ lipgloss.Style) string {
	return strings.Repeat("─", width)
}

// ─── NCPanel ──────────────────────────────────────────────────────────────────

// NCPanel is a bordered container that stacks controls vertically.
// It renders themed borders, a title bar, and manages focus and cursor.
type NCPanel struct {
	Title    string
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
func (p *NCPanel) Render(x, y, width, height int, pal *Palette, t *Theme) string {
	p.screenX = x
	p.screenY = y
	p.width = width
	p.height = height
	p.innerW = max(1, width-2)

	boxStyle := pal.BaseStyle()
	titleStyle := pal.HighlightStyle()
	lv := boxStyle.Render(t.OV())
	rv := boxStyle.Render(t.OV())

	var rows []string
	if p.Title != "" {
		titleText := " " + p.Title + " "
		titleRendered := titleStyle.Render(titleText)
		titleFill := max(0, p.innerW-1-ansi.StringWidth(titleText))
		topRow := boxStyle.Render(t.OTL()+t.IH()) + titleRendered + boxStyle.Render(strings.Repeat(t.OH(), titleFill)+t.OTR())
		rows = append(rows, topRow)
	} else {
		rows = append(rows, boxStyle.Render(t.OTL()+strings.Repeat(t.OH(), p.innerW)+t.OTR()))
	}

	// Calculate available height for flex controls.
	overhead := 2 // top + bottom border
	if len(p.Controls) > 1 {
		overhead += len(p.Controls) - 1 // separators
	}
	fixedH := 0
	flexCount := 0
	for _, c := range p.Controls {
		if c.Flex() {
			flexCount++
		} else {
			fixedH += c.Height(0)
		}
	}
	flexAvail := max(1, height-overhead-fixedH)
	perFlex := flexAvail
	if flexCount > 1 {
		perFlex = flexAvail / flexCount
	}

	// Render controls.
	p.controlY = make([]int, len(p.Controls))
	currentY := y + 1 // +1 for title/top border row

	for i, c := range p.Controls {
		if i > 0 {
			sep := boxStyle.Render(t.XL() + strings.Repeat(t.IH(), p.innerW) + t.XR())
			rows = append(rows, sep)
			currentY++
		}

		p.controlY[i] = currentY

		avail := perFlex
		_ = c.Height(avail)
		focused := i == p.FocusIdx

		// Use input-layer colors for text input controls.
		style := boxStyle
		if ti, isInput := c.(*NCTextInput); isInput {
			ti.bg = pal.InputBgC()
			ti.fg = pal.InputFgC()
			style = lipgloss.NewStyle().Background(ti.bg).Foreground(ti.fg)
		}
		content := c.Render(p.innerW, focused, style)
		for _, line := range strings.Split(content, "\n") {
			rows = append(rows, lv+line+rv)
			currentY++
		}
	}

	// Bottom border.
	rows = append(rows, boxStyle.Render(t.OBL()+strings.Repeat(t.OH(), p.innerW)+t.OBR()))

	// Pad to fill requested height.
	for len(rows) < height {
		rows = append(rows[:len(rows)-1],
			lv+boxStyle.Width(p.innerW).Render("")+rv,
			rows[len(rows)-1])
	}

	return strings.Join(rows, "\n")
}

// HandleUpdate routes a tea.Msg to the focused control.
func (p *NCPanel) HandleUpdate(msg tea.Msg) {
	if p.FocusIdx >= 0 && p.FocusIdx < len(p.Controls) {
		if p.Controls[p.FocusIdx].Focusable() {
			p.Controls[p.FocusIdx].Update(msg)
		}
	}
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
	// +1 for panel border, +1 for "[" bracket
	cx = p.screenX + 2 + cursor.Position.X
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
func (p *NCPanel) HandleClick(mx, my int) bool {
	if mx < p.screenX || mx >= p.screenX+p.width {
		return false
	}
	if my < p.screenY || my >= p.screenY+p.height {
		return false
	}
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
	return true
}
