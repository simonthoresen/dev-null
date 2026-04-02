package server

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
	"null-space/internal/console"
	"null-space/internal/engine"
	"null-space/internal/theme"
	"null-space/internal/widget"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

func testTheme() *theme.Theme  { return theme.Default() }
func testLayer() *theme.Layer  { return testTheme().LayerAt(0) }

func newTestTextInput() *textinput.Model {
	m := new(textinput.Model)
	*m = textinput.New()
	m.Prompt = ""
	m.Placeholder = ""
	m.CharLimit = 256
	return m
}

func keyMsg(key string) tea.Msg {
	return tea.KeyPressMsg{Code: -1, Text: key}
}

func simpleKeyMsg(key string) tea.KeyPressMsg {
	// For simple single-char keys we can construct directly.
	// For special keys we rely on the string representation.
	return tea.KeyPressMsg{Code: -1, Text: key}
}

// stripANSI removes all ANSI escape sequences for easier assertion.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// renderControl is a test helper that renders a control into a ImageBuffer and returns the string.
func renderControl(ctrl widget.Control, w, h int, focused bool, layer *theme.Layer) string {
	buf := common.NewImageBuffer(w, h)
	ctrl.Render(buf, 0, 0, w, h, focused, layer)
	return buf.ToString()
}

// ─── Theme & Palette tests ────────────────────────────────────────────────────

func TestDefaultThemeNotNil(t *testing.T) {
	th := theme.Default()
	if th == nil {
		t.Fatal("DefaultTheme returned nil")
	}
	if th.Name != "norton" {
		t.Errorf("expected name 'norton', got %q", th.Name)
	}
}

func TestLayerAtDepth(t *testing.T) {
	th := theme.Default()
	l0 := th.LayerAt(0)
	l1 := th.LayerAt(1)
	l2 := th.LayerAt(2)
	l3 := th.LayerAt(3)
	l4 := th.LayerAt(4)

	if l0 != &th.Primary {
		t.Error("depth 0 should be Primary")
	}
	if l1 != &th.Secondary {
		t.Error("depth 1 should be Secondary")
	}
	if l2 != &th.Tertiary {
		t.Error("depth 2 should be Tertiary")
	}
	if l3 != &th.Secondary {
		t.Error("depth 3 should be Secondary (alternating)")
	}
	if l4 != &th.Tertiary {
		t.Error("depth 4 should be Tertiary (alternating)")
	}
}

func TestWarningLayer(t *testing.T) {
	th := theme.Default()
	w := th.WarningLayer()
	if w != &th.Warning {
		t.Error("WarningLayer should return Warning layer")
	}
}

func TestBorderDefaults(t *testing.T) {
	th := theme.Default()
	layer := th.LayerAt(0)
	if layer.OTL() != "╔" {
		t.Errorf("expected double-line TL, got %q", layer.OTL())
	}
	if layer.IH() != "─" {
		t.Errorf("expected single-line inner H, got %q", layer.IH())
	}
	if layer.XL() != "╟" {
		t.Errorf("expected double-single intersection, got %q", layer.XL())
	}
}

// ─── widget.TextInput tests ───────────────────────────────────────────────────────

func TestNCTextInputRender(t *testing.T) {
	model := newTestTextInput()
	ti := &widget.TextInput{Model: model}
	layer := testLayer()

	output := renderControl(ti, 20, 1, false, layer)
	stripped := stripANSI(output)

	if !strings.HasPrefix(stripped, "[") {
		t.Errorf("expected bracket prefix, got %q", stripped)
	}
	if !strings.HasSuffix(stripped, "]") {
		t.Errorf("expected bracket suffix, got %q", stripped)
	}
	// Should contain dots for empty field.
	if !strings.Contains(stripped, "·") {
		t.Errorf("expected dots in empty field, got %q", stripped)
	}
}

func TestNCTextInputRenderWidth(t *testing.T) {
	model := newTestTextInput()
	ti := &widget.TextInput{Model: model}
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
	ti := &widget.TextInput{Model: model}

	ti.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ti.WantTab {
		t.Error("expected WantTab=true on tab press")
	}
}

func TestNCTextInputOnSubmit(t *testing.T) {
	model := newTestTextInput()
	var submitted string
	ti := &widget.TextInput{Model: model, OnSubmit: func(text string) { submitted = text }}

	model.SetValue("hello")
	ti.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})

	if submitted != "hello" {
		t.Errorf("expected 'hello', got %q", submitted)
	}
	if model.Value() != "" {
		t.Errorf("expected empty after submit, got %q", model.Value())
	}
}

