package server

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// PlaceOverlay renders overlay on top of bg at terminal position (col, row).
// Both strings may contain ANSI escape sequences; bg styling outside the
// overlay region is preserved.
func PlaceOverlay(col, row int, overlay, bg string) string {
	bgLines := strings.Split(bg, "\n")
	overLines := strings.Split(overlay, "\n")
	out := make([]string, len(bgLines))
	copy(out, bgLines)
	for i, ol := range overLines {
		r := row + i
		if r < 0 || r >= len(bgLines) {
			continue
		}
		w := lipgloss.Width(ol)
		if w == 0 {
			continue
		}
		out[r] = paintLine(col, w, ol, bgLines[r])
	}
	return strings.Join(out, "\n")
}

// paintLine replaces visual columns [col, col+overW) in bgLine with over.
func paintLine(col, overW int, over, bgLine string) string {
	bgW := lipgloss.Width(bgLine)

	left := ansi.Truncate(bgLine, col, "")
	leftW := lipgloss.Width(left)
	if leftW < col {
		left += strings.Repeat(" ", col-leftW)
	}

	right := ""
	if end := col + overW; end < bgW {
		right = ansiSkipColumns(bgLine, end)
	}

	return left + over + right
}

// ansiSkipColumns returns the portion of s starting at visual column n,
// re-emitting any ANSI escape sequences established before that column so
// the right-hand text retains its styling.
func ansiSkipColumns(s string, n int) string {
	var preEsc strings.Builder
	var out strings.Builder
	col := 0
	pastN := false

	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// CSI sequence: scan to final byte (A–Z or a–z).
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++
			}
			seq := s[i:j]
			if pastN {
				out.WriteString(seq)
			} else {
				preEsc.WriteString(seq)
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if col >= n {
			if !pastN {
				pastN = true
				out.WriteString(preEsc.String())
			}
			out.WriteString(s[i : i+size])
		}
		col++
		i += size
	}
	return out.String()
}
