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
func (l *NCLabel) Render(w, h int, layer *ThemeLayer) string {
	return layer.BaseStyle().Width(w).Render(truncateStyled(l.Text, w))
}

// ─── NCTextInput ──────────────────────────────────────────────────────────────

// NCTextInput is a basic single-line editable text field with NC-style [·····] brackets.
// It handles basic text editing only. Tab cycles focus. Enter calls OnSubmit if set.
// For command-line behavior (history, tab completion), use NCCommandInput instead.
type NCTextInput struct {
	Model    *textinput.Model
	bg       color.Color
	fg       color.Color
	OnSubmit func(text string) // called on Enter (nil = do nothing)

	// WantTab is set to true by Update when tab should cycle focus (not consumed).
	WantTab bool
}

func (ti *NCTextInput) Focusable() bool     { return true }
func (ti *NCTextInput) MinSize() (int, int) { return 4, 1 }
func (ti *NCTextInput) Value() string       { return ti.Model.Value() }
func (ti *NCTextInput) SetValue(s string)   { ti.Model.SetValue(s) }

func (ti *NCTextInput) Update(msg tea.Msg) {
	ti.WantTab = false
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if ti.OnSubmit != nil {
				text := strings.TrimSpace(ti.Model.Value())
				ti.Model.SetValue("")
				if text != "" {
					ti.OnSubmit(text)
				}
			}
			return
		case "tab":
			ti.WantTab = true // signal parent to cycle focus
			return
		}
	}
	updated, _ := ti.Model.Update(msg)
	*ti.Model = updated
}

// ─── NCCommandInput ───────────────────────────────────────────────────────────

// NCCommandInput is a single-line command input with history (Up/Down),
// tab completion (Tab when text is non-empty), and Enter-to-submit.
// Tab on empty input cycles focus to the next control.
type NCCommandInput struct {
	NCTextInput // embeds the basic text input and rendering

	OnTab func(current string) (string, bool) // tab completion callback

	History      []string
	MaxHistory   int
	historyIdx   int
	historyDraft string
}

func (ci *NCCommandInput) AddHistory(text string) {
	maxH := ci.MaxHistory
	if maxH <= 0 {
		maxH = 50
	}
	if len(ci.History) == 0 || ci.History[len(ci.History)-1] != text {
		ci.History = append(ci.History, text)
		if len(ci.History) > maxH {
			ci.History = ci.History[1:]
		}
	}
}

func (ci *NCCommandInput) Update(msg tea.Msg) {
	ci.WantTab = false
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(ci.Model.Value())
			ci.Model.SetValue("")
			ci.historyIdx = -1
			ci.historyDraft = ""
			if text != "" && ci.OnSubmit != nil {
				ci.AddHistory(text)
				ci.OnSubmit(text)
			}
			return
		case "esc":
			ci.Model.SetValue("")
			ci.historyIdx = -1
			ci.historyDraft = ""
			return
		case "up":
			if len(ci.History) == 0 {
				return
			}
			if ci.historyIdx == -1 {
				ci.historyDraft = ci.Model.Value()
				ci.historyIdx = len(ci.History) - 1
			} else if ci.historyIdx > 0 {
				ci.historyIdx--
			}
			ci.Model.SetValue(ci.History[ci.historyIdx])
			ci.Model.CursorEnd()
			return
		case "down":
			if ci.historyIdx == -1 {
				return
			}
			if ci.historyIdx < len(ci.History)-1 {
				ci.historyIdx++
				ci.Model.SetValue(ci.History[ci.historyIdx])
			} else {
				ci.historyIdx = -1
				ci.Model.SetValue(ci.historyDraft)
			}
			ci.Model.CursorEnd()
			return
		case "tab":
			// Tab on non-empty input = tab completion.
			// Tab on empty input = cycle focus.
			if ci.Model.Value() != "" && ci.OnTab != nil {
				if result, ok := ci.OnTab(ci.Model.Value()); ok {
					ci.Model.SetValue(result)
					ci.Model.CursorEnd()
				}
				return
			}
			ci.WantTab = true // empty = cycle focus
			return
		}
	}
	updated, _ := ci.Model.Update(msg)
	*ci.Model = updated
}

