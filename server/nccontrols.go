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
	Text  string
	Align string // "left" (default), "center", "right"
}

func (l *NCLabel) Update(_ tea.Msg)    {}
func (l *NCLabel) Focusable() bool     { return false }
func (l *NCLabel) MinSize() (int, int) { return ansi.StringWidth(l.Text), 1 }
func (l *NCLabel) Render(w, h int, _ bool, layer *ThemeLayer) string {
	text := l.Text
	vis := ansi.StringWidth(text)
	style := layer.BaseStyle()
	switch l.Align {
	case "center":
		if vis < w {
			pad := (w - vis) / 2
			text = strings.Repeat(" ", pad) + text + strings.Repeat(" ", w-vis-pad)
		}
	case "right":
		if vis < w {
			text = strings.Repeat(" ", w-vis) + text
		}
	}
	return style.Width(w).Render(truncateStyled(text, w))
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

	// WantTab/WantBackTab are set by Update when tab should cycle focus.
	WantTab     bool
	WantBackTab bool
}

func (ti *NCTextInput) TabWant() (bool, bool) { return ti.WantTab, ti.WantBackTab }

func (ti *NCTextInput) Focusable() bool     { return true }
func (ti *NCTextInput) MinSize() (int, int) { return 4, 1 }
func (ti *NCTextInput) Value() string       { return ti.Model.Value() }
func (ti *NCTextInput) SetValue(s string)   { ti.Model.SetValue(s) }

func (ti *NCTextInput) Update(msg tea.Msg) {
	ti.WantTab = false
	ti.WantBackTab = false
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
			ti.WantTab = true
			return
		case "shift+tab":
			ti.WantBackTab = true
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
	ci.WantBackTab = false
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
		case "shift+tab":
			ci.WantBackTab = true
			return
		}
	}
	updated, _ := ci.Model.Update(msg)
	*ci.Model = updated
}

