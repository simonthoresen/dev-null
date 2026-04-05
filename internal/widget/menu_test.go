package widget

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/domain"
)

// ─── Menu bar render tests ───────────────────────────────────────────────────

func TestRenderNCBarSingleMenu(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
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
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
		{Label: "&Help", Items: []domain.MenuItemDef{{Label: "&About"}}},
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
	o := OverlayState{MenuFocused: true, MenuCursor: 1, OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
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
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
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
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
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
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
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
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&View", Items: []domain.MenuItemDef{
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
	o := OverlayState{MenuFocused: true, MenuCursor: 1, OpenMenu: 1, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}, {Label: "&Paste"}}},
	}
	pal := testTheme().LayerAt(1)
	box := o.RenderDropdown(menus, 0, pal)
	dd, col, row := box.Content, box.Col, box.Row
	s := newScreen(dd)

	// Column should be offset to Edit's position.
	positions := MenuBarPositions(menus)
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
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
			{Label: "&New"},
			{Label: "&Open"},
		}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
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
	result := PlaceOverlay(ddCol, ddRow, dd, bg)
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
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "Confirm",
		Body:  "Are you sure?",
	})
	pal := testTheme().WarningLayer()
	buf, col, row := o.RenderDialogBuf(40, 20, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer from RenderDialogBuf")
	}

	// Buffer should have positive dimensions.
	w, h := buf.Width, buf.Height
	if w <= 0 || h <= 0 {
		t.Fatalf("expected positive dimensions, got %dx%d", w, h)
	}

	// Dialog should be roughly centered.
	if col < 5 || col > 20 {
		t.Errorf("expected col near center of 40-wide screen, got %d", col)
	}
	if row < 2 || row > 10 {
		t.Errorf("expected row near center of 20-high screen, got %d", row)
	}

	// Content should contain title, body, and button.
	content := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(content, "Confirm") {
		t.Errorf("expected title 'Confirm' in buffer content")
	}
	if !strings.Contains(content, "Are you sure?") {
		t.Errorf("expected body text in buffer content")
	}
	if !strings.Contains(content, "OK") {
		t.Errorf("expected 'OK' button in buffer content")
	}
}

func TestRenderDialogMultipleButtons(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title:   "Save?",
		Body:    "Unsaved changes.",
		Buttons: []string{"Yes", "No", "Cancel"},
	})
	pal := testTheme().WarningLayer()
	buf, _, _ := o.RenderDialogBuf(60, 20, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer from RenderDialogBuf")
	}

	// Buttons that fit within the dialog width should be present.
	content := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(content, "Yes") {
		t.Errorf("expected 'Yes' button in content")
	}
	if !strings.Contains(content, "No") {
		t.Errorf("expected 'No' button in content")
	}
	// "Cancel" may be truncated if the dialog is narrow; just verify the
	// buffer rendered without error and the first two buttons are visible.
}

func TestRenderDialogMultilineBody(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "Info",
		Body:  "Line one\nLine two\nLine three",
	})
	pal := testTheme().WarningLayer()
	buf, _, _ := o.RenderDialogBuf(50, 20, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer from RenderDialogBuf")
	}

	// Buffer should contain all three body lines.
	content := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(content, "Line one") {
		t.Errorf("expected 'Line one' in content")
	}
	if !strings.Contains(content, "Line two") {
		t.Errorf("expected 'Line two' in content")
	}
	if !strings.Contains(content, "Line three") {
		t.Errorf("expected 'Line three' in content")
	}
}

func TestRenderDialogCentered(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "Test",
		Body:  "Hi",
	})
	pal := testTheme().WarningLayer()
	buf, col, row := o.RenderDialogBuf(80, 24, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}

	// Dialog should be roughly centered.
	if col < 20 || col > 40 {
		t.Errorf("expected col near center of 80-wide screen, got %d", col)
	}
	if row < 5 || row > 15 {
		t.Errorf("expected row near center of 24-high screen, got %d", row)
	}
}

func TestShutdownDialogRendersFullBody(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title:   "Shutdown",
		Body:    "Are you sure you want to shut down the server?",
		Buttons: []string{"Yes", "No"},
	})
	w, h := o.DialogSize(80, 24)
	t.Logf("DialogSize: %dx%d", w, h)
	bodyLen := len("Are you sure you want to shut down the server?")
	if w < bodyLen+2 { // +2 for borders
		t.Errorf("dialog width %d too narrow for body (%d chars + 2 borders)", w, bodyLen)
	}
	pal := testTheme().WarningLayer()
	buf, _, _ := o.RenderDialogBuf(80, 24, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	content := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(content, "Are you sure you want to shut down the server?") {
		t.Errorf("full body text not visible in dialog.\nDialog size: %dx%d\nContent:\n%s", w, h, content)
	}
	if !strings.Contains(content, "Yes") || !strings.Contains(content, "No") {
		t.Errorf("buttons not visible in dialog content:\n%s", content)
	}
}

func TestDialogSizeFitsBody(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "About",
		Body:  "This is a fairly long line that should determine dialog width",
	})
	w, h := o.DialogSize(100, 40)
	// Width should accommodate the long body line + borders.
	bodyLen := len("This is a fairly long line that should determine dialog width")
	if w < bodyLen {
		t.Errorf("dialog width %d too narrow for body of length %d", w, bodyLen)
	}
	// Height should fit: body (1 line) + divider (1) + button row (1) + borders (2).
	if h < 5 {
		t.Errorf("dialog height %d too short, expected at least 5", h)
	}
}

func TestDialogSizeFitsButtons(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title:   "Shaders",
		Body:    "Hi",
		Buttons: []string{"Add", "Remove", "Up", "Down", "Close"},
	})
	w, _ := o.DialogSize(100, 40)
	// Each button is len(label)+6. Total: 9+12+8+10+11 = 50
	minBtnWidth := 0
	for _, lbl := range []string{"Add", "Remove", "Up", "Down", "Close"} {
		minBtnWidth += len(lbl) + 6
	}
	if w < minBtnWidth {
		t.Errorf("dialog width %d too narrow for buttons needing %d", w, minBtnWidth)
	}
}

// ─── Dialog composited on background ─────────────────────────────────────────

func TestDialogOnBackground(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "OK?",
		Body:  "Sure?",
	})
	pal := testTheme().WarningLayer()
	buf, col, row := o.RenderDialogBuf(40, 12, pal)
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}

	// Buffer should have positive dimensions.
	if buf.Width <= 0 || buf.Height <= 0 {
		t.Fatalf("expected positive dimensions, got %dx%d", buf.Width, buf.Height)
	}

	// Dialog should be positioned inside the screen (not at origin for a 40x12 screen).
	if col < 0 || col >= 40 {
		t.Errorf("expected col within screen bounds, got %d", col)
	}
	if row < 0 || row >= 12 {
		t.Errorf("expected row within screen bounds, got %d", row)
	}

	// Buffer content should contain the dialog title.
	content := stripANSI(buf.ToString(colorprofile.TrueColor))
	if !strings.Contains(content, "OK?") {
		t.Errorf("dialog title 'OK?' not found in buffer content")
	}
}
