package server

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ─── NCLabel ──────────────────────────────────────────────────────────────────

// NCLabel is a static text control, 1 row.
type NCLabel struct {
	Text string
}

func (l *NCLabel) Update(_ tea.Msg)                              {}
func (l *NCLabel) Focusable() bool                               { return false }
func (l *NCLabel) MinSize() (int, int)                           { return ansi.StringWidth(l.Text), 1 }
func (l *NCLabel) Render(w, h int, pal *Palette, t *Theme) string {
	return pal.BaseStyle().Width(w).Render(truncateStyled(l.Text, w))
}

// ─── NCTextInput ──────────────────────────────────────────────────────────────

// NCTextInput is a single-line editable text field with NC-style [·····] brackets.
type NCTextInput struct {
	Model *textinput.Model
	bg    color.Color
	fg    color.Color

	OnSubmit func(text string)
	OnTab    func(current string) (string, bool)
	OnEsc    func()

	History    []string
	MaxHistory int
	historyIdx   int
	historyDraft string
}

func (ti *NCTextInput) Focusable() bool    { return true }
func (ti *NCTextInput) MinSize() (int, int) { return 4, 1 } // "[" + min 2 chars + "]"

func (ti *NCTextInput) Value() string       { return ti.Model.Value() }
func (ti *NCTextInput) SetValue(s string)   { ti.Model.SetValue(s) }

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
	updated, _ := ti.Model.Update(msg)
	*ti.Model = updated
}

func (ti *NCTextInput) Render(width, height int, pal *Palette, t *Theme) string {
	bg := pal.InputBgC()
	fg := pal.InputFgC()
	ti.bg = bg
	ti.fg = fg

	fieldW := max(1, width-2)
	bracketStyle := pal.BaseStyle()
	inputStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	dotStyle := lipgloss.NewStyle().Background(bg).Foreground(fg).Faint(true)

	ti.Model.Prompt = ""
	ti.Model.Placeholder = ""
	ti.Model.SetWidth(fieldW + 1)
	s := ti.Model.Styles()
	s.Focused.Prompt = lipgloss.NewStyle()
	s.Focused.Text = inputStyle
	s.Focused.Placeholder = lipgloss.NewStyle()
	s.Cursor.Color = fg
	s.Cursor.Blink = true
	ti.Model.SetStyles(s)
	ti.Model.SetVirtualCursor(false)

	focused := ti.Model.Focused()
	if focused {
		view := ti.Model.View()
		viewW := ansi.StringWidth(view)
		remaining := max(0, fieldW-viewW)
		fill := dotStyle.Render(strings.Repeat("·", remaining))
		return bracketStyle.Render("[") + view + fill + bracketStyle.Render("]")
	}

	val := ti.Model.Value()
	dotsW := max(0, fieldW-ansi.StringWidth(val))
	if val == "" {
		return bracketStyle.Render("[") +
			dotStyle.Render(strings.Repeat("·", fieldW)) +
			bracketStyle.Render("]")
	}
	text := inputStyle.Render(truncateStyled(val, fieldW))
	dots := dotStyle.Render(strings.Repeat("·", dotsW))
	return bracketStyle.Render("[") + text + dots + bracketStyle.Render("]")
}

// ─── NCTextView ───────────────────────────────────────────────────────────────

// NCTextView is a read-only, scrollable, multi-line text area.
type NCTextView struct {
	Lines        []string
	BottomAlign  bool
	Scrollable   bool
	ScrollOffset int
	height       int
}

func (v *NCTextView) Focusable() bool     { return v.Scrollable }
func (v *NCTextView) MinSize() (int, int) { return 1, 1 }

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