func (ti *NCTextInput) Render(width, height int, layer *ThemeLayer) string {
	bg := layer.InputBgC()
	fg := layer.InputFgC()
	ti.bg = bg
	ti.fg = fg

	fieldW := max(1, width-2)
	// Removed: slog.Debug here caused a feedback loop when debug logging
	// was enabled — render → slog → console event → re-render.
	bracketStyle := layer.BaseStyle()
	inputStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	dotStyle := lipgloss.NewStyle().Background(bg).Foreground(fg).Faint(true)

	ti.Model.Prompt = ""
	ti.Model.Placeholder = ""
	ti.Model.SetWidth(fieldW)
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
		// The textinput pads its output to fieldW with spaces; strip trailing
		// space padding so we can replace it with dot fill.
		stripped := strings.TrimRight(ansi.Strip(view), " ")
		usedW := ansi.StringWidth(stripped)
		dotsW := max(0, fieldW-usedW)
		trimmedView := ansi.Truncate(view, usedW, "")
		fill := dotStyle.Render(strings.Repeat("·", dotsW))
		return bracketStyle.Render("[") + trimmedView + fill + bracketStyle.Render("]")
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

	// WantTab is set to true by Update when tab should cycle focus (not consumed).
	WantTab bool
}

func (v *NCTextView) Focusable() bool     { return v.Scrollable }
func (v *NCTextView) MinSize() (int, int) { return 1, 1 }

func (v *NCTextView) Update(msg tea.Msg) {
	v.WantTab = false
	if !v.Scrollable {
		return
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			v.WantTab = true
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

func (v *NCTextView) Render(width, height int, layer *ThemeLayer) string {
	style := layer.BaseStyle()
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

func (a *NCTextArea) Render(width, height int, layer *ThemeLayer) string {
	a.height = height
	fieldW := max(1, width-2) // -2 for "[" and "]"

	bracketStyle := layer.BaseStyle()
	inputStyle := layer.InputStyle()
	dotStyle := lipgloss.NewStyle().Background(layer.InputBgC()).Foreground(layer.InputFgC()).Faint(true)

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
func (b *NCButton) Render(width, height int, layer *ThemeLayer) string {
	// Buttons use highlight style when focused (handled by parent),
	// but we render with palette's active style since buttons are action items.
	label := "[ " + b.Label + " ]"
	return layer.HighlightStyle().Render(label)
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
func (cb *NCCheckbox) Render(width, height int, layer *ThemeLayer) string {
	mark := " "
	if cb.Checked {
		mark = "x"
	}
	text := "[" + mark + "] " + cb.Label
	return layer.BaseStyle().Width(width).Render(truncateStyled(text, width))
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
func (d *NCHDivider) Render(width, height int, layer *ThemeLayer) string {
	// The actual junction chars are rendered by the window — here we just render the inner line.
	return layer.BaseStyle().Render(strings.Repeat(layer.IH(), width))
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
func (d *NCVDivider) Render(width, height int, layer *ThemeLayer) string {
	style := layer.BaseStyle()
	var rows []string
	for i := 0; i < height; i++ {
		rows = append(rows, style.Render(layer.IV()))
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

func (p *NCPanel) Render(width, height int, layer *ThemeLayer) string {
	p.innerW = max(1, width-2)
	p.innerH = max(1, height-2)

	boxStyle := layer.BaseStyle()
	titleStyle := layer.HighlightStyle()
	lv := boxStyle.Render(layer.OV())
	rv := boxStyle.Render(layer.OV())

	var topRow string
	if p.Title != "" {
		titleText := " " + p.Title + " "
		titleRendered := titleStyle.Render(titleText)
		titleFill := max(0, p.innerW-1-ansi.StringWidth(titleText))
		topRow = boxStyle.Render(layer.OTL()+layer.IH()) + titleRendered + boxStyle.Render(strings.Repeat(layer.OH(), titleFill)+layer.OTR())
	} else {
		topRow = boxStyle.Render(layer.OTL() + strings.Repeat(layer.OH(), p.innerW) + layer.OTR())
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
		content := child.Control.Render(p.innerW, ch, layer)
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
	rows = append(rows, boxStyle.Render(layer.OBL()+strings.Repeat(layer.OH(), p.innerW)+layer.OBR()))

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
