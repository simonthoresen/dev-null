package widget

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"null-space/internal/domain"
	"null-space/internal/theme"
)

// ─── Theme border tests ─────────────────────────────────────────────────────

// singleLineTheme returns a theme that uses single-line box drawing chars.
func singleLineTheme() *theme.Theme {
	th := theme.Default()
	for _, l := range []*theme.Layer{&th.Primary, &th.Secondary, &th.Tertiary, &th.Warning} {
		l.OuterTL = "┌"
		l.OuterTR = "┐"
		l.OuterBL = "└"
		l.OuterBR = "┘"
		l.OuterH = "─"
		l.OuterV = "│"
		l.InnerH = "·"
		l.InnerV = ":"
		l.CrossL = "├"
		l.CrossR = "┤"
		l.BarSep = "|"
	}
	return th
}

// asciiTheme returns a theme with plain ASCII borders.
func asciiTheme() *theme.Theme {
	th := theme.Default()
	for _, l := range []*theme.Layer{&th.Primary, &th.Secondary, &th.Tertiary, &th.Warning} {
		l.OuterTL = "+"
		l.OuterTR = "+"
		l.OuterBL = "+"
		l.OuterBR = "+"
		l.OuterH = "-"
		l.OuterV = "|"
		l.InnerH = "."
		l.InnerV = ":"
		l.CrossL = "+"
		l.CrossR = "+"
		l.BarSep = "|"
	}
	return th
}

func TestNCWindowBordersDefaultTheme(t *testing.T) {
	label := &Label{Text: "X"}
	win := &Window{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := theme.Default()
	output := renderWindow(win,0, 0, 10, 3, th.LayerAt(0))
	s := newScreen(output)

	// Default theme uses double-line borders.
	assertRegion(t, output, 0, 0, 1, 3, `
		╔
		║
		╚`)
	assertRegion(t, output, 9, 0, 1, 3, `
		╗
		║
		╝`)
	if !strings.Contains(s.lines[0], "═") {
		t.Errorf("expected ═ in top border, got %q", s.lines[0])
	}
}

func TestNCWindowBordersSingleLineTheme(t *testing.T) {
	label := &Label{Text: "X"}
	win := &Window{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := singleLineTheme()
	output := renderWindow(win,0, 0, 10, 3, th.LayerAt(0))
	s := newScreen(output)

	assertRegion(t, output, 0, 0, 1, 3, `
		┌
		│
		└`)
	assertRegion(t, output, 9, 0, 1, 3, `
		┐
		│
		┘`)
	if !strings.Contains(s.lines[0], "─") {
		t.Errorf("expected ─ in top border, got %q", s.lines[0])
	}
	if strings.Contains(s.lines[0], "╔") || strings.Contains(s.lines[0], "═") {
		t.Errorf("single-line theme should not have double-line chars, got %q", s.lines[0])
	}
}

func TestNCWindowBordersASCIITheme(t *testing.T) {
	label := &Label{Text: "X"}
	win := &Window{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := asciiTheme()
	output := renderWindow(win,0, 0, 10, 3, th.LayerAt(0))
	s := newScreen(output)

	assertRegion(t, output, 0, 0, 1, 3, `
		+
		|
		+`)
	assertRegion(t, output, 9, 0, 1, 3, `
		+
		|
		+`)
	if !strings.Contains(s.lines[0], "---") {
		t.Errorf("expected --- in top border, got %q", s.lines[0])
	}
}

func TestDropdownBordersSingleLineTheme(t *testing.T) {
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
			{Label: "&New"},
			{Label: "---"},
			{Label: "&Quit"},
		}},
	}
	th := singleLineTheme()
	dd := o.RenderDropdown(menus, 0, th.LayerAt(1)).Content
	s := newScreen(dd)

	// Top/bottom use single-line.
	if !strings.HasPrefix(s.lines[0], "┌") || !strings.HasSuffix(s.lines[0], "┐") {
		t.Errorf("expected single-line top border, got %q", s.lines[0])
	}
	if !strings.HasPrefix(s.lines[1], "│") || !strings.HasSuffix(s.lines[1], "│") {
		t.Errorf("expected single-line side borders, got %q", s.lines[1])
	}
	// Separator uses custom inner-H.
	if !strings.Contains(s.lines[2], "·") {
		t.Errorf("separator should use custom inner-H '·', got %q", s.lines[2])
	}
	if !strings.HasPrefix(s.lines[4], "└") || !strings.HasSuffix(s.lines[4], "┘") {
		t.Errorf("expected single-line bottom border, got %q", s.lines[4])
	}
}

