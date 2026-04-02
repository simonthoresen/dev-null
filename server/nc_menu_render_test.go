package server

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"null-space/common"
	"null-space/internal/widget"
)

// ─── Menu bar render tests ───────────────────────────────────────────────────

func TestRenderNCBarSingleMenu(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
	}
	pal := testTheme().LayerAt(1)
	output := o.RenderMenuBar(20, menus, pal)
	s := newScreen(output)

	// Bar should be exactly 1 line.
	if s.h != 1 {
		t.Fatalf("expected 1 line, got %d", s.h)
	}

	// Should contain the menu label (without &).
	if !strings.Contains(s.lines[0], "File") {
		t.Errorf("expected 'File' in bar, got %q", s.lines[0])
	}

	// Should fill to the requested width.
	if s.w != 20 {
		t.Errorf("expected width 20, got %d", s.w)
	}
}

func TestRenderNCBarMultipleMenus(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
		{Label: "&Help", Items: []common.MenuItemDef{{Label: "&About"}}},
	}
	pal := testTheme().LayerAt(1)
	output := o.RenderMenuBar(40, menus, pal)

	s := newScreen(output)
	// All menu labels should appear, separated by │.
	if !strings.Contains(s.lines[0], "File") || !strings.Contains(s.lines[0], "Edit") || !strings.Contains(s.lines[0], "Help") {
		t.Errorf("expected all menu labels in bar, got %q", s.lines[0])
	}
	if !strings.Contains(s.lines[0], "│") {
		t.Errorf("expected separator │ in bar, got %q", s.lines[0])
	}
	if s.w != 40 {
		t.Errorf("expected width 40, got %d", s.w)
	}
}

func TestRenderNCBarFocusedMenu(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 1, OpenMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}
	pal := testTheme().LayerAt(1)
	output := o.RenderMenuBar(30, menus, pal)
	s := newScreen(output)

	// Both labels should still be present.
	if !strings.Contains(s.lines[0], "File") {
		t.Errorf("expected 'File' in bar, got %q", s.lines[0])
	}
	if !strings.Contains(s.lines[0], "Edit") {
		t.Errorf("expected 'Edit' in bar, got %q", s.lines[0])
	}
}

// ─── Dropdown render tests ───────────────────────────────────────────────────

func TestRenderDropdownBasic(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&New"},
			{Label: "&Open"},
			{Label: "&Save"},
		}},
	}
	pal := testTheme().LayerAt(1)
	box := o.RenderDropdown(menus, 0, pal)
	dd, col, row := box.Content, box.Col, box.Row

	if col != 0 {
		t.Errorf("expected col 0, got %d", col)
	}
	if row != 1 {
		t.Errorf("expected row 1, got %d", row)
	}

	s := newScreen(dd)
	// Should have 5 lines: top border + 3 items + bottom border.
	if s.h != 5 {
		t.Fatalf("expected 5 lines, got %d\n%s", s.h, s.String())
	}

	// Top and bottom borders.
	if !strings.HasPrefix(s.lines[0], "╔") || !strings.HasSuffix(s.lines[0], "╗") {
		t.Errorf("expected top border, got %q", s.lines[0])
	}
	if !strings.HasPrefix(s.lines[4], "╚") || !strings.HasSuffix(s.lines[4], "╝") {
		t.Errorf("expected bottom border, got %q", s.lines[4])
	}

	// Item rows should have side borders.
	for i := 1; i <= 3; i++ {
		if !strings.HasPrefix(s.lines[i], "║") || !strings.HasSuffix(s.lines[i], "║") {
			t.Errorf("line %d: expected side borders, got %q", i, s.lines[i])
		}
	}

	// Items should contain their labels.
	if !strings.Contains(s.lines[1], "New") {
		t.Errorf("expected 'New' in line 1, got %q", s.lines[1])
	}
	if !strings.Contains(s.lines[2], "Open") {
		t.Errorf("expected 'Open' in line 2, got %q", s.lines[2])
	}
	if !strings.Contains(s.lines[3], "Save") {
		t.Errorf("expected 'Save' in line 3, got %q", s.lines[3])
	}
}