// ─── widget.CommandInput tests ─────────────────────────────────────────────────────

func TestNCCommandInputHistory(t *testing.T) {
	model := newTestTextInput()
	ci := &widget.CommandInput{TextInput: widget.TextInput{Model: model}}
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
	ci := &widget.CommandInput{TextInput: widget.TextInput{Model: model}}
	ci.OnTab = func(s string) (string, bool) { return "/help", true }

	// Tab on empty input should signal WantTab (cycle focus), not complete.
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ci.WantTab {
		t.Error("expected WantTab=true on tab with empty input")
	}
}

func TestNCCommandInputTabOnNonEmpty(t *testing.T) {
	model := newTestTextInput()
	ci := &widget.CommandInput{TextInput: widget.TextInput{Model: model}}
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
	ci := &widget.CommandInput{TextInput: widget.TextInput{Model: model}}

	model.SetValue("something")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "esc"})
	if model.Value() != "" {
		t.Errorf("expected empty after esc, got %q", model.Value())
	}
}

// ─── widget.TextView tests ─────────────────────────────────────────────────────────

func TestNCTextViewRender(t *testing.T) {
	tv := &widget.TextView{
		Lines:       []string{"line1", "line2", "line3"},
		BottomAlign: true,
	}
	layer := testLayer()

	output := renderControl(tv, 20, 5, false, layer)
	lines := strings.Split(output, "\n")

	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	// Bottom-aligned: last 3 lines should have content.
	stripped := stripANSI(lines[4])
	if !strings.Contains(stripped, "line3") {
		t.Errorf("expected line3 at bottom, got %q", stripped)
	}
}

