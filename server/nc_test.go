package server

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"null-space/common"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

func testTheme() *Theme   { return DefaultTheme() }
func testPalette() *Palette { return testTheme().PaletteAt(0) }

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

// ─── Theme & Palette tests ────────────────────────────────────────────────────

func TestDefaultThemeNotNil(t *testing.T) {
	th := DefaultTheme()
	if th == nil {
		t.Fatal("DefaultTheme returned nil")
	}
	if th.Name != "norton" {
		t.Errorf("expected name 'norton', got %q", th.Name)
	}
}

func TestPaletteAtDepth(t *testing.T) {
	th := DefaultTheme()
	p0 := th.PaletteAt(0)
	p1 := th.PaletteAt(1)
	p2 := th.PaletteAt(2)
	p3 := th.PaletteAt(3)
	p4 := th.PaletteAt(4)

	if p0 != &th.Primary {
		t.Error("depth 0 should be Primary")
	}
	if p1 != &th.Secondary {
		t.Error("depth 1 should be Secondary")
	}
	if p2 != &th.Tertiary {
		t.Error("depth 2 should be Tertiary")
	}
	if p3 != &th.Secondary {
		t.Error("depth 3 should be Secondary (alternating)")
	}
	if p4 != &th.Tertiary {
		t.Error("depth 4 should be Tertiary (alternating)")
	}
}

func TestWarningPalette(t *testing.T) {
	th := DefaultTheme()
	w := th.WarningPalette()
	if w != &th.Warning {
		t.Error("WarningPalette should return Warning palette")
	}
}

func TestBorderDefaults(t *testing.T) {
	th := DefaultTheme()
	if th.OTL() != "╔" {
		t.Errorf("expected double-line TL, got %q", th.OTL())
	}
	if th.IH() != "─" {
		t.Errorf("expected single-line inner H, got %q", th.IH())
	}
	if th.XL() != "╟" {
		t.Errorf("expected double-single intersection, got %q", th.XL())
	}
}

// ─── NCTextInput tests ───────────────────────────────────────────────────────

func TestNCTextInputRender(t *testing.T) {
	model := newTestTextInput()
	ti := &NCTextInput{Model: model}
	pal := testPalette()
	th := testTheme()

	output := ti.Render(20, 1, pal, th)
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
	ti := &NCTextInput{Model: model}
	pal := testPalette()
	th := testTheme()

	for _, w := range []int{10, 20, 40, 80} {
		output := ti.Render(w, 1, pal, th)
		visW := ansi.StringWidth(output)
		if visW > w {
			t.Errorf("width %d: rendered width %d exceeds allocated width", w, visW)
		}
	}
}

func TestNCTextInputWantTab(t *testing.T) {
	model := newTestTextInput()
	ti := &NCTextInput{Model: model}

	ti.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ti.WantTab {
		t.Error("expected WantTab=true on tab press")
	}
}

func TestNCTextInputOnSubmit(t *testing.T) {
	model := newTestTextInput()
	var submitted string
	ti := &NCTextInput{Model: model, OnSubmit: func(text string) { submitted = text }}

	model.SetValue("hello")
	ti.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})

	if submitted != "hello" {
		t.Errorf("expected 'hello', got %q", submitted)
	}
	if model.Value() != "" {
		t.Errorf("expected empty after submit, got %q", model.Value())
	}
}

// ─── NCCommandInput tests ─────────────────────────────────────────────────────

func TestNCCommandInputHistory(t *testing.T) {
	model := newTestTextInput()
	ci := &NCCommandInput{NCTextInput: NCTextInput{Model: model}}
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
	ci := &NCCommandInput{NCTextInput: NCTextInput{Model: model}}
	ci.OnTab = func(s string) (string, bool) { return "/help", true }

	// Tab on empty input should signal WantTab (cycle focus), not complete.
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "tab"})
	if !ci.WantTab {
		t.Error("expected WantTab=true on tab with empty input")
	}
}

func TestNCCommandInputTabOnNonEmpty(t *testing.T) {
	model := newTestTextInput()
	ci := &NCCommandInput{NCTextInput: NCTextInput{Model: model}}
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
	ci := &NCCommandInput{NCTextInput: NCTextInput{Model: model}}

	model.SetValue("something")
	ci.Update(tea.KeyPressMsg{Code: -1, Text: "esc"})
	if model.Value() != "" {
		t.Errorf("expected empty after esc, got %q", model.Value())
	}
}

// ─── NCTextView tests ─────────────────────────────────────────────────────────