func TestRenderDropdownWithSeparator(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&New"},
			{Label: "---"}, // separator
			{Label: "&Quit"},
		}},
	}
	pal := testTheme().LayerAt(1)
	dd := o.RenderDropdown(menus, 0, pal).Content
	s := newScreen(dd)

	// 5 lines: top + New + separator + Quit + bottom.
	if s.h != 5 {
		t.Fatalf("expected 5 lines, got %d\n%s", s.h, s.String())
	}

	// Separator row should use inner horizontal chars.
	if !strings.Contains(s.lines[2], "─") {
		t.Errorf("separator should contain inner-H chars, got %q", s.lines[2])
	}

	if !strings.Contains(s.lines[1], "New") {
		t.Errorf("expected 'New' in line 1, got %q", s.lines[1])
	}
	if !strings.Contains(s.lines[3], "Quit") {
		t.Errorf("expected 'Quit' in line 3, got %q", s.lines[3])
	}
}

func TestRenderDropdownWithHotkey(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&Quit", Hotkey: "ctrl+q"},
		}},
	}
	pal := testTheme().LayerAt(1)
	dd := o.RenderDropdown(menus, 0, pal).Content
	s := newScreen(dd)

	// Should show hotkey display.
	if !strings.Contains(s.lines[1], "(Ctrl+Q)") {
		t.Errorf("expected '(Ctrl+Q)' in item, got %q", s.lines[1])
	}
}

func TestRenderDropdownWithToggles(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&View", Items: []common.MenuItemDef{
			{Label: "&Sidebar", Toggle: true, Checked: func() bool { return true }},
			{Label: "&Toolbar", Toggle: true, Checked: func() bool { return false }},
		}},
	}
	pal := testTheme().LayerAt(1)
	dd := o.RenderDropdown(menus, 0, pal).Content
	s := newScreen(dd)

	// Checked item should have checkmark.
	if !strings.Contains(s.lines[1], "√") {
		t.Errorf("expected checkmark for Sidebar, got %q", s.lines[1])
	}
	// Unchecked item should not have checkmark.
	if strings.Contains(s.lines[2], "√") {
		t.Errorf("expected no checkmark for Toolbar, got %q", s.lines[2])
	}

	// Both labels present.
	if !strings.Contains(s.lines[1], "Sidebar") {
		t.Errorf("expected 'Sidebar', got %q", s.lines[1])
	}
	if !strings.Contains(s.lines[2], "Toolbar") {
		t.Errorf("expected 'Toolbar', got %q", s.lines[2])
	}
}

func TestRenderDropdownSecondMenu(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 1, OpenMenu: 1, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}, {Label: "&Paste"}}},
	}
	pal := testTheme().LayerAt(1)
	box := o.RenderDropdown(menus, 0, pal)
	dd, col, row := box.Content, box.Col, box.Row
	s := newScreen(dd)

	// Column should be offset to Edit's position.
	positions := widget.MenuBarPositions(menus)
	if col != positions[1] {
		t.Errorf("expected col %d, got %d", positions[1], col)
	}
	if row != 1 {
		t.Errorf("expected row 1, got %d", row)
	}

	// Should have 4 lines: top + 2 items + bottom.
	if s.h != 4 {
		t.Fatalf("expected 4 lines, got %d\n%s", s.h, s.String())
	}

	if !strings.Contains(s.lines[1], "Copy") {
		t.Errorf("expected 'Copy', got %q", s.lines[1])
	}
	if !strings.Contains(s.lines[2], "Paste") {
		t.Errorf("expected 'Paste', got %q", s.lines[2])
	}
}

// ─── Dropdown composited on bar ──────────────────────────────────────────────

func TestDropdownOnBar(t *testing.T) {
	o := widget.OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&New"},
			{Label: "&Open"},
		}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}
	pal := testTheme().LayerAt(1)

	// Render bar.
	bar := o.RenderMenuBar(40, menus, pal)

	// Render dropdown.
	ddBox := o.RenderDropdown(menus, 0, pal)
	dd, ddCol, ddRow := ddBox.Content, ddBox.Col, ddBox.Row

	// Build a background: bar + empty rows to have space for the dropdown.
	bgLines := []string{ansi.Strip(bar)}
	for range 5 {
		bgLines = append(bgLines, strings.Repeat(" ", 40))
	}
	bg := strings.Join(bgLines, "\n")

	// Composite.
	result := widget.PlaceOverlay(ddCol, ddRow, dd, bg)
	s := newScreen(result)

	// Bar should still be visible on row 0.
	if !strings.Contains(s.lines[0], "File") {
		t.Errorf("bar row should contain 'File', got %q", s.lines[0])
	}

	// Dropdown should appear starting at row 1.
	if !strings.HasPrefix(s.lines[1], "╔") {
		t.Errorf("dropdown top border should start at row 1, got %q", s.lines[1])
	}

	// Items.
	if !strings.Contains(s.lines[2], "New") {
		t.Errorf("expected 'New' at row 2, got %q", s.lines[2])
	}
	if !strings.Contains(s.lines[3], "Open") {
		t.Errorf("expected 'Open' at row 3, got %q", s.lines[3])
	}

	// Bottom border.
	if !strings.HasPrefix(s.lines[4], "╚") {
		t.Errorf("expected bottom border at row 4, got %q", s.lines[4])
	}

	// Area to the right of the dropdown should still be blank.
	ddWidth := len([]rune(newScreen(dd).lines[0]))
	region := newScreen(result).region(ddWidth, 1, 5, 4)
	if strings.TrimSpace(region) != "" {
		t.Errorf("area right of dropdown should be blank, got:\n%s", region)
	}
}