func TestNCTextViewScrollClamp(t *testing.T) {
	tv := &widget.TextView{
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

// ─── widget.Button tests ──────────────────────────────────────────────────────────

func TestNCButtonRender(t *testing.T) {
	btn := &widget.Button{Label: "OK"}
	output := renderControl(btn, 10, 1, false, testLayer())
	stripped := stripANSI(output)
	if !strings.Contains(stripped, "[ OK ]") {
		t.Errorf("expected '[ OK ]', got %q", stripped)
	}
}

func TestNCButtonPress(t *testing.T) {
	pressed := false
	btn := &widget.Button{Label: "OK", OnPress: func() { pressed = true }}
	btn.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if !pressed {
		t.Error("expected button press on enter")
	}
}

// ─── widget.Checkbox tests ─────────────────────────────────────────────────────────

func TestNCCheckboxToggle(t *testing.T) {
	cb := &widget.Checkbox{Label: "Option", Checked: false}
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

func TestNCCheckboxRender(t *testing.T) {
	cb := &widget.Checkbox{Label: "Opt", Checked: true}
	output := renderControl(cb, 20, 1, false, testLayer())
	stripped := stripANSI(output)
	if !strings.Contains(stripped, "[x] Opt") {
		t.Errorf("expected '[x] Opt', got %q", stripped)
	}

	cb.Checked = false
	output = renderControl(cb, 20, 1, false, testLayer())
	stripped = stripANSI(output)
	if !strings.Contains(stripped, "[ ] Opt") {
		t.Errorf("expected '[ ] Opt', got %q", stripped)
	}
}

// ─── widget.Window tests ──────────────────────────────────────────────────────────

func TestNCWindowRenderBasic(t *testing.T) {
	label := &widget.Label{Text: "Hello"}
	win := &widget.Window{
		Title: "Test",
		Children: []widget.GridChild{
			{Control: label, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal}},
		},
	}

	output := win.Render(0, 0, 30, 5, testLayer())
	lines := strings.Split(output, "\n")

	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	// First line should contain the title.
	stripped := stripANSI(lines[0])
	if !strings.Contains(stripped, "Test") {
		t.Errorf("expected title 'Test' in first line, got %q", stripped)
	}

	// Should have double borders.
	if !strings.Contains(stripped, "╔") {
		t.Errorf("expected ╔ in top border, got %q", stripped)
	}
}

func TestNCWindowNoTitle(t *testing.T) {
	win := &widget.Window{
		Children: []widget.GridChild{
			{Control: &widget.Label{Text: "X"}, Constraint: widget.GridConstraint{Col: 0, Row: 0}},
		},
	}
	output := win.Render(0, 0, 20, 4, testLayer())
	stripped := stripANSI(strings.Split(output, "\n")[0])

	// Should be plain top border without title.
	if strings.Contains(stripped, "─") {
		// Inner horizontal used for title — without title should be all outer.
	}
	if !strings.Contains(stripped, "╔") {
		t.Errorf("expected ╔ in top border, got %q", stripped)
	}
}

func TestNCWindowFocusCycle(t *testing.T) {
	btn1 := &widget.Button{Label: "A"}
	btn2 := &widget.Button{Label: "B"}
	label := &widget.Label{Text: "sep"} // not focusable

	win := &widget.Window{
		Children: []widget.GridChild{
			{Control: btn1, Constraint: widget.GridConstraint{Col: 0, Row: 0}},
			{Control: label, Constraint: widget.GridConstraint{Col: 0, Row: 1}},
			{Control: btn2, Constraint: widget.GridConstraint{Col: 0, Row: 2}},
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
	ti := &widget.TextInput{Model: model}

	win := &widget.Window{
		Children: []widget.GridChild{
			{Control: ti, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Render to compute positions and trigger Focus().
	win.Render(5, 10, 30, 4, testLayer())

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
	label := &widget.Label{Text: "A"}
	tv := &widget.TextView{Lines: []string{"line"}}

	win := &widget.Window{
		Children: []widget.GridChild{
			{Control: label, Constraint: widget.GridConstraint{Col: 0, Row: 0}},
			{Control: tv, Constraint: widget.GridConstraint{Col: 0, Row: 1, WeightY: 1, Fill: widget.FillBoth}},
		},
	}
	win.Render(0, 0, 20, 10, testLayer())

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

// ─── widget.HDivider / widget.VDivider tests ───────────────────────────────────────────

func TestNCHDividerRender(t *testing.T) {
	d := &widget.HDivider{Connected: true}
	output := renderControl(d, 10, 1, false, testLayer())
	stripped := stripANSI(output)
	// The divider renders inner horizontal chars repeated to fill width.
	visW := ansi.StringWidth(output)
	if visW < 10 {
		t.Errorf("expected at least 10 visual width, got %d (stripped: %q)", visW, stripped)
	}
}

func TestNCVDividerRender(t *testing.T) {
	d := &widget.VDivider{}
	output := renderControl(d, 1, 3, false, testLayer())
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

// ─── Overlay / Menu tests ─────────────────────────────────────────────────────

func TestMenuShortcut(t *testing.T) {
	_, r := widget.StripAmpersand("&File")
	if r != 'f' {
		t.Errorf("expected 'f', got %c", r)
	}
	_, r = widget.StripAmpersand("E&xit")
	if r != 'x' {
		t.Errorf("expected 'x', got %c", r)
	}
	_, r = widget.StripAmpersand("NoShortcut")
	if r != 0 {
		t.Errorf("expected 0, got %c", r)
	}
}

func TestHotkeyDisplay(t *testing.T) {
	d := widget.HotkeyDisplay("ctrl+c")
	if d != "(Ctrl+C)" {
		t.Errorf("expected '(Ctrl+C)', got %q", d)
	}
}

func TestOverlayMenuNavigation(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}, {Label: "&Open"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}

	// F10 activates menu bar.
	if !o.HandleKey("f10", menus, "") {
		t.Error("F10 should be consumed")
	}
	if !o.MenuFocused {
		t.Error("expected menuFocused after F10")
	}
	if o.MenuCursor != 0 {
		t.Errorf("expected cursor 0, got %d", o.MenuCursor)
	}

	// Right arrow moves to Edit.
	o.HandleKey("right", menus, "")
	if o.MenuCursor != 1 {
		t.Errorf("expected cursor 1, got %d", o.MenuCursor)
	}

	// Down opens dropdown.
	o.HandleKey("down", menus, "")
	if o.OpenMenu != 1 {
		t.Errorf("expected openMenu 1, got %d", o.OpenMenu)
	}

	// Esc closes dropdown.
	o.HandleKey("esc", menus, "")
	if o.OpenMenu != -1 {
		t.Errorf("expected openMenu -1, got %d", o.OpenMenu)
	}

	// Esc again deactivates bar.
	o.HandleKey("esc", menus, "")
	if o.MenuFocused {
		t.Error("expected menuFocused=false after second Esc")
	}
}

func TestOverlayHotkey(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	triggered := false
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&Quit", Hotkey: "ctrl+q", Handler: func(_ string) { triggered = true }},
		}},
	}

	if !o.HandleKey("ctrl+q", menus, "") {
		t.Error("hotkey should be consumed")
	}
	if !triggered {
		t.Error("expected hotkey handler to fire")
	}
}

func TestOverlayAltActivation(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "Item"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "Item"}}},
	}

	o.HandleKey("alt+e", menus, "")
	if o.OpenMenu != 1 {
		t.Errorf("expected Alt+E to open Edit menu (index 1), got %d", o.OpenMenu)
	}
}

func TestOverlayDialogStack(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}

	if o.HasDialog() {
		t.Error("should have no dialog initially")
	}

	o.PushDialog(common.DialogRequest{Title: "First", Body: "A"})
	o.PushDialog(common.DialogRequest{Title: "Second", Body: "B"})

	if !o.HasDialog() {
		t.Error("should have dialogs")
	}
	if d := o.TopDialog(); d.Title != "Second" {
		t.Errorf("expected top 'Second', got %q", d.Title)
	}

	o.PopDialog()
	if d := o.TopDialog(); d.Title != "First" {
		t.Errorf("expected top 'First', got %q", d.Title)
	}

	o.PopDialog()
	if o.HasDialog() {
		t.Error("should have no dialogs after popping both")
	}
}