func TestMenuBarSeparatorFromTheme(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
	}

	// Default theme separator is │.
	th := theme.Default()
	output := o.RenderMenuBar(30, menus, th.LayerAt(1))
	s := newScreen(output)
	if !strings.Contains(s.lines[0], "│") {
		t.Errorf("default bar should use │ separator, got %q", s.lines[0])
	}

	// Custom theme separator.
	th2 := singleLineTheme() // uses "|"
	output2 := o.RenderMenuBar(30, menus, th2.LayerAt(1))
	s2 := newScreen(output2)
	if !strings.Contains(s2.lines[0], "|") {
		t.Errorf("custom bar should use | separator, got %q", s2.lines[0])
	}
}

func TestDialogBordersASCIITheme(t *testing.T) {
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{
		Title: "Err",
		Body:  "Oops",
	})
	th := asciiTheme()
	buf, _, _ := o.RenderDialogBuf(40, 20, th.WarningLayer())
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	s := newScreen(buf.ToString(colorprofile.TrueColor))

	if !strings.HasPrefix(s.lines[0], "+") || !strings.HasSuffix(s.lines[0], "+") {
		t.Errorf("expected ASCII top border, got %q", s.lines[0])
	}
	if !strings.Contains(s.lines[0], "---") {
		t.Errorf("expected --- in top border, got %q", s.lines[0])
	}
	// Dialog dividers are non-connected — separator floats inside padding.
	sepRow := 3 // top border + padding + body
	if !strings.Contains(s.lines[sepRow], "...") {
		t.Errorf("expected ... in separator at row %d, got %q", sepRow, s.lines[sepRow])
	}
	if !strings.HasPrefix(s.lines[1], "|") || !strings.HasSuffix(s.lines[1], "|") {
		t.Errorf("expected | side borders, got %q", s.lines[1])
	}
	last := s.lines[s.h-1]
	if !strings.HasPrefix(last, "+") || !strings.HasSuffix(last, "+") {
		t.Errorf("expected ASCII bottom border, got %q", last)
	}
}

// ─── Cross-theme visual consistency ──────────────────────────────────────────