// ─── Dialog render tests ─────────────────────────────────────────────────────

func TestRenderDialogBasic(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	o.PushDialog(common.DialogRequest{
		Title: "Confirm",
		Body:  "Are you sure?",
	})
	pal := testTheme().WarningLayer()
	dlg := o.RenderDialog(40, 20, pal).Content
	s := newScreen(dlg)

	// Top border.
	if !strings.HasPrefix(s.lines[0], "╔") || !strings.HasSuffix(s.lines[0], "╗") {
		t.Errorf("expected top border, got %q", s.lines[0])
	}

	// Title row.
	if !strings.Contains(s.lines[1], "Confirm") {
		t.Errorf("expected title 'Confirm', got %q", s.lines[1])
	}

	// Separator after title.
	if !strings.HasPrefix(s.lines[2], "╟") || !strings.HasSuffix(s.lines[2], "╢") {
		t.Errorf("expected title separator, got %q", s.lines[2])
	}

	// Body.
	if !strings.Contains(s.lines[3], "Are you sure?") {
		t.Errorf("expected body text, got %q", s.lines[3])
	}

	// Separator before buttons.
	if !strings.HasPrefix(s.lines[4], "╟") || !strings.HasSuffix(s.lines[4], "╢") {
		t.Errorf("expected button separator, got %q", s.lines[4])
	}

	// Default OK button.
	if !strings.Contains(s.lines[5], "[ OK ]") {
		t.Errorf("expected '[ OK ]' button, got %q", s.lines[5])
	}

	// Bottom border.
	last := s.lines[s.h-1]
	if !strings.HasPrefix(last, "╚") || !strings.HasSuffix(last, "╝") {
		t.Errorf("expected bottom border, got %q", last)
	}
}

func TestRenderDialogMultipleButtons(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1, DialogFocus: 1}
	o.PushDialog(common.DialogRequest{
		Title:   "Save?",
		Body:    "Unsaved changes.",
		Buttons: []string{"Yes", "No", "Cancel"},
	})
	pal := testTheme().WarningLayer()
	dlg := o.RenderDialog(60, 20, pal).Content
	s := newScreen(dlg)

	// All buttons should be present.
	btnLine := ""
	for _, l := range s.lines {
		if strings.Contains(l, "[ Yes ]") {
			btnLine = l
			break
		}
	}
	if btnLine == "" {
		t.Fatalf("no button line found\n%s", s.String())
	}

	if !strings.Contains(btnLine, "[ Yes ]") {
		t.Errorf("expected '[ Yes ]', got %q", btnLine)
	}
	if !strings.Contains(btnLine, "[ No ]") {
		t.Errorf("expected '[ No ]', got %q", btnLine)
	}
	if !strings.Contains(btnLine, "[ Cancel ]") {
		t.Errorf("expected '[ Cancel ]', got %q", btnLine)
	}
}

func TestRenderDialogMultilineBody(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	o.PushDialog(common.DialogRequest{
		Title: "Info",
		Body:  "Line one\nLine two\nLine three",
	})
	pal := testTheme().WarningLayer()
	dlg := o.RenderDialog(50, 20, pal).Content
	s := newScreen(dlg)

	// Should have: top + title + sep + 3 body + sep + buttons + bottom = 9 lines.
	if s.h != 9 {
		t.Fatalf("expected 9 lines, got %d\n%s", s.h, s.String())
	}

	if !strings.Contains(s.lines[3], "Line one") {
		t.Errorf("body line 1: expected 'Line one', got %q", s.lines[3])
	}
	if !strings.Contains(s.lines[4], "Line two") {
		t.Errorf("body line 2: expected 'Line two', got %q", s.lines[4])
	}
	if !strings.Contains(s.lines[5], "Line three") {
		t.Errorf("body line 3: expected 'Line three', got %q", s.lines[5])
	}
}