func (ti *NCTextInput) Render(width, height int, focused bool, layer *ThemeLayer) string {
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

	hasCursor := ti.Model.Focused()
	if hasCursor {
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

	// WantTab/WantBackTab are set by Update when tab should cycle focus.
	WantTab     bool
	WantBackTab bool
}

func (v *NCTextView) TabWant() (bool, bool) { return v.WantTab, v.WantBackTab }

func (v *NCTextView) Focusable() bool     { return v.Scrollable }
func (v *NCTextView) MinSize() (int, int) { return 1, 1 }

func (v *NCTextView) Update(msg tea.Msg) {
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

func (v *NCTextView) Render(width, height int, focused bool, layer *ThemeLayer) string {
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

	WantTab     bool
	WantBackTab bool
}

func (a *NCTextArea) Focusable() bool                 { return true }
func (a *NCTextArea) MinSize() (int, int)              { return 4, 1 }
func (a *NCTextArea) TabWant() (bool, bool)            { return a.WantTab, a.WantBackTab }

func (a *NCTextArea) Update(msg tea.Msg) {
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

func (a *NCTextArea) Render(width, height int, focused bool, layer *ThemeLayer) string {
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

	WantTab     bool
	WantBackTab bool
}

func (b *NCButton) Focusable() bool              { return true }
func (b *NCButton) MinSize() (int, int)           { return len(b.Label) + 4, 1 } // "[ " + label + " ]"
func (b *NCButton) TabWant() (bool, bool)         { return b.WantTab, b.WantBackTab }
func (b *NCButton) Update(msg tea.Msg) {
	b.WantTab = false
	b.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter", " ":
			if b.OnPress != nil {
				b.OnPress()
			}
		case "tab":
			b.WantTab = true
		case "shift+tab":
			b.WantBackTab = true
		}
	}
}
func (b *NCButton) Render(width, height int, focused bool, layer *ThemeLayer) string {
	label := "[ " + b.Label + " ]"
	if focused {
		return layer.HighlightStyle().Render(label)
	}
	return layer.BaseStyle().Render(label)
}

// ─── NCCheckbox ───────────────────────────────────────────────────────────────

// NCCheckbox is a toggleable [x] Label control.
type NCCheckbox struct {
	Label    string
	Checked  bool
	OnToggle func(checked bool)

	WantTab     bool
	WantBackTab bool
}

func (cb *NCCheckbox) Focusable() bool          { return true }
func (cb *NCCheckbox) MinSize() (int, int)       { return 4 + len(cb.Label), 1 } // "[x] " + label
func (cb *NCCheckbox) TabWant() (bool, bool)     { return cb.WantTab, cb.WantBackTab }
func (cb *NCCheckbox) Update(msg tea.Msg) {
	cb.WantTab = false
	cb.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter", " ":
			cb.Checked = !cb.Checked
			if cb.OnToggle != nil {
				cb.OnToggle(cb.Checked)
			}
		case "tab":
			cb.WantTab = true
		case "shift+tab":
			cb.WantBackTab = true
		}
	}
}
func (cb *NCCheckbox) Render(width, height int, focused bool, layer *ThemeLayer) string {
	mark := " "
	if cb.Checked {
		mark = "x"
	}
	text := "[" + mark + "] " + cb.Label
	style := layer.BaseStyle()
	if focused {
		style = layer.HighlightStyle()
	}
	return style.Width(width).Render(truncateStyled(text, width))
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
func (d *NCHDivider) Render(width, height int, _ bool, layer *ThemeLayer) string {
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
func (d *NCVDivider) Render(width, height int, _ bool, layer *ThemeLayer) string {
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

func (p *NCPanel) Render(width, height int, _ bool, layer *ThemeLayer) string {
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
		content := child.Control.Render(p.innerW, ch, false, layer)
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

// ─── NCGameView ──────────────────────────────────────────────────────────────

// NCGameView wraps a game's View() function as an NCControl. When focused,
// non-Tab keys are forwarded to the game via OnKey.
type NCGameView struct {
	ViewFn               func(w, h int) string
	OnKey                func(key string) // bound to game.OnInput(playerID, key)
	focusable            bool
	WantTab, WantBackTab bool
}

func (g *NCGameView) Focusable() bool     { return g.focusable }
func (g *NCGameView) MinSize() (int, int) { return 1, 1 }
func (g *NCGameView) TabWant() (bool, bool) {
	fwd, back := g.WantTab, g.WantBackTab
	g.WantTab = false
	g.WantBackTab = false
	return fwd, back
}

func (g *NCGameView) Update(msg tea.Msg) {
	g.WantTab = false
	g.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "tab":
			g.WantTab = true
		case "shift+tab":
			g.WantBackTab = true
		default:
			if g.OnKey != nil {
				key := km.String()
				if key == "space" {
					key = " "
				}
				g.OnKey(key)
			}
		}
	}
}

func (g *NCGameView) Render(width, height int, _ bool, layer *ThemeLayer) string {
	if g.ViewFn == nil {
		blank := strings.Repeat(" ", width)
		var rows []string
		for range height {
			rows = append(rows, blank)
		}
		return strings.Join(rows, "\n")
	}
	raw := g.ViewFn(width, height)
	lines := strings.Split(raw, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = fitLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

// ─── NCTable ─────────────────────────────────────────────────────────────────

// NCTable renders a table from row/column data.
type NCTable struct {
	Rows [][]string
}

func (t *NCTable) Update(_ tea.Msg)           {}
func (t *NCTable) Focusable() bool            { return false }
func (t *NCTable) MinSize() (int, int)        { return 1, len(t.Rows) }

func (t *NCTable) Render(width, height int, _ bool, layer *ThemeLayer) string {
	if len(t.Rows) == 0 {
		blank := strings.Repeat(" ", width)
		var lines []string
		for range height {
			lines = append(lines, blank)
		}
		return strings.Join(lines, "\n")
	}

	// Calculate column widths.
	cols := 0
	for _, row := range t.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	colWidths := make([]int, cols)
	for _, row := range t.Rows {
		for c, cell := range row {
			w := ansi.StringWidth(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	var result []string
	for _, row := range t.Rows {
		var line strings.Builder
		for c := range cols {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			line.WriteString(fitLine(cell, colWidths[c]))
			if c < cols-1 {
				line.WriteByte(' ')
			}
		}
		result = append(result, fitLine(line.String(), width))
	}

	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	if len(result) > height {
		result = result[:height]
	}
	return strings.Join(result, "\n")
}

// ─── NCContainer ─────────────────────────────────────────────────────────────

// NCContainer is a borderless layout container that arranges children
// horizontally (hsplit) or vertically (vsplit). It replaces the duplicated
// layout logic that was in ncrender.go.
type NCContainer struct {
	Horizontal bool // true = side-by-side, false = stacked
	Children   []ContainerChild
}

// ContainerChild pairs an NCControl with its sizing info.
type ContainerChild struct {
	Control NCControl
	Weight  float64 // flex weight (0 = use Fixed)
	Fixed   int     // fixed size (0 = use Weight)
}

func (c *NCContainer) Update(_ tea.Msg)     {}
func (c *NCContainer) Focusable() bool      { return false }
func (c *NCContainer) MinSize() (int, int)  { return 1, 1 }

func (c *NCContainer) Render(width, height int, _ bool, layer *ThemeLayer) string {
	if len(c.Children) == 0 {
		blank := strings.Repeat(" ", width)
		var lines []string
		for range height {
			lines = append(lines, blank)
		}
		return strings.Join(lines, "\n")
	}

	// Compute sizes using the same allocation logic.
	sizes := c.allocate(width, height)

	if c.Horizontal {
		return c.renderHorizontal(sizes, width, height, layer)
	}
	return c.renderVertical(sizes, width, height, layer)
}

func (c *NCContainer) allocate(width, height int) []int {
	total := height
	if c.Horizontal {
		total = width
	}
	sizes := make([]int, len(c.Children))
	remaining := total
	totalWeight := 0.0

	for i, child := range c.Children {
		if child.Fixed > 0 {
			sizes[i] = min(child.Fixed, remaining)
			remaining -= sizes[i]
		} else {
			w := child.Weight
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
	}

	if totalWeight > 0 && remaining > 0 {
		distributed := 0
		for i, child := range c.Children {
			if child.Fixed > 0 {
				continue
			}
			w := child.Weight
			if w <= 0 {
				w = 1
			}
			sizes[i] = int(float64(remaining) * w / totalWeight)
			distributed += sizes[i]
		}
		leftover := remaining - distributed
		for i, child := range c.Children {
			if child.Fixed == 0 {
				sizes[i] += leftover
				break
			}
			_ = child
		}
	}
	return sizes
}

func (c *NCContainer) renderHorizontal(widths []int, totalW, height int, layer *ThemeLayer) string {
	// Render each child column.
	childCols := make([][]string, len(c.Children))
	for i, child := range c.Children {
		cw := widths[i]
		rendered := child.Control.Render(cw, height, false, layer)
		childCols[i] = strings.Split(rendered, "\n")
	}

	// Merge columns side by side.
	result := make([]string, height)
	for y := range height {
		var row strings.Builder
		for i, cols := range childCols {
			cw := widths[i]
			if y < len(cols) {
				row.WriteString(fitLine(cols[y], cw))
			} else {
				row.WriteString(strings.Repeat(" ", cw))
			}
		}
		result[y] = row.String()
	}
	return strings.Join(result, "\n")
}

func (c *NCContainer) renderVertical(heights []int, width, totalH int, layer *ThemeLayer) string {
	var lines []string
	for i, child := range c.Children {
		ch := heights[i]
		rendered := child.Control.Render(width, ch, false, layer)
		lines = append(lines, strings.Split(rendered, "\n")...)
	}

	for len(lines) < totalH {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) > totalH {
		lines = lines[:totalH]
	}
	return strings.Join(lines, "\n")
}
