package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/theme"
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

// ─── Shared test helpers ────────────────────────────────────────────────────

func testTheme() *theme.Theme { return theme.Default() }
func testLayer() *theme.Layer {
	l := testTheme().LayerAt(0)
	l.Monochrome = true // widget tests simulate a monochrome terminal so cursor glyphs (►/›) appear
	return l
}

// stripANSI removes all ANSI escape sequences for easier assertion.
func stripANSI(s string) string {
	return ansi.Strip(s)
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