func (v *NCTextView) Render(width, height int, pal *Palette, t *Theme) string {
	style := pal.BaseStyle()
	v.height = height
	h := max(1, height)
	v.clampScroll()

	n := len(v.Lines)
	contentW := width
	showScrollbar := v.Scrollable && n > h
	if showScrollbar {
		contentW = max(1, width-1)
	}

	var visibleLines []string
	if n == 0 {
		for i := 0; i < h; i++ {
			visibleLines = append(visibleLines, "")
		}
	} else {
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

	var rows []string
	if v.BottomAlign && len(visibleLines) < h {
		for i := 0; i < h-len(visibleLines); i++ {
			rows = append(rows, style.Width(contentW).Render(""))
		}
	}
	for _, line := range visibleLines {
		rows = append(rows, style.Width(contentW).Render(truncateStyled(line, contentW)))
	}
	for len(rows) < h {
		rows = append(rows, style.Width(contentW).Render(""))
	}

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

// ─── NCTextArea ───────────────────────────────────────────────────────────────

// NCTextArea is a multi-line editable text area with NC-style [·····] per line.
type NCTextArea struct {
	Lines      []string
	CursorRow  int
	CursorCol  int
	ScrollTop  int // first visible row
	height     int

	OnSubmit func(lines []string) // called on Ctrl+Enter
}

func (a *NCTextArea) Focusable() bool     { return true }
func (a *NCTextArea) MinSize() (int, int) { return 4, 1 }

func (a *NCTextArea) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
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

func (a *NCTextArea) Render(width, height int, pal *Palette, t *Theme) string {
	a.height = height
	fieldW := max(1, width-2) // -2 for "[" and "]"

	bracketStyle := pal.BaseStyle()
	inputStyle := pal.InputStyle()
	dotStyle := lipgloss.NewStyle().Background(pal.InputBgC()).Foreground(pal.InputFgC()).Faint(true)

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

	var rows []string
	for i := 0; i < height; i++ {
		lineIdx := a.ScrollTop + i
		var lineContent string
		if lineIdx < len(a.Lines) {
			lineContent = a.Lines[lineIdx]
		}
		text := truncateStyled(lineContent, fieldW)
		textW := ansi.StringWidth(text)
		dotsW := max(0, fieldW-textW)
		dots := dotStyle.Render(strings.Repeat("·", dotsW))

		if lineContent != "" {
			rows = append(rows, bracketStyle.Render("[")+inputStyle.Render(text)+dots+bracketStyle.Render("]"))
		} else {
			rows = append(rows, bracketStyle.Render("[")+dotStyle.Render(strings.Repeat("·", fieldW))+bracketStyle.Render("]"))
		}
	}

	return strings.Join(rows, "\n")
}

// ─── NCButton ─────────────────────────────────────────────────────────────────

// NCButton is a clickable button: [ Label ].
type NCButton struct {
	Label   string
	OnPress func()
}

func (b *NCButton) Focusable() bool     { return true }
func (b *NCButton) MinSize() (int, int) { return len(b.Label) + 4, 1 } // "[ " + label + " ]"
func (b *NCButton) Update(msg tea.Msg) {
	if km, ok := msg.(tea.KeyPressMsg); ok && (km.String() == "enter" || km.String() == " ") {
		if b.OnPress != nil {
			b.OnPress()
		}
	}
}
func (b *NCButton) Render(width, height int, pal *Palette, t *Theme) string {
	// Buttons use highlight style when focused (handled by parent),
	// but we render with palette's active style since buttons are action items.
	label := "[ " + b.Label + " ]"
	return pal.HighlightStyle().Render(label)
}

// ─── NCCheckbox ───────────────────────────────────────────────────────────────

// NCCheckbox is a toggleable [x] Label control.
type NCCheckbox struct {
	Label    string
	Checked  bool
	OnToggle func(checked bool)
}

func (cb *NCCheckbox) Focusable() bool     { return true }
func (cb *NCCheckbox) MinSize() (int, int) { return 4 + len(cb.Label), 1 } // "[x] " + label
func (cb *NCCheckbox) Update(msg tea.Msg) {
	if km, ok := msg.(tea.KeyPressMsg); ok && (km.String() == "enter" || km.String() == " ") {
		cb.Checked = !cb.Checked
		if cb.OnToggle != nil {
			cb.OnToggle(cb.Checked)
		}
	}
}
func (cb *NCCheckbox) Render(width, height int, pal *Palette, t *Theme) string {
	mark := " "
	if cb.Checked {
		mark = "x"
	}
	text := "[" + mark + "] " + cb.Label
	return pal.BaseStyle().Width(width).Render(truncateStyled(text, width))
}

// ─── NCHDivider ───────────────────────────────────────────────────────────────

// NCHDivider is a horizontal divider line.
// Connected=true means junctions connect to the parent's outer frame (╟──╢).
// Connected=false means the divider floats inside (║──║).
type NCHDivider struct {
	Connected bool
}

func (d *NCHDivider) Update(_ tea.Msg)     {}
func (d *NCHDivider) Focusable() bool      { return false }
func (d *NCHDivider) MinSize() (int, int)  { return 1, 1 }
func (d *NCHDivider) Render(width, height int, pal *Palette, t *Theme) string {
	// The actual junction chars are rendered by the window — here we just render the inner line.
	return pal.BaseStyle().Render(strings.Repeat(t.IH(), width))
}

// ─── NCVDivider ───────────────────────────────────────────────────────────────

// NCVDivider is a vertical divider line.
// Connected=true means junctions connect to the parent's outer frame (╤ at top, ╧ at bottom).
// Connected=false means the divider floats inside.
type NCVDivider struct {
	Connected bool
}

func (d *NCVDivider) Update(_ tea.Msg)     {}
func (d *NCVDivider) Focusable() bool      { return false }
func (d *NCVDivider) MinSize() (int, int)  { return 1, 1 }
func (d *NCVDivider) Render(width, height int, pal *Palette, t *Theme) string {
	style := pal.BaseStyle()
	var rows []string
	for i := 0; i < height; i++ {
		rows = append(rows, style.Render(t.IV()))
	}
	return strings.Join(rows, "\n")
}

// ─── NCPanel ──────────────────────────────────────────────────────────────────

// NCPanel is a bordered sub-container within a window. It implements NCControl
// and can contain its own grid of children. It inherits the palette from its
// parent window.
type NCPanel struct {
	Title    string
	Children []GridChild
	FocusIdx int

	screenX, screenY int
	innerW, innerH   int
}

func (p *NCPanel) Focusable() bool     { return false } // panels aren't directly focusable; their children are
func (p *NCPanel) MinSize() (int, int) { return 4, 3 }  // min border box
func (p *NCPanel) Update(msg tea.Msg)  {}               // updates go to children directly

func (p *NCPanel) Render(width, height int, pal *Palette, t *Theme) string {
	p.innerW = max(1, width-2)
	p.innerH = max(1, height-2)

	boxStyle := pal.BaseStyle()
	titleStyle := pal.HighlightStyle()
	lv := boxStyle.Render(t.OV())
	rv := boxStyle.Render(t.OV())

	var topRow string
	if p.Title != "" {
		titleText := " " + p.Title + " "
		titleRendered := titleStyle.Render(titleText)
		titleFill := max(0, p.innerW-1-ansi.StringWidth(titleText))
		topRow = boxStyle.Render(t.OTL()+t.IH()) + titleRendered + boxStyle.Render(strings.Repeat(t.OH(), titleFill)+t.OTR())
	} else {
		topRow = boxStyle.Render(t.OTL() + strings.Repeat(t.OH(), p.innerW) + t.OTR())
	}

	// Fill inner area with base style.
	var innerRows []string
	for i := 0; i < p.innerH; i++ {
		innerRows = append(innerRows, boxStyle.Width(p.innerW).Render(""))
	}

	// TODO: render children with grid layout (for now, stack vertically).
	cy := 0
	for _, child := range p.Children {
		_, minH := child.Control.MinSize()
		ch := minH
		if child.Constraint.WeightY > 0 {
			ch = max(minH, p.innerH-cy)
		}
		if cy+ch > p.innerH {
			ch = p.innerH - cy
		}
		if ch <= 0 {
			continue
		}
		content := child.Control.Render(p.innerW, ch, pal, t)
		for j, line := range strings.Split(content, "\n") {
			if cy+j < p.innerH {
				innerRows[cy+j] = line
			}
		}
		cy += ch
	}

	var rows []string
	rows = append(rows, topRow)
	for _, ir := range innerRows {
		rows = append(rows, lv+ir+rv)
	}
	rows = append(rows, boxStyle.Render(t.OBL()+strings.Repeat(t.OH(), p.innerW)+t.OBR()))

	return strings.Join(rows, "\n")
}

// ─── Scrollbar helper ─────────────────────────────────────────────────────────

func renderScrollbar(total, visible, offset int, style lipgloss.Style) []string {
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
