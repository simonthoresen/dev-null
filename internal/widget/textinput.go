package widget

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
	"null-space/internal/theme"
)

// TextInput is a basic single-line editable text field with NC-style [·····] brackets.
// It handles basic text editing only. Tab cycles focus. Enter calls OnSubmit if set.
// For command-line behavior (history, tab completion), use CommandInput instead.
type TextInput struct {
	Model    *textinput.Model
	bg       color.Color
	fg       color.Color
	OnSubmit func(text string) // called on Enter (nil = do nothing)

	// WantTab/WantBackTab are set by Update when tab should cycle focus.
	WantTab     bool
	WantBackTab bool
}

func (ti *TextInput) TabWant() (bool, bool) { return ti.WantTab, ti.WantBackTab }

func (ti *TextInput) Focusable() bool     { return true }
func (ti *TextInput) MinSize() (int, int) { return 4, 1 }
func (ti *TextInput) Value() string       { return ti.Model.Value() }
func (ti *TextInput) SetValue(s string)   { ti.Model.SetValue(s) }

func (ti *TextInput) Update(msg tea.Msg) {
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

func (ti *TextInput) Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
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
	buf.SetChar(x, y, '[', baseFg, baseBg, common.AttrNone)
	buf.SetChar(x+width-1, y, ']', baseFg, baseBg, common.AttrNone)

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
			buf.SetChar(x+1+usedW+i, y, '·', inputFg, inputBg, common.AttrFaint)
		}
		return
	}

	val := ti.Model.Value()
	valW := ansi.StringWidth(val)
	if val == "" {
		// All dots.
		for i := 0; i < fieldW; i++ {
			buf.SetChar(x+1+i, y, '·', inputFg, inputBg, common.AttrFaint)
		}
		return
	}
	// Value + dots.
	col := 0
	for _, r := range val {
		if col >= fieldW {
			break
		}
		buf.SetChar(x+1+col, y, r, inputFg, inputBg, common.AttrNone)
		col++
	}
	dotsW := max(0, fieldW-valW)
	for i := 0; i < dotsW; i++ {
		buf.SetChar(x+1+valW+i, y, '·', inputFg, inputBg, common.AttrFaint)
	}
}

// ─── CommandInput ────────────────────────────────────────────────────────────

// CommandInput is a single-line command input with history (Up/Down),
// tab completion (Tab when text is non-empty), and Enter-to-submit.
// Tab on empty input cycles focus to the next control.
type CommandInput struct {
	TextInput // embeds the basic text input and rendering

	OnTab func(current string) (string, bool) // tab completion callback
	OnEsc func()                              // called on Esc (after clearing value)

	History      []string
	MaxHistory   int
	historyIdx   int
	historyDraft string
}

func (ci *CommandInput) AddHistory(text string) {
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

func (ci *CommandInput) Update(msg tea.Msg) {
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
			if ci.OnEsc != nil {
				ci.OnEsc()
			}
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
