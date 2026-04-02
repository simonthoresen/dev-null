package server

import (
	"image/color"
	"strings"
	"unicode/utf8"

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
func (l *NCLabel) Render(buf *ImageBuffer, x, y, w, h int, _ bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	text := l.Text
	vis := utf8.RuneCountInString(text)

	startX := x
	switch l.Align {
	case "center":
		if vis < w {
			startX = x + (w-vis)/2
		}
	case "right":
		if vis < w {
			startX = x + w - vis
		}
	}

	col := startX
	for _, r := range text {
		if col >= x+w {
			break
		}
		if col >= x {
			buf.SetChar(col, y, r, fg, bg, AttrNone)
		}
		col++
	}
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

func (ti *NCTextInput) Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer) {
	inputBg := layer.InputBgC()
	inputFg := layer.InputFgC()
	ti.bg = inputBg
	ti.fg = inputFg
	baseFg := layer.FgC()
	baseBg := layer.BgC()

	fieldW := max(1, width-2)

	// Configure the textinput model for rendering via PaintANSI.
	ti.Model.Prompt = ""
	ti.Model.Placeholder = ""
	ti.Model.SetWidth(fieldW)
	inputStyle := lipgloss.NewStyle().Background(inputBg).Foreground(inputFg)
	s := ti.Model.Styles()
	s.Focused.Prompt = lipgloss.NewStyle()
	s.Focused.Text = inputStyle
	s.Focused.Placeholder = lipgloss.NewStyle()
	s.Cursor.Color = inputFg
	s.Cursor.Blink = true
	ti.Model.SetStyles(s)
	ti.Model.SetVirtualCursor(false)

	// Brackets.
	buf.SetChar(x, y, '[', baseFg, baseBg, AttrNone)
	buf.SetChar(x+width-1, y, ']', baseFg, baseBg, AttrNone)

	hasCursor := ti.Model.Focused()
	if hasCursor {
		// Use PaintANSI to render the textinput's styled output.
		view := ti.Model.View()
		stripped := strings.TrimRight(ansi.Strip(view), " ")
		usedW := ansi.StringWidth(stripped)
		dotsW := max(0, fieldW-usedW)
		trimmedView := ansi.Truncate(view, usedW, "")
		buf.PaintANSI(x+1, y, fieldW, 1, trimmedView, inputFg, inputBg)
		// Fill remaining with dots.
		for i := 0; i < dotsW; i++ {
			buf.SetChar(x+1+usedW+i, y, '·', inputFg, inputBg, AttrFaint)
		}
		return
	}

	val := ti.Model.Value()
	valW := ansi.StringWidth(val)
	if val == "" {
		// All dots.
		for i := 0; i < fieldW; i++ {
			buf.SetChar(x+1+i, y, '·', inputFg, inputBg, AttrFaint)
		}
		return
	}
	// Value + dots.
	col := 0
	for _, r := range val {
		if col >= fieldW {
			break
		}
		buf.SetChar(x+1+col, y, r, inputFg, inputBg, AttrNone)
		col++
	}
	dotsW := max(0, fieldW-valW)
	for i := 0; i < dotsW; i++ {
		buf.SetChar(x+1+valW+i, y, '·', inputFg, inputBg, AttrFaint)
	}
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

func (v *NCTextView) Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
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
	buf.Fill(x, y, width, height, ' ', fg, bg, AttrNone)

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
		renderScrollbarBuf(buf, x+contentW, y, n, h, v.ScrollOffset, fg, bg)
	}
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

func (a *NCTextArea) Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer) {
	a.height = height
	fieldW := max(1, width-2) // -2 for "[" and "]"
	baseFg := layer.FgC()
	baseBg := layer.BgC()
	inputFg := layer.InputFgC()
	inputBg := layer.InputBgC()

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

	for i := 0; i < height; i++ {
		lineIdx := a.ScrollTop + i
		row := y + i

		// Brackets.
		buf.SetChar(x, row, '[', baseFg, baseBg, AttrNone)
		buf.SetChar(x+width-1, row, ']', baseFg, baseBg, AttrNone)

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
				buf.SetChar(x+1+col, row, r, inputFg, inputBg, AttrNone)
				col++
			}
			// Dots for remaining.
			for col < fieldW {
				buf.SetChar(x+1+col, row, '·', inputFg, inputBg, AttrFaint)
				col++
			}
		} else {
			for col := 0; col < fieldW; col++ {
				buf.SetChar(x+1+col, row, '·', inputFg, inputBg, AttrFaint)
			}
		}
	}
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
func (b *NCButton) Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	attr := PixelAttr(AttrNone)
	if focused {
		fg = layer.HighlightFgC()
		bg = layer.HighlightBgC()
		attr = AttrBold
	}
	label := "[ " + b.Label + " ]"
	col := x
	for _, r := range label {
		if col >= x+width {
			break
		}
		buf.SetChar(col, y, r, fg, bg, attr)
		col++
	}
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
func (cb *NCCheckbox) Render(buf *ImageBuffer, x, y, width, height int, focused bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	attr := PixelAttr(AttrNone)
	if focused {
		fg = layer.HighlightFgC()
		bg = layer.HighlightBgC()
		attr = AttrBold
	}
	mark := ' '
	if cb.Checked {
		mark = 'x'
	}
	text := "[" + string(mark) + "] " + cb.Label
	col := x
	for _, r := range text {
		if col >= x+width {
			break
		}
		buf.SetChar(col, y, r, fg, bg, attr)
		col++
	}
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
func (d *NCHDivider) Render(buf *ImageBuffer, x, y, width, height int, _ bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	ch := runeOf(layer.IH())
	for col := x; col < x+width; col++ {
		buf.SetChar(col, y, ch, fg, bg, AttrNone)
	}
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
func (d *NCVDivider) Render(buf *ImageBuffer, x, y, width, height int, _ bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	ch := runeOf(layer.IV())
	for row := y; row < y+height; row++ {
		buf.SetChar(x, row, ch, fg, bg, AttrNone)
	}
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

func (p *NCPanel) Render(buf *ImageBuffer, x, y, width, height int, _ bool, layer *ThemeLayer) {
	p.innerW = max(1, width-2)
	p.innerH = max(1, height-2)
	p.screenX = x
	p.screenY = y

	fg := layer.FgC()
	bg := layer.BgC()
	hlFg := layer.HighlightFgC()
	hlBg := layer.HighlightBgC()

	// Fill with background.
	buf.Fill(x, y, width, height, ' ', fg, bg, AttrNone)

	// Top border with optional title (same pattern as NCWindow).
	buf.SetChar(x, y, runeOf(layer.OTL()), fg, bg, AttrNone)
	buf.SetChar(x+width-1, y, runeOf(layer.OTR()), fg, bg, AttrNone)
	if p.Title != "" {
		titleText := " " + p.Title + " "
		buf.SetChar(x+1, y, runeOf(layer.IH()), fg, bg, AttrNone)
		n := buf.WriteString(x+2, y, titleText, hlFg, hlBg, AttrBold)
		for col := x + 2 + n; col < x+width-1; col++ {
			buf.SetChar(col, y, runeOf(layer.OH()), fg, bg, AttrNone)
		}
	} else {
		for col := x + 1; col < x+width-1; col++ {
			buf.SetChar(col, y, runeOf(layer.OH()), fg, bg, AttrNone)
		}
	}

	// Bottom border.
	boty := y + height - 1
	buf.SetChar(x, boty, runeOf(layer.OBL()), fg, bg, AttrNone)
	buf.SetChar(x+width-1, boty, runeOf(layer.OBR()), fg, bg, AttrNone)
	for col := x + 1; col < x+width-1; col++ {
		buf.SetChar(col, boty, runeOf(layer.OH()), fg, bg, AttrNone)
	}

	// Left/right borders.
	vr := runeOf(layer.OV())
	for row := y + 1; row < boty; row++ {
		buf.SetChar(x, row, vr, fg, bg, AttrNone)
		buf.SetChar(x+width-1, row, vr, fg, bg, AttrNone)
	}

	// Render children stacked vertically in the inner area.
	cy := y + 1
	for _, child := range p.Children {
		_, minH := child.Control.MinSize()
		ch := minH
		if child.Constraint.WeightY > 0 {
			ch = max(minH, (y+1+p.innerH)-cy)
		}
		if cy+ch > y+1+p.innerH {
			ch = y + 1 + p.innerH - cy
		}
		if ch <= 0 {
			continue
		}
		child.Control.Render(buf, x+1, cy, p.innerW, ch, false, layer)
		cy += ch
	}
}

// ─── Scrollbar helper ─────────────────────────────────────────────────────────

// renderScrollbarBuf writes a scrollbar track directly into the buffer.
func renderScrollbarBuf(buf *ImageBuffer, x, y, total, visible, offset int, fg, bg color.Color) {
	if visible <= 0 {
		return
	}
	if total <= visible {
		for i := 0; i < visible; i++ {
			buf.SetChar(x, y+i, ' ', fg, bg, AttrNone)
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
		buf.SetChar(x, y+i, ch, fg, bg, AttrNone)
	}
}

// renderScrollbar returns styled string slices for a scrollbar (legacy, used by widget.go).
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

func (g *NCGameView) Render(buf *ImageBuffer, x, y, width, height int, _ bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()
	if g.ViewFn == nil {
		buf.Fill(x, y, width, height, ' ', fg, bg, AttrNone)
		return
	}
	raw := g.ViewFn(width, height)
	buf.PaintANSI(x, y, width, height, raw, fg, bg)
}

// ─── NCTable ─────────────────────────────────────────────────────────────────

// NCTable renders a table from row/column data.
type NCTable struct {
	Rows [][]string
}

func (t *NCTable) Update(_ tea.Msg)           {}
func (t *NCTable) Focusable() bool            { return false }
func (t *NCTable) MinSize() (int, int)        { return 1, len(t.Rows) }

func (t *NCTable) Render(buf *ImageBuffer, x, y, width, height int, _ bool, layer *ThemeLayer) {
	fg := layer.FgC()
	bg := layer.BgC()

	if len(t.Rows) == 0 {
		return
	}

	// Calculate column widths.
	numCols := 0
	for _, row := range t.Rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	colWidths := make([]int, numCols)
	for _, row := range t.Rows {
		for c, cell := range row {
			w := utf8.RuneCountInString(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	row := y
	for _, dataRow := range t.Rows {
		if row >= y+height {
			break
		}
		col := x
		for c := 0; c < numCols; c++ {
			cell := ""
			if c < len(dataRow) {
				cell = dataRow[c]
			}
			n := buf.WriteString(col, row, cell, fg, bg, AttrNone)
			// Pad to column width.
			for i := n; i < colWidths[c]; i++ {
				buf.SetChar(col+i, row, ' ', fg, bg, AttrNone)
			}
			col += colWidths[c]
			if c < numCols-1 {
				buf.SetChar(col, row, ' ', fg, bg, AttrNone)
				col++
			}
		}
		row++
	}
}

// ─── NCContainer ─────────────────────────────────────────────────────────────

// NCContainer is a borderless layout container that arranges children
// horizontally (hsplit) or vertically (vsplit).
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

func (c *NCContainer) Render(buf *ImageBuffer, bx, by, width, height int, _ bool, layer *ThemeLayer) {
	if len(c.Children) == 0 {
		return
	}

	sizes := c.allocate(width, height)

	if c.Horizontal {
		col := bx
		for i, child := range c.Children {
			cw := sizes[i]
			if cw > 0 {
				child.Control.Render(buf, col, by, cw, height, false, layer)
			}
			col += cw
		}
	} else {
		row := by
		for i, child := range c.Children {
			ch := sizes[i]
			if ch > 0 {
				child.Control.Render(buf, bx, row, width, ch, false, layer)
			}
			row += ch
		}
	}
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