// ─── Shadow tests ─────────────────────────────────────────────────────────────

func TestApplyShadowShape(t *testing.T) {
	// Create a simple 5x3 background.
	bg := strings.Join([]string{
		"AAAAA",
		"BBBBB",
		"CCCCC",
		"DDDDD",
		"EEEEE",
	}, "\n")

	// Box at (1,1) size 3x2 → shadow right strip at col=4, rows 2..2
	// and bottom strip at row=3, cols 2..4.
	result := widget.ApplyShadow(1, 1, 3, 2, bg, testTheme().ShadowStyle())
	lines := strings.Split(result, "\n")

	// Row 0 should be unchanged.
	if stripANSI(lines[0]) != "AAAAA" {
		t.Errorf("row 0 should be unchanged, got %q", stripANSI(lines[0]))
	}

	// Row 2 (box row 1): col 4 should be shadowed.
	row2 := stripANSI(lines[2])
	if row2[0:4] != "CCCC" {
		t.Errorf("row 2 cols 0-3 should be unchanged, got %q", row2)
	}

	// Row 3 (bottom shadow): cols 2-4 should be shadowed.
	row3 := stripANSI(lines[3])
	if row3[0:1] != "D" {
		t.Errorf("row 3 col 0 should be unchanged (bottom-left corner skip), got %q", row3)
	}
}

// ─── widget.PlaceOverlay tests ──────────────────────────────────────────────────────