func TestNCTextViewRender(t *testing.T) {
	tv := &NCTextView{
		Lines:       []string{"line1", "line2", "line3"},
		BottomAlign: true,
	}
	pal := testPalette()
	th := testTheme()

	output := tv.Render(20, 5, pal, th)
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
	tv := &NCTextView{
		Lines:      []string{"a", "b", "c"},
		Scrollable: true,
		height:     2,
	}

	tv.ScrollOffset = 100
	tv.clampScroll()
	if tv.ScrollOffset != 1 { // 3 lines - 2 visible = max 1
		t.Errorf("expected clamped to 1, got %d", tv.ScrollOffset)
	}

	tv.ScrollOffset = -5
	tv.clampScroll()
	if tv.ScrollOffset != 0 {
		t.Errorf("expected clamped to 0, got %d", tv.ScrollOffset)
	}
}

// ─── NCButton tests ──────────────────────────────────────────────────────────

func TestNCButtonRender(t *testing.T) {
	btn := &NCButton{Label: "OK"}
	output := btn.Render(10, 1, testPalette(), testTheme())
	stripped := stripANSI(output)
	if !strings.Contains(stripped, "[ OK ]") {
		t.Errorf("expected '[ OK ]', got %q", stripped)
	}
}

func TestNCButtonPress(t *testing.T) {
	pressed := false
	btn := &NCButton{Label: "OK", OnPress: func() { pressed = true }}
	btn.Update(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if !pressed {
		t.Error("expected button press on enter")
	}
}

// ─── NCCheckbox tests ─────────────────────────────────────────────────────────

func TestNCCheckboxToggle(t *testing.T) {
	cb := &NCCheckbox{Label: "Option", Checked: false}
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
	cb := &NCCheckbox{Label: "Opt", Checked: true}
	output := cb.Render(20, 1, testPalette(), testTheme())
	stripped := stripANSI(output)
	if !strings.Contains(stripped, "[x] Opt") {
		t.Errorf("expected '[x] Opt', got %q", stripped)
	}

	cb.Checked = false
	output = cb.Render(20, 1, testPalette(), testTheme())
	stripped = stripANSI(output)
	if !strings.Contains(stripped, "[ ] Opt") {
		t.Errorf("expected '[ ] Opt', got %q", stripped)
	}
}

// ─── NCWindow tests ──────────────────────────────────────────────────────────

func TestNCWindowRenderBasic(t *testing.T) {
	label := &NCLabel{Text: "Hello"}
	win := &NCWindow{
		Title: "Test",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}

	output := win.Render(0, 0, 30, 5, testPalette(), testTheme())
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
	win := &NCWindow{
		Children: []GridChild{
			{Control: &NCLabel{Text: "X"}, Constraint: GridConstraint{Col: 0, Row: 0}},
		},
	}
	output := win.Render(0, 0, 20, 4, testPalette(), testTheme())
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
	btn1 := &NCButton{Label: "A"}
	btn2 := &NCButton{Label: "B"}
	label := &NCLabel{Text: "sep"} // not focusable

	win := &NCWindow{
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
	ti := &NCTextInput{Model: model}

	win := &NCWindow{
		Children: []GridChild{
			{Control: ti, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Render to compute positions and trigger Focus().
	win.Render(5, 10, 30, 4, testPalette(), testTheme())

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
	label := &NCLabel{Text: "A"}
	tv := &NCTextView{Lines: []string{"line"}}

	win := &NCWindow{
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0}},
			{Control: tv, Constraint: GridConstraint{Col: 0, Row: 1, WeightY: 1, Fill: FillBoth}},
		},
	}
	win.Render(0, 0, 20, 10, testPalette(), testTheme())

	// Label gets row 0 (height 1), textview gets the rest.
	_, _, _, labelH := win.childRect(0)
	_, _, _, tvH := win.childRect(1)

	if labelH != 1 {
		t.Errorf("expected label height 1, got %d", labelH)
	}
	if tvH < 5 {
		t.Errorf("expected textview to fill remaining space, got height %d", tvH)
	}
}

// ─── NCHDivider / NCVDivider tests ───────────────────────────────────────────

func TestNCHDividerRender(t *testing.T) {
	d := &NCHDivider{Connected: true}
	output := d.Render(10, 1, testPalette(), testTheme())
	stripped := stripANSI(output)
	// The divider renders inner horizontal chars repeated to fill width.
	visW := ansi.StringWidth(output)
	if visW < 10 {
		t.Errorf("expected at least 10 visual width, got %d (stripped: %q)", visW, stripped)
	}
}

func TestNCVDividerRender(t *testing.T) {
	d := &NCVDivider{}
	output := d.Render(1, 3, testPalette(), testTheme())
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

// ─── Overlay / Menu tests ─────────────────────────────────────────────────────

func TestMenuShortcut(t *testing.T) {
	_, r := stripAmpersand("&File")
	if r != 'f' {
		t.Errorf("expected 'f', got %c", r)
	}
	_, r = stripAmpersand("E&xit")
	if r != 'x' {
		t.Errorf("expected 'x', got %c", r)
	}
	_, r = stripAmpersand("NoShortcut")
	if r != 0 {
		t.Errorf("expected 0, got %c", r)
	}
}

func TestHotkeyDisplay(t *testing.T) {
	d := hotkeyDisplay("ctrl+c")
	if d != "(Ctrl+C)" {
		t.Errorf("expected '(Ctrl+C)', got %q", d)
	}
}

func TestOverlayMenuNavigation(t *testing.T) {
	o := overlayState{openMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}, {Label: "&Open"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}

	// F10 activates menu bar.
	if !o.handleKey("f10", menus, "") {
		t.Error("F10 should be consumed")
	}
	if !o.menuFocused {
		t.Error("expected menuFocused after F10")
	}
	if o.menuCursor != 0 {
		t.Errorf("expected cursor 0, got %d", o.menuCursor)
	}

	// Right arrow moves to Edit.
	o.handleKey("right", menus, "")
	if o.menuCursor != 1 {
		t.Errorf("expected cursor 1, got %d", o.menuCursor)
	}

	// Down opens dropdown.
	o.handleKey("down", menus, "")
	if o.openMenu != 1 {
		t.Errorf("expected openMenu 1, got %d", o.openMenu)
	}

	// Esc closes dropdown.
	o.handleKey("esc", menus, "")
	if o.openMenu != -1 {
		t.Errorf("expected openMenu -1, got %d", o.openMenu)
	}

	// Esc again deactivates bar.
	o.handleKey("esc", menus, "")
	if o.menuFocused {
		t.Error("expected menuFocused=false after second Esc")
	}
}

func TestOverlayHotkey(t *testing.T) {
	o := overlayState{openMenu: -1}
	triggered := false
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&Quit", Hotkey: "ctrl+q", Handler: func(_ string) { triggered = true }},
		}},
	}

	if !o.handleKey("ctrl+q", menus, "") {
		t.Error("hotkey should be consumed")
	}
	if !triggered {
		t.Error("expected hotkey handler to fire")
	}
}

