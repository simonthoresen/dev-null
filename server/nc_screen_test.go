package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"null-space/common"
)

// ─── Screen test harness ─────────────────────────────────────────────────────
//
// Provides helpers for comparing rendered NC output (after stripping ANSI)
// against expected multi-line strings. Supports full-screen and region matching.

// screen wraps a rendered string (ANSI-stripped) split into lines.
type screen struct {
	lines []string
	w, h  int
}

// newScreen strips ANSI codes and splits the rendered output into lines.
func newScreen(rendered string) screen {
	stripped := ansi.Strip(rendered)
	lines := strings.Split(stripped, "\n")
	w := 0
	for _, l := range lines {
		if lw := len([]rune(l)); lw > w {
			w = lw
		}
	}
	return screen{lines: lines, w: w, h: len(lines)}
}

// String returns the screen content as a single string (for error messages).
func (s screen) String() string {
	return strings.Join(s.lines, "\n")
}

// region extracts a rectangle at (col, row) with the given width and height.
// Out-of-bounds areas are filled with spaces.
func (s screen) region(col, row, w, h int) string {
	var lines []string
	for dy := range h {
		r := row + dy
		var line string
		if r >= 0 && r < len(s.lines) {
			runes := []rune(s.lines[r])
			for dx := range w {
				c := col + dx
				if c >= 0 && c < len(runes) {
					line += string(runes[c])
				} else {
					line += " "
				}
			}
		} else {
			line = strings.Repeat(" ", w)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// assertScreen compares the full rendered output against an expected multi-line
// string. Both are ANSI-stripped. Leading/trailing blank lines in expected are
// trimmed so you can write:
//
//	assertScreen(t, rendered, `
//	    ╔══════╗
//	    ║ Hi   ║
//	    ╚══════╝
//	`)
func assertScreen(t *testing.T, rendered, expected string) {
	t.Helper()
	got := newScreen(rendered)
	exp := trimExpected(expected)
	if got.String() != exp {
		t.Errorf("screen mismatch\n--- got ---\n%s\n--- expected ---\n%s", got.String(), exp)
	}
}

// assertRegion checks that a rectangle at (col, row) with dimensions w×h
// in the rendered output matches the expected multi-line string.
func assertRegion(t *testing.T, rendered string, col, row, w, h int, expected string) {
	t.Helper()
	got := newScreen(rendered).region(col, row, w, h)
	exp := trimExpected(expected)
	if got != exp {
		t.Errorf("region (%d,%d %dx%d) mismatch\n--- got ---\n%s\n--- expected ---\n%s",
			col, row, w, h, got, exp)
	}
}

// trimExpected strips the common leading whitespace and leading/trailing blank
// lines from a raw string literal so tests can be written with indentation:
//
//	trimExpected(`
//	    ╔══╗
//	    ║Hi║
//	    ╚══╝
//	`)
//
// becomes "╔══╗\n║Hi║\n╚══╝".
func trimExpected(s string) string {
	lines := strings.Split(s, "\n")

	// Drop leading blank lines.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	// Drop trailing blank lines.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}

	// Find minimum indentation (tabs or spaces) across non-empty lines.
	minIndent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}

	// Strip common indent.
	for i, l := range lines {
		if len(l) >= minIndent {
			lines[i] = l[minIndent:]
		}
	}
	return strings.Join(lines, "\n")
}

// assertHasANSI checks that the raw (non-stripped) rendered output contains the
// given ANSI color sequence. Useful for verifying palette application.
func assertHasANSI(t *testing.T, rendered, colorHex, label string) {
	t.Helper()
	if len(colorHex) != 7 || colorHex[0] != '#' {
		t.Fatalf("bad hex color %q", colorHex)
	}
	r := hexToDec(colorHex[1:3])
	g := hexToDec(colorHex[3:5])
	b := hexToDec(colorHex[5:7])
	needle := fmt.Sprintf("%d;%d;%d", r, g, b)
	if !strings.Contains(rendered, needle) {
		t.Errorf("expected ANSI color %s (%s) in output for %s", colorHex, needle, label)
	}
}

func hexToDec(h string) int {
	v := 0
	for _, c := range h {
		v *= 16
		switch {
		case c >= '0' && c <= '9':
			v += int(c - '0')
		case c >= 'a' && c <= 'f':
			v += int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v += int(c-'A') + 10
		}
	}
	return v
}

// ─── Tests for the harness itself ────────────────────────────────────────────

func TestTrimExpected(t *testing.T) {
	got := trimExpected(`
		hello
		world
	`)
	if got != "hello\nworld" {
		t.Errorf("trimExpected failed: %q", got)
	}
}

func TestScreenRegion(t *testing.T) {
	s := newScreen("ABCDE\nFGHIJ\nKLMNO")
	got := s.region(1, 0, 3, 2)
	exp := "BCD\nGHI"
	if got != exp {
		t.Errorf("region mismatch\ngot: %q\nexp: %q", got, exp)
	}
}

func TestScreenRegionOutOfBounds(t *testing.T) {
	s := newScreen("AB\nCD")
	got := s.region(1, 1, 3, 3)
	// (1,1) in a 2x2 grid, requesting 3x3 → pads with spaces
	exp := "D  \n   \n   "
	if got != exp {
		t.Errorf("region oob mismatch\ngot: %q\nexp: %q", got, exp)
	}
}

// ─── Theme border tests ─────────────────────────────────────────────────────

// singleLineTheme returns a theme that uses single-line box drawing chars.
func singleLineTheme() *Theme {
	th := DefaultTheme()
	for _, l := range []*ThemeLayer{&th.Primary, &th.Secondary, &th.Tertiary, &th.Warning} {
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
func asciiTheme() *Theme {
	th := DefaultTheme()
	for _, l := range []*ThemeLayer{&th.Primary, &th.Secondary, &th.Tertiary, &th.Warning} {
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
	label := &NCLabel{Text: "X"}
	win := &NCWindow{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := DefaultTheme()
	output := win.Render(0, 0, 10, 3, th.LayerAt(0))
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
	label := &NCLabel{Text: "X"}
	win := &NCWindow{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := singleLineTheme()
	output := win.Render(0, 0, 10, 3, th.LayerAt(0))
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
	label := &NCLabel{Text: "X"}
	win := &NCWindow{
		Title: "T",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	th := asciiTheme()
	output := win.Render(0, 0, 10, 3, th.LayerAt(0))
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
	o := overlayState{menuFocused: true, menuCursor: 0, openMenu: 0, dropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&New"},
			{Label: "---"},
			{Label: "&Quit"},
		}},
	}
	th := singleLineTheme()
	dd := o.renderDropdown(menus, 0, th.LayerAt(1)).content
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
	o := overlayState{openMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}

	// Default theme separator is │.
	th := DefaultTheme()
	output := o.renderNCBar(30, menus, th.LayerAt(1))
	s := newScreen(output)
	if !strings.Contains(s.lines[0], "│") {
		t.Errorf("default bar should use │ separator, got %q", s.lines[0])
	}

	// Custom theme separator.
	th2 := singleLineTheme() // uses "|"
	output2 := o.renderNCBar(30, menus, th2.LayerAt(1))
	s2 := newScreen(output2)
	if !strings.Contains(s2.lines[0], "|") {
		t.Errorf("custom bar should use | separator, got %q", s2.lines[0])
	}
}

func TestDialogBordersASCIITheme(t *testing.T) {
	o := overlayState{openMenu: -1}
	o.pushDialog(common.DialogRequest{
		Title: "Err",
		Body:  "Oops",
	})
	th := asciiTheme()
	dlg := o.renderDialog(40, 20, th.WarningLayer()).content
	s := newScreen(dlg)

	if !strings.HasPrefix(s.lines[0], "+") || !strings.HasSuffix(s.lines[0], "+") {
		t.Errorf("expected ASCII top border, got %q", s.lines[0])
	}
	if !strings.Contains(s.lines[0], "---") {
		t.Errorf("expected --- in top border, got %q", s.lines[0])
	}
	// Title separator uses CrossL/CrossR + inner-H.
	if !strings.HasPrefix(s.lines[2], "+") || !strings.HasSuffix(s.lines[2], "+") {
		t.Errorf("expected ASCII separator, got %q", s.lines[2])
	}
	if !strings.Contains(s.lines[2], "...") {
		t.Errorf("expected ... in separator, got %q", s.lines[2])
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
	label := &NCLabel{Text: "Hello"}
	makeWin := func() *NCWindow {
		return &NCWindow{
			Title: "T",
			Children: []GridChild{
				{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
			},
		}
	}

	themes := []*Theme{DefaultTheme(), singleLineTheme(), asciiTheme()}
	corners := [][4]string{
		{"╔", "╗", "╚", "╝"},
		{"┌", "┐", "└", "┘"},
		{"+", "+", "+", "+"},
	}

	for i, th := range themes {
		win := makeWin()
		output := win.Render(0, 0, 12, 3, th.LayerAt(0))
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
//
// Verify the Primary → Secondary → Tertiary → Secondary → Tertiary cycle
// by rendering layers at each depth and checking ANSI color codes.

// distinctPaletteTheme creates a theme where each palette has a unique,
// easily-identifiable background color.
func distinctPaletteTheme() *Theme {
	th := DefaultTheme()
	th.Primary.Bg = "#110000"   // depth 0
	th.Secondary.Bg = "#002200" // depth 1, 3, 5...
	th.Tertiary.Bg = "#000033"  // depth 2, 4, 6...
	th.Warning.Bg = "#330000"   // warning dialogs
	return th
}

func TestPaletteDepthOnNCWindow(t *testing.T) {
	th := distinctPaletteTheme()
	label := &NCLabel{Text: "hello"}
	win := &NCWindow{
		Title: "Main",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}

	// Depth 0 → Primary.
	output0 := win.Render(0, 0, 20, 4, th.LayerAt(0))
	assertHasANSI(t, output0, "#110000", "depth 0 (Primary)")

	// Depth 1 → Secondary.
	output1 := win.Render(0, 0, 20, 4, th.LayerAt(1))
	assertHasANSI(t, output1, "#002200", "depth 1 (Secondary)")

	// Depth 2 → Tertiary.
	output2 := win.Render(0, 0, 20, 4, th.LayerAt(2))
	assertHasANSI(t, output2, "#000033", "depth 2 (Tertiary)")
}

func TestPaletteDepthCyclesThroughLayers(t *testing.T) {
	// Build a composited screen layer by layer — window, dropdown, dialog,
	// nested dialog — verifying the correct palette at each step.
	th := distinctPaletteTheme()

	// Layer 0: Main window at depth 0 (Primary).
	label := &NCLabel{Text: "content"}
	win := &NCWindow{
		Title: "Panel",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	layer0 := win.Render(0, 0, 40, 12, th.LayerAt(0))
	assertHasANSI(t, layer0, "#110000", "layer 0 (Primary)")

	s0 := newScreen(layer0)
	if !strings.Contains(s0.lines[0], "Panel") {
		t.Fatalf("expected title 'Panel', got %q", s0.lines[0])
	}

	// Layer 1: Dropdown at depth 1 (Secondary) over the window.
	o := overlayState{menuFocused: true, menuCursor: 0, openMenu: 0, dropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}, {Label: "&Quit"}}},
	}
	ddBox := o.renderDropdown(menus, 0, th.LayerAt(1))
	dd, ddCol, ddRow := ddBox.content, ddBox.col, ddBox.row
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
	o2 := overlayState{openMenu: -1}
	o2.pushDialog(common.DialogRequest{
		Title:   "Confirm",
		Body:    "Proceed?",
		Buttons: []string{"Yes", "No"},
	})
	dlgBox := o2.renderDialog(40, 12, th.LayerAt(2))
	dlg, dlgCol, dlgRow := dlgBox.content, dlgBox.col, dlgBox.row
	assertHasANSI(t, dlg, "#000033", "layer 2 dialog (Tertiary)")

	layer2 := PlaceOverlay(dlgCol, dlgRow, dlg, layer1)
	assertHasANSI(t, layer2, "#110000", "layer 2: Primary")
	assertHasANSI(t, layer2, "#002200", "layer 2: Secondary")
	assertHasANSI(t, layer2, "#000033", "layer 2: Tertiary")

	// Layer 3: Nested dialog at depth 3 → Secondary again.
	o3 := overlayState{openMenu: -1}
	o3.pushDialog(common.DialogRequest{Title: "Nested", Body: "Inner"})
	dlg3Box := o3.renderDialog(40, 12, th.LayerAt(3))
	dlg3, dlg3Col, dlg3Row := dlg3Box.content, dlg3Box.col, dlg3Box.row
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
	o4 := overlayState{openMenu: -1}
	o4.pushDialog(common.DialogRequest{Title: "Deep", Body: "Very deep"})
	dlg4 := o4.renderDialog(40, 12, th.LayerAt(4)).content
	assertHasANSI(t, dlg4, "#000033", "layer 4 (Tertiary again)")
}

func TestPaletteDepthWarningBypassesCycle(t *testing.T) {
	th := distinctPaletteTheme()
	o := overlayState{openMenu: -1}
	o.pushDialog(common.DialogRequest{Title: "Error", Body: "Something broke"})

	dlg := o.renderDialog(40, 20, th.WarningLayer()).content
	assertHasANSI(t, dlg, "#330000", "warning dialog")

	s := newScreen(dlg)
	if !strings.Contains(s.lines[1], "Error") {
		t.Errorf("expected 'Error' in title, got %q", s.lines[1])
	}
}

func TestNCBarPaletteMatchesDepth(t *testing.T) {
	th := distinctPaletteTheme()
	o := overlayState{openMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
	}

	bar1 := o.renderNCBar(30, menus, th.LayerAt(1))
	assertHasANSI(t, bar1, "#002200", "bar at depth 1 (Secondary)")

	bar0 := o.renderNCBar(30, menus, th.LayerAt(0))
	assertHasANSI(t, bar0, "#110000", "bar at depth 0 (Primary)")
}

// ─── Per-layer border depth tests ────────────────────────────────────────────

// layeredBorderTheme creates a theme where each layer has distinct border chars,
// allowing depth cycling to be verified structurally from stripped text.
func layeredBorderTheme() *Theme {
	th := DefaultTheme()
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
	label := &NCLabel{Text: "X"}

	makeWin := func() *NCWindow {
		return &NCWindow{
			Title: "T",
			Children: []GridChild{
				{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
			},
		}
	}

	// Depth 0 → Primary → double-line borders.
	win0 := makeWin()
	out0 := win0.Render(0, 0, 10, 3, th.LayerAt(0))
	assertRegion(t, out0, 0, 0, 1, 3, "╔\n║\n╚")
	assertRegion(t, out0, 9, 0, 1, 3, "╗\n║\n╝")

	// Depth 1 → Secondary → single-line borders.
	win1 := makeWin()
	out1 := win1.Render(0, 0, 10, 3, th.LayerAt(1))
	assertRegion(t, out1, 0, 0, 1, 3, "┌\n│\n└")
	assertRegion(t, out1, 9, 0, 1, 3, "┐\n│\n┘")

	// Depth 2 → Tertiary → ASCII borders.
	win2 := makeWin()
	out2 := win2.Render(0, 0, 10, 3, th.LayerAt(2))
	assertRegion(t, out2, 0, 0, 1, 3, "+\n|\n+")
	assertRegion(t, out2, 9, 0, 1, 3, "+\n|\n+")

	// Depth 3 → Secondary again (single-line).
	win3 := makeWin()
	out3 := win3.Render(0, 0, 10, 3, th.LayerAt(3))
	assertRegion(t, out3, 0, 0, 1, 3, "┌\n│\n└")

	// Depth 4 → Tertiary again (ASCII).
	win4 := makeWin()
	out4 := win4.Render(0, 0, 10, 3, th.LayerAt(4))
	assertRegion(t, out4, 0, 0, 1, 3, "+\n|\n+")
}

func TestPerLayerBordersOnDropdown(t *testing.T) {
	th := layeredBorderTheme()
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{
			{Label: "&New"},
			{Label: "---"},
			{Label: "&Quit"},
		}},
	}

	// Depth 1 → Secondary → single-line.
	o1 := overlayState{menuFocused: true, menuCursor: 0, openMenu: 0, dropCursor: 0}
	dd1 := o1.renderDropdown(menus, 0, th.LayerAt(1)).content
	s1 := newScreen(dd1)
	if !strings.HasPrefix(s1.lines[0], "┌") || !strings.HasSuffix(s1.lines[0], "┐") {
		t.Errorf("depth 1 dropdown: expected single-line top, got %q", s1.lines[0])
	}
	if !strings.Contains(s1.lines[2], "·") {
		t.Errorf("depth 1 separator: expected '·', got %q", s1.lines[2])
	}

	// Depth 2 → Tertiary → ASCII.
	o2 := overlayState{menuFocused: true, menuCursor: 0, openMenu: 0, dropCursor: 0}
	dd2 := o2.renderDropdown(menus, 0, th.LayerAt(2)).content
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
	o2 := overlayState{openMenu: -1}
	o2.pushDialog(common.DialogRequest{Title: "Test", Body: "Hello"})
	dlg2 := o2.renderDialog(40, 20, th.LayerAt(2)).content
	s2 := newScreen(dlg2)
	if !strings.HasPrefix(s2.lines[0], "+") || !strings.HasSuffix(s2.lines[0], "+") {
		t.Errorf("depth 2 dialog: expected ASCII top, got %q", s2.lines[0])
	}
	// Title separator should use Tertiary CrossL/CrossR.
	if !strings.HasPrefix(s2.lines[2], "+") || !strings.HasSuffix(s2.lines[2], "+") {
		t.Errorf("depth 2 dialog separator: expected +...+, got %q", s2.lines[2])
	}

	// Depth 1 → Secondary → single-line.
	o1 := overlayState{openMenu: -1}
	o1.pushDialog(common.DialogRequest{Title: "Test", Body: "Hello"})
	dlg1 := o1.renderDialog(40, 20, th.LayerAt(1)).content
	s1 := newScreen(dlg1)
	if !strings.HasPrefix(s1.lines[0], "┌") || !strings.HasSuffix(s1.lines[0], "┐") {
		t.Errorf("depth 1 dialog: expected single-line top, got %q", s1.lines[0])
	}
}

func TestPerLayerBordersOnMenuBar(t *testing.T) {
	th := layeredBorderTheme()
	o := overlayState{openMenu: -1}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
		{Label: "&Edit", Items: []common.MenuItemDef{{Label: "&Copy"}}},
	}

	// Depth 1 → Secondary → "|" separator.
	bar1 := o.renderNCBar(30, menus, th.LayerAt(1))
	s1 := newScreen(bar1)
	if !strings.Contains(s1.lines[0], "|") {
		t.Errorf("depth 1 bar: expected '|' separator, got %q", s1.lines[0])
	}

	// Depth 0 → Primary → "│" separator.
	bar0 := o.renderNCBar(30, menus, th.LayerAt(0))
	s0 := newScreen(bar0)
	if !strings.Contains(s0.lines[0], "│") {
		t.Errorf("depth 0 bar: expected '│' separator, got %q", s0.lines[0])
	}

	// Depth 2 → Tertiary → ":" separator.
	bar2 := o.renderNCBar(30, menus, th.LayerAt(2))
	s2 := newScreen(bar2)
	if !strings.Contains(s2.lines[0], ":") {
		t.Errorf("depth 2 bar: expected ':' separator, got %q", s2.lines[0])
	}
}

func TestPerLayerBordersCompositedStack(t *testing.T) {
	th := layeredBorderTheme()

	// Render a window at depth 0 (double-line).
	label := &NCLabel{Text: "main"}
	win := &NCWindow{
		Title: "Main",
		Children: []GridChild{
			{Control: label, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal}},
		},
	}
	layer0 := win.Render(0, 0, 30, 10, th.LayerAt(0))
	s0 := newScreen(layer0)
	if !strings.HasPrefix(s0.lines[0], "╔") {
		t.Fatalf("depth 0 window should use ╔, got %q", s0.lines[0])
	}

	// Composite a dropdown at depth 1 (single-line).
	o := overlayState{menuFocused: true, menuCursor: 0, openMenu: 0, dropCursor: 0}
	menus := []common.MenuDef{
		{Label: "&File", Items: []common.MenuItemDef{{Label: "&New"}}},
	}
	ddBox := o.renderDropdown(menus, 0, th.LayerAt(1))
	composited := PlaceOverlay(ddBox.col, ddBox.row, ddBox.content, layer0)
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