func TestSameLayoutDifferentThemeBorders(t *testing.T) {
	// Render the same window with three themes — only borders should change.
	label := &Label{Text: "Hello"}
	makeWin := func() *Window {
		return &Window{
			Title: "T",
			Children: []GridChild{
				{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
			},
		}
	}

	themes := []*theme.Theme{theme.Default(), singleLineTheme(), asciiTheme()}
	corners := [][4]string{
		{"╔", "╗", "╚", "╝"},
		{"┌", "┐", "└", "┘"},
		{"+", "+", "+", "+"},
	}

	for i, th := range themes {
		win := makeWin()
		output := renderWindow(win,0, 0, 12, 3, th.LayerAt(0))
		s := newScreen(output)

		tl := s.region(0, 0, 1, 1)
		tr := s.region(11, 0, 1, 1)
		bl := s.region(0, 2, 1, 1)
		br := s.region(11, 2, 1, 1)

		if tl != corners[i][0] || tr != corners[i][1] || bl != corners[i][2] || br != corners[i][3] {
			t.Errorf("theme %d: corners got [%s %s %s %s], want %v",
				i, tl, tr, bl, br, corners[i])
		}

		// Content should be the same across all themes.
		inner := s.region(1, 1, 10, 1)
		if !strings.Contains(inner, "Hello") {
			t.Errorf("theme %d: inner content should contain 'Hello', got %q", i, inner)
		}
	}
}

// ─── Palette depth cycling tests ─────────────────────────────────────────────

// distinctPaletteTheme creates a theme where each palette has a unique,
// easily-identifiable background color.
func distinctPaletteTheme() *theme.Theme {
	th := theme.Default()
	th.Primary.Bg = lipgloss.Color("#110000")   // depth 0
	th.Secondary.Bg = lipgloss.Color("#002200") // depth 1, 3, 5...
	th.Tertiary.Bg = lipgloss.Color("#000033")  // depth 2, 4, 6...
	th.Warning.Bg = lipgloss.Color("#330000")   // warning dialogs
	return th
}

func TestPaletteDepthOnNCWindow(t *testing.T) {
	th := distinctPaletteTheme()
	label := &Label{Text: "hello"}
	win := &Window{
		Title: "Main",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}

	// Depth 0 → Primary.
	output0 := renderWindow(win,0, 0, 20, 4, th.LayerAt(0))
	assertHasANSI(t, output0, "#110000", "depth 0 (Primary)")

	// Depth 1 → Secondary.
	output1 := renderWindow(win,0, 0, 20, 4, th.LayerAt(1))
	assertHasANSI(t, output1, "#002200", "depth 1 (Secondary)")

	// Depth 2 → Tertiary.
	output2 := renderWindow(win,0, 0, 20, 4, th.LayerAt(2))
	assertHasANSI(t, output2, "#000033", "depth 2 (Tertiary)")
}

func TestPaletteDepthCyclesThroughLayers(t *testing.T) {
	th := distinctPaletteTheme()

	// Layer 0: Main window at depth 0 (Primary).
	label := &Label{Text: "content"}
	win := &Window{
		Title: "Panel",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	layer0 := renderWindow(win,0, 0, 40, 12, th.LayerAt(0))
	assertHasANSI(t, layer0, "#110000", "layer 0 (Primary)")

	s0 := newScreen(layer0)
	if !strings.Contains(s0.lines[0], "Panel") {
		t.Fatalf("expected title 'Panel', got %q", s0.lines[0])
	}

	// Layer 1: Dropdown at depth 1 (Secondary) over the window.
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}, {Label: "&Quit"}}},
	}
	ddBox := o.RenderDropdown(menus, 0, th.LayerAt(1))
	dd, ddCol, ddRow := ddBox.Content, ddBox.Col, ddBox.Row
	assertHasANSI(t, dd, "#002200", "layer 1 dropdown (Secondary)")

	layer1 := PlaceOverlay(ddCol, ddRow, dd, layer0)
	assertHasANSI(t, layer1, "#110000", "layer 1: Primary still visible")
	assertHasANSI(t, layer1, "#002200", "layer 1: Secondary in dropdown")

	s1 := newScreen(layer1)
	found := false
	for _, l := range s1.lines {
		if strings.Contains(l, "New") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dropdown 'New' missing from layer 1\n%s", s1.String())
	}

	// Layer 2: Dialog at depth 2 (Tertiary) over everything.
	o2 := OverlayState{OpenMenu: -1}
	o2.PushDialog(domain.DialogRequest{
		Title:   "Confirm",
		Body:    "Proceed?",
		Buttons: []string{"Yes", "No"},
	})
	dlgBuf, dlgCol, dlgRow := o2.RenderDialogBuf(40, 12, th.LayerAt(2))
	if dlgBuf == nil {
		t.Fatal("expected non-nil buffer for layer 2 dialog")
	}
	dlg := dlgBuf.ToString(colorprofile.TrueColor)
	assertHasANSI(t, dlg, "#000033", "layer 2 dialog (Tertiary)")

	layer2 := PlaceOverlay(dlgCol, dlgRow, dlg, layer1)
	assertHasANSI(t, layer2, "#110000", "layer 2: Primary")
	assertHasANSI(t, layer2, "#002200", "layer 2: Secondary")
	assertHasANSI(t, layer2, "#000033", "layer 2: Tertiary")

	// Layer 3: Nested dialog at depth 3 → Secondary again.
	o3 := OverlayState{OpenMenu: -1}
	o3.PushDialog(domain.DialogRequest{Title: "Nested", Body: "Inner"})
	dlg3Buf, dlg3Col, dlg3Row := o3.RenderDialogBuf(40, 12, th.LayerAt(3))
	if dlg3Buf == nil {
		t.Fatal("expected non-nil buffer for layer 3 dialog")
	}
	dlg3 := dlg3Buf.ToString(colorprofile.TrueColor)
	assertHasANSI(t, dlg3, "#002200", "layer 3 nested dialog (Secondary again)")

	layer3 := PlaceOverlay(dlg3Col, dlg3Row, dlg3, layer2)
	s3 := newScreen(layer3)
	foundNested := false
	for _, l := range s3.lines {
		if strings.Contains(l, "Nested") {
			foundNested = true
			break
		}
	}
	if !foundNested {
		t.Errorf("nested dialog 'Nested' missing\n%s", s3.String())
	}

	// Layer 4: Depth 4 → Tertiary again.
	o4 := OverlayState{OpenMenu: -1}
	o4.PushDialog(domain.DialogRequest{Title: "Deep", Body: "Very deep"})
	dlg4Buf, _, _ := o4.RenderDialogBuf(40, 12, th.LayerAt(4))
	if dlg4Buf == nil {
		t.Fatal("expected non-nil buffer for layer 4 dialog")
	}
	dlg4 := dlg4Buf.ToString(colorprofile.TrueColor)
	assertHasANSI(t, dlg4, "#000033", "layer 4 (Tertiary again)")
}

