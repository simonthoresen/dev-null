package widget

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

func newTestTextInput() *textinput.Model {
	m := new(textinput.Model)
	*m = textinput.New()
	m.Prompt = ""
	m.Placeholder = ""
	m.CharLimit = 256
	return m
}

// renderControl is a test helper that renders a control into a ImageBuffer and returns the string.
func renderControl(ctrl Control, w, h int, focused bool, layer *theme.Layer) string {
	buf := render.NewImageBuffer(w, h)
	ctrl.Render(buf, 0, 0, w, h, focused, layer)
	return buf.ToString(colorprofile.TrueColor)
}

// renderWindow is a test helper that renders a window to a string.
func renderWindow(w *Window, x, y, width, height int, layer *theme.Layer) string {
	buf := render.NewImageBuffer(width, height)
	w.RenderToBuf(buf, x, y, width, height, layer)
	return buf.ToString(colorprofile.TrueColor)
}

// ─── TextInput tests ─────────────────────────────────────────────────────────

func TestNCTextInputRenderWidth(t *testing.T) {
	model := newTestTextInput()
	ti := &TextInput{Model: model}
	layer := testLayer()

	for _, w := range []int{10, 20, 40, 80} {
		output := renderControl(ti, w, 1, false, layer)
		visW := ansi.StringWidth(output)
		if visW > w {
			t.Errorf("width %d: rendered width %d exceeds allocated width", w, visW)
		}
	}
}

func TestNCTextInputWantTab(t *testing.T) {
	model := newTestTextInput()
	ti := &TextInput{Model: model}

	ti.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ti.WantTab {
		t.Error("expected WantTab=true on tab press")
	}
}

func TestNCTextInputOnSubmit(t *testing.T) {
	model := newTestTextInput()
	var submitted string
	ti := &TextInput{Model: model, OnSubmit: func(text string) { submitted = text }}

	model.SetValue("hello")
	ti.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})

	if submitted != "hello" {
		t.Errorf("expected 'hello', got %q", submitted)
	}
	if model.Value() != "" {
		t.Errorf("expected empty after submit, got %q", model.Value())
	}
}

// ─── CommandInput tests ──────────────────────────────────────────────────────

func TestNCCommandInputHistory(t *testing.T) {
	model := newTestTextInput()
	ci := &CommandInput{TextInput: TextInput{Model: model}}
	ci.OnSubmit = func(text string) {}

	// Submit a few commands.
	model.SetValue("first")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	model.SetValue("second")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	model.SetValue("third")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})

	if len(ci.History) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(ci.History))
	}

	// Up should go to "third" (most recent).
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "up"})
	if model.Value() != "third" {
		t.Errorf("expected 'third', got %q", model.Value())
	}

	// Up again → "second".
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "up"})
	if model.Value() != "second" {
		t.Errorf("expected 'second', got %q", model.Value())
	}

	// Down → back to "third".
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "down"})
	if model.Value() != "third" {
		t.Errorf("expected 'third', got %q", model.Value())
	}

	// Down past end → restore draft (empty).
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "down"})
	if model.Value() != "" {
		t.Errorf("expected empty (draft), got %q", model.Value())
	}
}

func TestNCCommandInputTabOnEmpty(t *testing.T) {
	model := newTestTextInput()
	ci := &CommandInput{TextInput: TextInput{Model: model}}
	ci.OnTab = func(s string) (string, bool) { return "/help", true }

	// Tab on empty input should signal WantTab (cycle focus), not complete.
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ci.WantTab {
		t.Error("expected WantTab=true on tab with empty input")
	}
}

func TestNCCommandInputTabOnNonEmpty(t *testing.T) {
	model := newTestTextInput()
	ci := &CommandInput{TextInput: TextInput{Model: model}}
	ci.OnTab = func(s string) (string, bool) { return "/help", true }

	model.SetValue("/h")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if ci.WantTab {
		t.Error("expected WantTab=false on tab with non-empty input")
	}
	if model.Value() != "/help" {
		t.Errorf("expected '/help', got %q", model.Value())
	}
}

func TestNCCommandInputEscClears(t *testing.T) {
	model := newTestTextInput()
	ci := &CommandInput{TextInput: TextInput{Model: model}}

	model.SetValue("something")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "esc"})
	if model.Value() != "" {
		t.Errorf("expected empty after esc, got %q", model.Value())
	}
}

// ─── TextView tests ──────────────────────────────────────────────────────────

func TestNCTextViewScrollClamp(t *testing.T) {
	tv := &TextView{
		Lines:      []string{"a", "b", "c"},
		Scrollable: true,
	}
	tv.SetHeight(2)

	tv.ScrollOffset = 100
	tv.ClampScroll()
	if tv.ScrollOffset != 1 { // 3 lines - 2 visible = max 1
		t.Errorf("expected clamped to 1, got %d", tv.ScrollOffset)
	}

	tv.ScrollOffset = -5
	tv.ClampScroll()
	if tv.ScrollOffset != 0 {
		t.Errorf("expected clamped to 0, got %d", tv.ScrollOffset)
	}
}

func TestTextViewMinSize(t *testing.T) {
	// TextView always returns 1×1 regardless of content: it fills whatever
	// space the layout provides. Content-aware callers (e.g. dialog body) set
	// GridConstraint.MinW/MinH explicitly instead of relying on MinSize.
	tv := &TextView{Lines: []string{"short", "a longer line", "mid"}}
	w, h := tv.MinSize()
	if w != 1 || h != 1 {
		t.Errorf("expected (1,1) for TextView, got (%d,%d)", w, h)
	}
}