func TestRenderDialogCentered(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	o.PushDialog(common.DialogRequest{
		Title: "Test",
		Body:  "Hi",
	})
	pal := testTheme().WarningLayer()
	box := o.RenderDialog(80, 24, pal)
	col, row := box.Col, box.Row

	// Dialog should be roughly centered.
	if col < 20 || col > 40 {
		t.Errorf("expected col near center of 80-wide screen, got %d", col)
	}
	if row < 5 || row > 15 {
		t.Errorf("expected row near center of 24-high screen, got %d", row)
	}
}

// ─── Dialog composited on background ─────────────────────────────────────────

func TestDialogOnBackground(t *testing.T) {
	o := widget.OverlayState{OpenMenu: -1}
	o.PushDialog(common.DialogRequest{
		Title: "OK?",
		Body:  "Sure?",
	})
	pal := testTheme().WarningLayer()
	dlgBox := o.RenderDialog(40, 12, pal)
	dlg, dlgCol, dlgRow := dlgBox.Content, dlgBox.Col, dlgBox.Row

	// Build a dot-filled background.
	var bgLines []string
	for range 12 {
		bgLines = append(bgLines, strings.Repeat(".", 40))
	}
	bg := strings.Join(bgLines, "\n")

	result := widget.PlaceOverlay(dlgCol, dlgRow, dlg, bg)
	s := newScreen(result)

	// Background should show dots outside dialog.
	if !strings.HasPrefix(s.lines[0], "....") {
		t.Errorf("row 0 should be background dots, got %q", s.lines[0])
	}

	// Dialog should be inside the output.
	found := false
	for _, l := range s.lines {
		if strings.Contains(l, "OK?") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dialog title 'OK?' not found in composited output\n%s", s.String())
	}

	// Dots should still be visible to the left and right of the dialog.
	for _, l := range s.lines {
		if strings.Contains(l, "OK?") {
			// Should have dots before the dialog border.
			if dlgCol > 0 && !strings.HasPrefix(l, ".") {
				t.Errorf("expected background dots before dialog, got %q", l)
			}
			break
		}
	}
}

// ─── Full NCWindow render with assertScreen ──────────────────────────────────

func TestNCWindowRenderAssertScreen(t *testing.T) {
	label := &widget.Label{Text: "Hi"}
	win := &widget.Window{
		Title: "Win",
		Children: []widget.GridChild{
			{Control: label, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal}},
		},
	}
	output := win.Render(0, 0, 12, 4, testLayer())
	s := newScreen(output)

	// Verify structure line by line.
	if !strings.Contains(s.lines[0], "Win") {
		t.Errorf("top border should contain title, got %q", s.lines[0])
	}
	if !strings.HasPrefix(s.lines[0], "╔") {
		t.Errorf("expected ╔ start, got %q", s.lines[0])
	}
	if !strings.HasSuffix(s.lines[0], "╗") {
		t.Errorf("expected ╗ end, got %q", s.lines[0])
	}

	// Content row with label.
	if !strings.HasPrefix(s.lines[1], "║") || !strings.HasSuffix(s.lines[1], "║") {
		t.Errorf("content row should have side borders, got %q", s.lines[1])
	}
	if !strings.Contains(s.lines[1], "Hi") {
		t.Errorf("content row should contain 'Hi', got %q", s.lines[1])
	}

	// Bottom border.
	last := s.lines[s.h-1]
	if !strings.HasPrefix(last, "╚") || !strings.HasSuffix(last, "╝") {
		t.Errorf("expected bottom border, got %q", last)
	}
}

// ─── Region extraction from composited views ─────────────────────────────────

func TestRegionExtractionFromNCWindow(t *testing.T) {
	label := &widget.Label{Text: "ABCDE"}
	win := &widget.Window{
		Title: "T",
		Children: []widget.GridChild{
			{Control: label, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal}},
		},
	}
	output := win.Render(0, 0, 14, 4, testLayer())

	// Extract just the content area (inside borders).
	// Border is 1 char each side, so content starts at col 1, row 1.
	// Width is 14-2=12, content rows are rows 1 and 2 (height 4 - top border - bottom border = 2).
	s := newScreen(output)

	// The label "ABCDE" should appear somewhere in the inner region.
	inner := s.region(1, 1, 12, 2)
	if !strings.Contains(inner, "ABCDE") {
		t.Errorf("inner region should contain label 'ABCDE', got:\n%s", inner)
	}
}