func TestPaletteDepthWarningBypassesCycle(t *testing.T) {
	th := distinctPaletteTheme()
	o := OverlayState{OpenMenu: -1}
	o.PushDialog(domain.DialogRequest{Title: "Error", Body: "Something broke"})

	dlgBuf, _, _ := o.RenderDialogBuf(40, 20, th.WarningLayer())
	if dlgBuf == nil {
		t.Fatal("expected non-nil buffer for warning dialog")
	}
	dlgStr := dlgBuf.ToString(colorprofile.TrueColor)
	assertHasANSI(t, dlgStr, "#330000", "warning dialog")

	// Title should appear somewhere in the rendered dialog (may be in border row).
	if !strings.Contains(stripANSI(dlgStr), "Error") {
		t.Errorf("expected 'Error' in dialog content, got %q", stripANSI(dlgStr))
	}
}

func TestNCBarPaletteMatchesDepth(t *testing.T) {
	th := distinctPaletteTheme()
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
	}

	bar1 := o.RenderMenuBar(30, menus, th.LayerAt(1))
	assertHasANSI(t, bar1, "#002200", "bar at depth 1 (Secondary)")

	bar0 := o.RenderMenuBar(30, menus, th.LayerAt(0))
	assertHasANSI(t, bar0, "#110000", "bar at depth 0 (Primary)")
}

// ─── Per-layer border depth tests ────────────────────────────────────────────

// layeredBorderTheme creates a theme where each layer has distinct border chars.
func layeredBorderTheme() *theme.Theme {
	th := theme.Default()
	// Primary (depth 0): double-line (default)
	th.Primary.OuterTL = "╔"
	th.Primary.OuterTR = "╗"
	th.Primary.OuterBL = "╚"
	th.Primary.OuterBR = "╝"
	th.Primary.OuterH = "═"
	th.Primary.OuterV = "║"
	th.Primary.InnerH = "─"
	th.Primary.CrossL = "╟"
	th.Primary.CrossR = "╢"
	th.Primary.BarSep = "│"
	// Secondary (depth 1, 3, 5...): single-line
	th.Secondary.OuterTL = "┌"
	th.Secondary.OuterTR = "┐"
	th.Secondary.OuterBL = "└"
	th.Secondary.OuterBR = "┘"
	th.Secondary.OuterH = "─"
	th.Secondary.OuterV = "│"
	th.Secondary.InnerH = "·"
	th.Secondary.CrossL = "├"
	th.Secondary.CrossR = "┤"
	th.Secondary.BarSep = "|"
	// Tertiary (depth 2, 4, 6...): ASCII
	th.Tertiary.OuterTL = "+"
	th.Tertiary.OuterTR = "+"
	th.Tertiary.OuterBL = "+"
	th.Tertiary.OuterBR = "+"
	th.Tertiary.OuterH = "-"
	th.Tertiary.OuterV = "|"
	th.Tertiary.InnerH = "."
	th.Tertiary.CrossL = "+"
	th.Tertiary.CrossR = "+"
	th.Tertiary.BarSep = ":"
	return th
}