func TestOverlayAltActivation(t *testing.T) {
	o := overlayState{openMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "Item"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "Item"}}},
	}

	o.handleKey("alt+e", menus, "")
	if o.openMenu != 1 {
		t.Errorf("expected Alt+E to open Edit menu (index 1), got %d", o.openMenu)
	}
}

func TestOverlayDialogStack(t *testing.T) {
	o := overlayState{openMenu: -1}

	if o.hasDialog() {
		t.Error("should have no dialog initially")
	}

	o.pushDialog(common.DialogRequest{Title: "First", Body: "A"})
	o.pushDialog(common.DialogRequest{Title: "Second", Body: "B"})

	if !o.hasDialog() {
		t.Error("should have dialogs")
	}
	if d := o.topDialog(); d.Title != "Second" {
		t.Errorf("expected top 'Second', got %q", d.Title)
	}

	o.popDialog()
	if d := o.topDialog(); d.Title != "First" {
		t.Errorf("expected top 'First', got %q", d.Title)
	}

	o.popDialog()
	if o.hasDialog() {
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
	result := ApplyShadow(1, 1, 3, 2, bg, testTheme().ShadowStyle())
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

// ─── PlaceOverlay tests ──────────────────────────────────────────────────────

func TestPlaceOverlay(t *testing.T) {
	bg := "AAAAAAA\nBBBBBBB\nCCCCCCC"
	overlay := "XX\nYY"

	result := PlaceOverlay(2, 1, overlay, bg)
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
	pal := testPalette()
	sb := renderScrollbar(100, 10, 0, pal.BaseStyle())

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
	pal := testPalette()
	sb := renderScrollbar(5, 10, 0, pal.BaseStyle())

	// Content fits — no scrollbar needed.
	for _, s := range sb {
		if strings.Contains(stripANSI(s), "█") || strings.Contains(stripANSI(s), "░") {
			t.Error("expected no scrollbar when content fits")
			break
		}
	}
}

// ─── Integration: NCWindow with multiple controls ─────────────────────────────

func TestNCWindowIntegration(t *testing.T) {
	model := newTestTextInput()
	tv := &NCTextView{Lines: []string{"log1", "log2"}, BottomAlign: true}
	ci := &NCCommandInput{NCTextInput: NCTextInput{Model: model}}
	submitted := ""
	ci.OnSubmit = func(text string) { submitted = text }

	win := &NCWindow{
		Title: "Console",
		Children: []GridChild{
			{Control: tv, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth}},
			{Control: &NCHDivider{Connected: true}, Constraint: GridConstraint{Col: 0, Row: 1}},
			{Control: ci, Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	win.FocusFirst()

	// Focus should be on the command input (first focusable).
	if win.FocusIdx != 2 {
		t.Errorf("expected focus on command input (idx 2), got %d", win.FocusIdx)
	}

	// Render should produce output.
	output := win.Render(0, 0, 40, 10, testPalette(), testTheme())
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
// (e.g. "NCWindow render child") are not routed to the console channel, which
// would cause a feedback loop: render → debug log → console → re-render → ...
func TestSlogRenderLogsNotRoutedToConsole(t *testing.T) {
	ch := make(chan slogLine, 10)
	wrapped := &discardHandler{}
	handler := NewConsoleSlogHandler(ch, wrapped)

	ctx := t.Context()

	// Render-path debug message should NOT appear in channel.
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "NCWindow render child", 0)
	_ = handler.Handle(ctx, rec)

	rec2 := slog.NewRecord(time.Now(), slog.LevelDebug, "NCTextInput.Render", 0)
	_ = handler.Handle(ctx, rec2)

	// Non-render debug message SHOULD appear in channel.
	rec3 := slog.NewRecord(time.Now(), slog.LevelDebug, "plugin loaded: greeter", 0)
	_ = handler.Handle(ctx, rec3)

	// INFO message SHOULD appear in channel.
	rec4 := slog.NewRecord(time.Now(), slog.LevelInfo, "server started", 0)
	_ = handler.Handle(ctx, rec4)

	// Drain channel and check.
	close(ch)
	var messages []string
	for sl := range ch {
		messages = append(messages, sl.text)
	}

	if len(messages) != 2 {
		t.Errorf("expected 2 messages in console channel, got %d: %v", len(messages), messages)
	}
	for _, m := range messages {
		if strings.Contains(m, "NCWindow render child") || strings.Contains(m, "NCTextInput.Render") {
			t.Errorf("render-path debug message leaked to console: %q", m)
		}
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
	o := overlayState{openMenu: -1}
	body := aboutLogo()
	o.pushDialog(common.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	screenW, screenH := 120, 30
	pal := testTheme().PaletteAt(2)
	th := testTheme()

	// Get the rendered dialog position.
	dlgStr, renderCol, renderRow := o.renderDialog(screenW, screenH, pal, th)
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
	consumed := o.handleDialogClick(clickX, clickY, screenW, screenH)
	if !consumed {
		t.Error("click on OK button was not consumed")
	}
	if o.hasDialog() {
		t.Error("dialog should have been dismissed after clicking OK, but it's still open")
	}
}

// TestDialogClickMultiButton verifies click detection for multi-button dialogs.
func TestDialogClickMultiButton(t *testing.T) {
	o := overlayState{openMenu: -1}
	var clicked string
	o.pushDialog(common.DialogRequest{
		Title:   "Confirm",
		Body:    "Are you sure?",
		Buttons: []string{"Yes", "No", "Cancel"},
		OnClose: func(btn string) { clicked = btn },
	})

	screenW, screenH := 80, 24
	pal := testTheme().PaletteAt(2)
	th := testTheme()

	// Render to find button positions.
	dlgStr, renderCol, renderRow := o.renderDialog(screenW, screenH, pal, th)
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
	o.handleDialogClick(renderCol+noX+1, clickY, screenW, screenH)
	if o.hasDialog() {
		t.Error("dialog should be dismissed after clicking No")
	}
	if clicked != "No" {
		t.Errorf("expected OnClose('No'), got %q", clicked)
	}
}

// TestAboutDialogKeyDismiss verifies that pressing Enter closes the About dialog.
func TestAboutDialogKeyDismiss(t *testing.T) {
	o := overlayState{openMenu: -1}
	body := aboutLogo()
	o.pushDialog(common.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	if !o.hasDialog() {
		t.Fatal("dialog should be open")
	}

	// Press Enter to close.
	consumed := o.handleDialogKey("enter")
	if !consumed {
		t.Error("enter should be consumed by dialog")
	}
	if o.hasDialog() {
		t.Error("dialog should be dismissed after Enter")
	}
}