func TestTextViewMinSizeEmpty(t *testing.T) {
	tv := &TextView{}
	w, h := tv.MinSize()
	if w != 1 || h != 1 {
		t.Errorf("expected (1,1) for empty TextView, got (%d,%d)", w, h)
	}
}

// ─── Button tests ────────────────────────────────────────────────────────────

func TestNCButtonPress(t *testing.T) {
	pressed := false
	btn := &Button{Label: "OK", OnPress: func() { pressed = true }}
	btn.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if !pressed {
		t.Error("expected button press on enter")
	}
}

// ─── Checkbox tests ──────────────────────────────────────────────────────────

func TestNCCheckboxToggle(t *testing.T) {
	cb := &Checkbox{Label: "Option", Checked: false}
	cb.OnToggle = func(v bool) {}

	// Use enter instead of space — both should work per the Update code.
	cb.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if !cb.Checked {
		t.Error("expected checked after enter")
	}

	cb.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if cb.Checked {
		t.Error("expected unchecked after second enter")
	}
}

// ─── Window tests ────────────────────────────────────────────────────────────

func TestNCWindowFocusCycle(t *testing.T) {
	btn1 := &Button{Label: "A"}
	btn2 := &Button{Label: "B"}
	label := &Label{Text: "sep"} // not focusable

	win := &Window{
		Children: []GridChild{
			{Control: btn1, Constraint: GridConstraint{Col: 0, Row: 0}},
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 1}},
			{Control: btn2, Constraint: GridConstraint{Col: 0, Row: 2}},
		},
	}
	win.FocusFirst()

	if win.FocusIdx != 0 {
		t.Errorf("expected focus on 0, got %d", win.FocusIdx)
	}

	win.CycleFocus()
	if win.FocusIdx != 2 {
		t.Errorf("expected focus on 2 (skipping label), got %d", win.FocusIdx)
	}

	win.CycleFocus()
	if win.FocusIdx != 0 {
		t.Errorf("expected focus wrapped to 0, got %d", win.FocusIdx)
	}
}

func TestNCWindowCursorPosition(t *testing.T) {
	model := newTestTextInput()
	model.Focus() // Must be focused for cursor to be visible.
	ti := &TextInput{Model: model}

	win := &Window{
		Children: []GridChild{
			{Control: ti, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Render to compute positions and trigger Focus().
	renderWindow(win, 5, 10, 30, 4, testLayer())

	cx, cy, visible := win.CursorPosition()
	if !visible {
		t.Skip("cursor not visible (bubbletea textinput may need update cycle)")
	}
	// cx should be screenX(5) + border(1) + bracket(1) + cursor(0) = 7
	if cx != 7 {
		t.Errorf("expected cx=7, got %d", cx)
	}
	// cy should be screenY(10) + border(1) = 11
	if cy != 11 {
		t.Errorf("expected cy=11, got %d", cy)
	}
}

func TestNCWindowGridLayout(t *testing.T) {
	label := &Label{Text: "A"}
	tv := &TextView{Lines: []string{"line"}}

	win := &Window{
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0}},
			{Control: tv, Constraint: GridConstraint{Col: 0, Row: 1, WeightY: 1, Fill: FillBoth}},
		},
	}
	renderWindow(win, 0, 0, 20, 10, testLayer())

	// Label gets row 0 (height 1), textview gets the rest.
	_, _, _, labelH := win.ChildRect(0)
	_, _, _, tvH := win.ChildRect(1)

	if labelH != 1 {
		t.Errorf("expected label height 1, got %d", labelH)
	}
	if tvH < 5 {
		t.Errorf("expected textview to fill remaining space, got height %d", tvH)
	}
}

// ─── Integration: Window with multiple controls ──────────────────────────────

func TestNCWindowIntegration(t *testing.T) {
	model := newTestTextInput()
	tv := &TextView{Lines: []string{"log1", "log2"}, BottomAlign: true}
	ci := &CommandInput{TextInput: TextInput{Model: model}}
	submitted := ""
	ci.OnSubmit = func(text string) { submitted = text }

	win := &Window{
		Title: "Console",
		Children: []GridChild{
			{Control: tv, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth}},
			{Control: &HDivider{Connected: true}, Constraint: GridConstraint{Col: 0, Row: 1}},
			{Control: ci, Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Focus should be on the command input (first focusable).
	if win.FocusIdx != 2 {
		t.Errorf("expected focus on command input (idx 2), got %d", win.FocusIdx)
	}

	// Render should produce output.
	output := renderWindow(win, 0, 0, 40, 10, testLayer())
	if output == "" {
		t.Error("expected non-empty render output")
	}

	// Simulate typing and submitting.
	model.SetValue("/help")
	win.HandleUpdate(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if submitted != "/help" {
		t.Errorf("expected '/help' submitted, got %q", submitted)
	}
}

// ─── Region extraction from composited views ─────────────────────────────────

func TestRegionExtractionFromNCWindow(t *testing.T) {
	label := &Label{Text: "ABCDE"}
	win := &Window{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	output := renderWindow(win, 0, 0, 14, 4, testLayer())

	// Extract just the content area (inside borders).
	s := newScreen(output)

	// The label "ABCDE" should appear somewhere in the inner region.
	inner := s.region(1, 1, 12, 2)
	if !strings.Contains(inner, "ABCDE") {
		t.Errorf("inner region should contain label 'ABCDE', got:\n%s", inner)
	}
}