func TestPerLayerBordersOnNCWindow(t *testing.T) {
	th := layeredBorderTheme()
	label := &Label{Text: "X"}

	makeWin := func() *Window {
		return &Window{
			Title: "T",
			Children: []GridChild{
				{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
			},
		}
	}

	// Depth 0 → Primary → double-line borders.
	win0 := makeWin()
	out0 := renderWindow(win0,0, 0, 10, 3, th.LayerAt(0))
	assertRegion(t, out0, 0, 0, 1, 3, "╔\n║\n╚")
	assertRegion(t, out0, 9, 0, 1, 3, "╗\n║\n╝")

	// Depth 1 → Secondary → single-line borders.
	win1 := makeWin()
	out1 := renderWindow(win1,0, 0, 10, 3, th.LayerAt(1))
	assertRegion(t, out1, 0, 0, 1, 3, "┌\n│\n└")
	assertRegion(t, out1, 9, 0, 1, 3, "┐\n│\n┘")

	// Depth 2 → Tertiary → ASCII borders.
	win2 := makeWin()
	out2 := renderWindow(win2,0, 0, 10, 3, th.LayerAt(2))
	assertRegion(t, out2, 0, 0, 1, 3, "+\n|\n+")
	assertRegion(t, out2, 9, 0, 1, 3, "+\n|\n+")

	// Depth 3 → Secondary again (single-line).
	win3 := makeWin()
	out3 := renderWindow(win3,0, 0, 10, 3, th.LayerAt(3))
	assertRegion(t, out3, 0, 0, 1, 3, "┌\n│\n└")

	// Depth 4 → Tertiary again (ASCII).
	win4 := makeWin()
	out4 := renderWindow(win4,0, 0, 10, 3, th.LayerAt(4))
	assertRegion(t, out4, 0, 0, 1, 3, "+\n|\n+")
}

func TestPerLayerBordersOnDropdown(t *testing.T) {
	th := layeredBorderTheme()
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{
			{Label: "&New"},
			{Label: "---"},
			{Label: "&Quit"},
		}},
	}

	// Depth 1 → Secondary → single-line.
	o1 := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	dd1 := o1.RenderDropdown(menus, 0, th.LayerAt(1)).Content
	s1 := newScreen(dd1)
	if !strings.HasPrefix(s1.lines[0], "┌") || !strings.HasSuffix(s1.lines[0], "┐") {
		t.Errorf("depth 1 dropdown: expected single-line top, got %q", s1.lines[0])
	}
	if !strings.Contains(s1.lines[2], "·") {
		t.Errorf("depth 1 separator: expected '·', got %q", s1.lines[2])
	}

	// Depth 2 → Tertiary → ASCII.
	o2 := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	dd2 := o2.RenderDropdown(menus, 0, th.LayerAt(2)).Content
	s2 := newScreen(dd2)
	if !strings.HasPrefix(s2.lines[0], "+") || !strings.HasSuffix(s2.lines[0], "+") {
		t.Errorf("depth 2 dropdown: expected ASCII top, got %q", s2.lines[0])
	}
	if !strings.Contains(s2.lines[2], ".") {
		t.Errorf("depth 2 separator: expected '.', got %q", s2.lines[2])
	}
}