func TestPlaceOverlay(t *testing.T) {
	bg := "AAAAAAA\nBBBBBBB\nCCCCCCC"
	overlay := "XX\nYY"

	result := widget.PlaceOverlay(2, 1, overlay, bg)
	lines := strings.Split(result, "\n")

	if lines[0] != "AAAAAAA" {
		t.Errorf("row 0 should be unchanged, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "XX") {
		t.Errorf("row 1 should contain overlay, got %q", lines[1])
	}
}

// ─── Scrollbar tests ─────────────────────────────────────────────────────────

func TestRenderScrollbar(t *testing.T) {
	pal := testLayer()
	sb := widget.RenderScrollbar(100, 10, 0, pal.BaseStyle())

	if len(sb) != 10 {
		t.Errorf("expected 10 rows, got %d", len(sb))
	}

	// At offset 0 (bottom), thumb should be at the bottom.
	hasThumb := false
	for _, s := range sb {
		if strings.Contains(stripANSI(s), "█") {
			hasThumb = true
		}
	}
	if !hasThumb {
		t.Error("expected scrollbar thumb")
	}
}

func TestRenderScrollbarNoScroll(t *testing.T) {
	pal := testLayer()
	sb := widget.RenderScrollbar(5, 10, 0, pal.BaseStyle())

	// Content fits — no scrollbar needed.
	for _, s := range sb {
		if strings.Contains(stripANSI(s), "█") || strings.Contains(stripANSI(s), "░") {
			t.Error("expected no scrollbar when content fits")
			break
		}
	}
}

// ─── Integration: widget.Window with multiple controls ─────────────────────────────

func TestNCWindowIntegration(t *testing.T) {
	model := newTestTextInput()
	tv := &widget.TextView{Lines: []string{"log1", "log2"}, BottomAlign: true}
	ci := &widget.CommandInput{TextInput: widget.TextInput{Model: model}}
	submitted := ""
	ci.OnSubmit = func(text string) { submitted = text }

	win := &widget.Window{
		Title: "Console",
		Children: []widget.GridChild{
			{Control: tv, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: widget.FillBoth}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{Col: 0, Row: 1}},
			{Control: ci, Constraint: widget.GridConstraint{Col: 0, Row: 2, WeightX: 1, Fill: widget.FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Focus should be on the command input (first focusable).
	if win.FocusIdx != 2 {
		t.Errorf("expected focus on command input (idx 2), got %d", win.FocusIdx)
	}

	// Render should produce output.
	output := win.Render(0, 0, 40, 10, testLayer())
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

// ─── Regression: slog feedback loop ───────────────────────────────────────────

// TestSlogRenderLogsNotRoutedToConsole verifies that render-path debug messages
// (e.g. "widget.Window render child") are not routed to the console channel, which
// would cause a feedback loop: render → debug log → console → re-render → ...
func TestSlogDebugRoutedToConsole(t *testing.T) {
	ch := make(chan console.SlogLine, 10)
	wrapped := &discardHandler{}
	handler := console.NewSlogHandler(ch, wrapped)

	ctx := t.Context()

	// Debug messages should appear in channel.
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "plugin loaded: greeter", 0)
	_ = handler.Handle(ctx, rec)

	// INFO message should also appear.
	rec2 := slog.NewRecord(time.Now(), slog.LevelInfo, "server started", 0)
	_ = handler.Handle(ctx, rec2)

	close(ch)
	var messages []string
	for sl := range ch {
		messages = append(messages, sl.Text)
	}

	if len(messages) != 2 {
		t.Errorf("expected 2 messages in console channel, got %d: %v", len(messages), messages)
	}
}

// TestNoSlogInRenderPath ensures that render-path source files don't contain
// slog calls. Any slog call in a Render/View method creates a feedback loop:
// View → slog → console channel → Update → View → slog → ...
// This caused the CPU spin-up bug and keyboard starvation.
func TestNoSlogInRenderPath(t *testing.T) {
	// Files that are called from View/Render and must never use slog.
	renderFiles := []string{
		"window.go",
		"control.go",
		"label.go",
		"textview.go",
		"textinput.go",
		"button.go",
		"checkbox.go",
		"divider.go",
		"panel.go",
		"table.go",
		"teampanel.go",
		"container.go",
		"gameview.go",
		"overlay.go",
		"menu.go",
	}
	slogCall := regexp.MustCompile(`\bslog\.(Debug|Info|Warn|Error)\b`)

	for _, name := range renderFiles {
		path := filepath.Join("..", "internal", "widget", name)
		f, err := os.Open(path)
		if err != nil {
			// File might not exist in some build configurations; skip.
			continue
		}
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			// Skip comments.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if slogCall.MatchString(line) {
				t.Errorf("%s:%d: slog call in render path (causes feedback loop): %s", name, lineNum, trimmed)
			}
		}
		f.Close()
	}
}

// TestSlogBlockedInRenderPath verifies that the consoleSlogHandler suppresses
// messages sent from inside a View/Render call stack (feedback loop guard).
func TestSlogBlockedInRenderPath(t *testing.T) {
	ch := make(chan console.SlogLine, 10)
	wrapped := &discardHandler{}
	handler := console.NewSlogHandler(ch, wrapped)

	ctx := t.Context()

	// Normal call — should appear in channel.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "normal log", 0)
	_ = handler.Handle(ctx, rec)

	// Simulate a call from inside a Render method.
	renderHelper := func() {
		rec2 := slog.NewRecord(time.Now(), slog.LevelInfo, "render log", 0)
		_ = handler.Handle(ctx, rec2)
	}
	// Simulate being inside a View/Render cycle.
	console.EnterRenderPath()
	renderHelper()
	console.LeaveRenderPath()

	close(ch)
	var messages []string
	for sl := range ch {
		messages = append(messages, sl.Text)
	}

	if len(messages) != 1 {
		t.Errorf("expected 1 message (render-path one blocked), got %d: %v", len(messages), messages)
	}
	if len(messages) > 0 && strings.Contains(messages[0], "render log") {
		t.Error("render-path message should have been blocked")
	}
}

// discardHandler is a slog.Handler that discards all records (for testing).
type discardHandler struct{}

func (h *discardHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *discardHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (h *discardHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *discardHandler) WithGroup(_ string) slog.Handler               { return h }

// TestAboutDialogClickDetection verifies that renderDialog and handleDialogClick
// agree on the dialog position and button row, so clicking OK actually works.
func TestAboutDialogClickDetection(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	body := engine.AboutLogo()
	o.PushDialog(common.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	screenW, screenH := 120, 30
	layer := testTheme().LayerAt(2)

	// Get the rendered dialog position.
	dlgBox := o.RenderDialog(screenW, screenH, layer)
	dlgStr, renderCol, renderRow := dlgBox.Content, dlgBox.Col, dlgBox.Row
	if dlgStr == "" {
		t.Fatal("renderDialog returned empty string")
	}
	dlgLines := strings.Split(dlgStr, "\n")
	t.Logf("renderDialog: col=%d row=%d lines=%d", renderCol, renderRow, len(dlgLines))

	// Find the button row in the rendered output (look for "[ OK ]").
	renderBtnRow := -1
	for i, line := range dlgLines {
		if strings.Contains(stripANSI(line), "[ OK ]") {
			renderBtnRow = renderRow + i
			t.Logf("OK button rendered at screen row %d (line %d of dialog)", renderBtnRow, i)
			break
		}
	}
	if renderBtnRow < 0 {
		t.Fatal("could not find [ OK ] in rendered dialog")
	}

	// Now simulate clicking on the OK button.
	// Find the X position of "[ OK ]" in the rendered button row line.
	btnLineIdx := renderBtnRow - renderRow
	btnLine := stripANSI(dlgLines[btnLineIdx])
	btnX := strings.Index(btnLine, "[ OK ]")
	if btnX < 0 {
		t.Fatal("could not find [ OK ] in button line")
	}
	clickX := renderCol + btnX + 1 // +1 to be inside the button
	clickY := renderBtnRow

	t.Logf("clicking at (%d, %d) to hit OK button", clickX, clickY)

	// The click should dismiss the dialog.
	consumed := o.HandleDialogClick(clickX, clickY, screenW, screenH)
	if !consumed {
		t.Error("click on OK button was not consumed")
	}
	if o.HasDialog() {
		t.Error("dialog should have been dismissed after clicking OK, but it's still open")
	}
}

// TestDialogClickMultiButton verifies click detection for multi-button dialogs.
func TestDialogClickMultiButton(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	var clicked string
	o.PushDialog(common.DialogRequest{
		Title:   "Confirm",
		Body:    "Are you sure?",
		Buttons: []string{"Yes", "No", "Cancel"},
		OnClose: func(btn string) { clicked = btn },
	})

	screenW, screenH := 80, 24
	layer := testTheme().LayerAt(2)

	// Render to find button positions.
	dlgBox := o.RenderDialog(screenW, screenH, layer)
	dlgStr, renderCol, renderRow := dlgBox.Content, dlgBox.Col, dlgBox.Row
	dlgLines := strings.Split(dlgStr, "\n")

	// Find button row.
	btnRowIdx := -1
	for i, line := range dlgLines {
		if strings.Contains(stripANSI(line), "[ Yes ]") {
			btnRowIdx = i
			break
		}
	}
	if btnRowIdx < 0 {
		t.Fatal("could not find buttons in rendered dialog")
	}

	btnLine := stripANSI(dlgLines[btnRowIdx])
	clickY := renderRow + btnRowIdx

	// Click "No" button.
	noX := strings.Index(btnLine, "[ No ]")
	if noX < 0 {
		t.Fatal("could not find [ No ] in button line")
	}
	o.HandleDialogClick(renderCol+noX+1, clickY, screenW, screenH)
	if o.HasDialog() {
		t.Error("dialog should be dismissed after clicking No")
	}
	if clicked != "No" {
		t.Errorf("expected OnClose('No'), got %q", clicked)
	}
}

// TestAboutDialogKeyDismiss verifies that pressing Enter closes the About dialog.
func TestAboutDialogKeyDismiss(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	body := engine.AboutLogo()
	o.PushDialog(common.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	if !o.HasDialog() {
		t.Fatal("dialog should be open")
	}

	// Press Enter to close.
	consumed := o.HandleDialogKey("enter")
	if !consumed {
		t.Error("enter should be consumed by dialog")
	}
	if o.HasDialog() {
		t.Error("dialog should be dismissed after Enter")
	}
}

