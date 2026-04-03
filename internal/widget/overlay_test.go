package widget

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"null-space/internal/domain"
	"null-space/internal/engine"
)

// ─── Overlay / Menu tests ────────────────────────────────────────────────────

func TestMenuShortcut(t *testing.T) {
	_, r := StripAmpersand("&File")
	if r != 'f' {
		t.Errorf("expected 'f', got %c", r)
	}
	_, r = StripAmpersand("E&xit")
	if r != 'x' {
		t.Errorf("expected 'x', got %c", r)
	}
	_, r = StripAmpersand("NoShortcut")
	if r != 0 {
		t.Errorf("expected 0, got %c", r)
	}
}

func TestHotkeyDisplayFunc(t *testing.T) {
	d := HotkeyDisplay("ctrl+c")
	if d != "(Ctrl+C)" {
		t.Errorf("expected '(Ctrl+C)', got %q", d)
	}
}

func TestOverlayMenuNavigation(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}, {Label: "&Open"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
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
	o := OverlayState{OpenMenu: -1}
	triggered := false
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
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
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "Item"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "Item"}}},
	}

	o.HandleKey("alt+e", menus, "")
	if o.OpenMenu != 1 {
		t.Errorf("expected Alt+E to open Edit menu (index 1), got %d", o.OpenMenu)
	}
}

func TestOverlayDialogStack(t *testing.T) {
	o := OverlayState{OpenMenu: -1}

	if o.HasDialog() {
		t.Error("should have no dialog initially")
	}

	o.PushDialog(domain.DialogRequest{Title: "First", Body: "A"})
	o.PushDialog(domain.DialogRequest{Title: "Second", Body: "B"})

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

// ─── Shadow tests ────────────────────────────────────────────────────────────

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
	pal := testLayer()
	sb := RenderScrollbar(100, 10, 0, pal.BaseStyle())

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
	sb := RenderScrollbar(5, 10, 0, pal.BaseStyle())

	// Content fits — no scrollbar needed.
	for _, s := range sb {
		if strings.Contains(stripANSI(s), "█") || strings.Contains(stripANSI(s), "░") {
			t.Error("expected no scrollbar when content fits")
			break
		}
	}
}

// ─── About dialog click detection ────────────────────────────────────────────

func TestAboutDialogClickDetection(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	body := engine.AboutLogo()
	o.PushDialog(domain.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	screenW, screenH := 120, 30
	layer := testTheme().LayerAt(2)

	// Get the rendered dialog position.
	buf, renderCol, renderRow := o.RenderDialogBuf(screenW, screenH, layer)
	if buf == nil {
		t.Fatal("RenderDialogBuf returned nil buffer")
	}

	// Click inside dialog bounds should be consumed (modal).
	clickX := renderCol + buf.Width/2
	clickY := renderRow + buf.Height/2
	consumed := o.HandleDialogClick(clickX, clickY, screenW, screenH)
	if !consumed {
		t.Error("click inside dialog bounds was not consumed")
	}

	// Click outside dialog bounds should also be consumed (modal).
	consumed = o.HandleDialogClick(0, 0, screenW, screenH)
	if !consumed {
		t.Error("click outside dialog bounds was not consumed (modal)")
	}

	// Dialog can be dismissed via Enter key on the focused OK button.
	o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if o.HasDialog() {
		t.Error("dialog should have been dismissed after Enter")
	}
}

func TestDialogClickMultiButton(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	var clicked string
	o.PushDialog(domain.DialogRequest{
		Title:   "Confirm",
		Body:    "Are you sure?",
		Buttons: []string{"Yes", "No", "Cancel"},
		OnClose: func(btn string) { clicked = btn },
	})

	screenW, screenH := 80, 24
	layer := testTheme().LayerAt(2)

	// Render to verify dialog is present.
	buf, _, _ := o.RenderDialogBuf(screenW, screenH, layer)
	if buf == nil {
		t.Fatal("RenderDialogBuf returned nil buffer")
	}

	// Click inside dialog is consumed (modal behavior).
	consumed := o.HandleDialogClick(40, 12, screenW, screenH)
	if !consumed {
		t.Error("click inside dialog should be consumed")
	}

	// Use keyboard to navigate to "No" and press it.
	// First button (Yes) is focused by default; Tab moves to No.
	o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "tab"})
	o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if o.HasDialog() {
		t.Error("dialog should be dismissed after pressing No")
	}
	if clicked != "No" {
		t.Errorf("expected OnClose('No'), got %q", clicked)
	}
}

func TestAboutDialogKeyDismiss(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	body := engine.AboutLogo()
	o.PushDialog(domain.DialogRequest{
		Title:   "About",
		Body:    body,
		Buttons: []string{"OK"},
	})

	if !o.HasDialog() {
		t.Fatal("dialog should be open")
	}

	// Press Enter to close (activates the focused OK button).
	consumed, _ := o.HandleDialogMsg(tea.KeyPressMsg{Code: -1, Text: "enter"})
	if !consumed {
		t.Error("enter should be consumed by dialog")
	}
	if o.HasDialog() {
		t.Error("dialog should be dismissed after Enter")
	}
}