func TestPerLayerBordersOnDialog(t *testing.T) {
	th := layeredBorderTheme()

	// Depth 2 → Tertiary → ASCII.
	o2 := OverlayState{OpenMenu: -1}
	o2.PushDialog(domain.DialogRequest{Title: "Test", Body: "Hello"})
	dlg2Buf, _, _ := o2.RenderDialogBuf(40, 20, th.LayerAt(2))
	if dlg2Buf == nil {
		t.Fatal("expected non-nil buffer for depth 2 dialog")
	}
	s2 := newScreen(dlg2Buf.ToString(colorprofile.TrueColor))
	if !strings.HasPrefix(s2.lines[0], "+") || !strings.HasSuffix(s2.lines[0], "+") {
		t.Errorf("depth 2 dialog: expected ASCII top, got %q", s2.lines[0])
	}
	// Dialog dividers are non-connected — separator floats inside padding.
	sepRow := 3 // top border + padding + body
	if !strings.Contains(s2.lines[sepRow], ".") {
		t.Errorf("depth 2 dialog separator: expected dots at row %d, got %q", sepRow, s2.lines[sepRow])
	}

	// Depth 1 → Secondary → single-line.
	o1 := OverlayState{OpenMenu: -1}
	o1.PushDialog(domain.DialogRequest{Title: "Test", Body: "Hello"})
	dlg1Buf, _, _ := o1.RenderDialogBuf(40, 20, th.LayerAt(1))
	if dlg1Buf == nil {
		t.Fatal("expected non-nil buffer for depth 1 dialog")
	}
	s1 := newScreen(dlg1Buf.ToString(colorprofile.TrueColor))
	if !strings.HasPrefix(s1.lines[0], "┌") || !strings.HasSuffix(s1.lines[0], "┐") {
		t.Errorf("depth 1 dialog: expected single-line top, got %q", s1.lines[0])
	}
}

func TestPerLayerBordersOnMenuBar(t *testing.T) {
	th := layeredBorderTheme()
	o := OverlayState{OpenMenu: -1}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []domain.MenuItemDef{{Label: "&Copy"}}},
	}

	// Depth 1 → Secondary → "|" separator.
	bar1 := o.RenderMenuBar(30, menus, th.LayerAt(1))
	s1 := newScreen(bar1)
	if !strings.Contains(s1.lines[0], "|") {
		t.Errorf("depth 1 bar: expected '|' separator, got %q", s1.lines[0])
	}

	// Depth 0 → Primary → "│" separator.
	bar0 := o.RenderMenuBar(30, menus, th.LayerAt(0))
	s0 := newScreen(bar0)
	if !strings.Contains(s0.lines[0], "│") {
		t.Errorf("depth 0 bar: expected '│' separator, got %q", s0.lines[0])
	}

	// Depth 2 → Tertiary → ":" separator.
	bar2 := o.RenderMenuBar(30, menus, th.LayerAt(2))
	s2 := newScreen(bar2)
	if !strings.Contains(s2.lines[0], ":") {
		t.Errorf("depth 2 bar: expected ':' separator, got %q", s2.lines[0])
	}
}

func TestPerLayerBordersCompositedStack(t *testing.T) {
	th := layeredBorderTheme()

	// Render a window at depth 0 (double-line).
	label := &Label{Text: "main"}
	win := &Window{
		Title: "Main",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	layer0 := renderWindow(win,0, 0, 30, 10, th.LayerAt(0))
	s0 := newScreen(layer0)
	if !strings.HasPrefix(s0.lines[0], "╔") {
		t.Fatalf("depth 0 window should use ╔, got %q", s0.lines[0])
	}

	// Composite a dropdown at depth 1 (single-line).
	o := OverlayState{MenuFocused: true, MenuCursor: 0, OpenMenu: 0, DropCursor: 0}
	menus := []domain.MenuDef{
		{Label: "&File", Items: []domain.MenuItemDef{{Label: "&New"}}},
	}
	ddBox := o.RenderDropdown(menus, 0, th.LayerAt(1))
	composited := PlaceOverlay(ddBox.Col, ddBox.Row, ddBox.Content, layer0)
	sc := newScreen(composited)

	// Row 0 should still have the double-line window border.
	if !strings.HasPrefix(sc.lines[0], "╔") {
		t.Errorf("window border lost after overlay, got %q", sc.lines[0])
	}
	// The dropdown (starting row 1) should use single-line borders.
	if !strings.HasPrefix(sc.lines[1], "┌") {
		t.Errorf("dropdown at depth 1 should use ┌, got %q", sc.lines[1])
	}
}
