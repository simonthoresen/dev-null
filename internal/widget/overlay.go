package widget

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
	placeOverlayLines(col, row, overLines, bgLines)
	return strings.Join(bgLines, "\n")
}

// placeOverlayLines composites overLines onto bgLines in-place at (col, row).
func placeOverlayLines(col, row int, overLines, bgLines []string) {
	for i, ol := range overLines {
		r := row + i
		if r < 0 || r >= len(bgLines) {
			continue
		}
		w := lipgloss.Width(ol)
		if w == 0 {
			continue
		}
		bgLines[r] = paintLine(col, w, ol, bgLines[r])
	}
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
		right = AnsiSkipColumns(bgLine, end)
	}

	return left + over + right
}

// ApplyShadow renders an L-shaped drop shadow around a box at (boxCol, boxRow)
// with dimensions boxW × boxH. The shadow is:
//   - Right strip: 1 cell wide at col=boxCol+boxW, rows boxRow+1 through boxRow+boxH-1
//   - Bottom strip: 1 cell tall at row=boxRow+boxH, cols boxCol+1 through boxCol+boxW
//
// Characters from the background are preserved but re-colored with shadowStyle.
func ApplyShadow(boxCol, boxRow, boxW, boxH int, bg string, shadowStyle lipgloss.Style) string {
	bgLines := strings.Split(bg, "\n")
	applyShadowLines(boxCol, boxRow, boxW, boxH, bgLines, shadowStyle)
	return strings.Join(bgLines, "\n")
}

// applyShadowLines applies the L-shaped drop shadow in-place on lines.
func applyShadowLines(boxCol, boxRow, boxW, boxH int, lines []string, shadowStyle lipgloss.Style) {
	// Right strip (skip top-right corner: start from row+1).
	rightCol := boxCol + boxW
	for dy := 1; dy < boxH; dy++ {
		r := boxRow + dy
		if r >= 0 && r < len(lines) {
			lines[r] = recolorCell(lines[r], rightCol, shadowStyle)
		}
	}

	// Bottom strip (skip bottom-left corner: start from col+1).
	// Batch all columns in a single pass to avoid O(W²) re-parsing.
	bottomRow := boxRow + boxH
	if bottomRow >= 0 && bottomRow < len(lines) {
		lines[bottomRow] = recolorSpan(lines[bottomRow], boxCol+1, boxW, shadowStyle)
	}
}

// recolorSpan replaces the styling of W consecutive characters starting at
// visual column startCol, keeping the characters but applying newStyle.
// This does a single parse of the line instead of W separate recolorCell calls.
func recolorSpan(line string, startCol, count int, newStyle lipgloss.Style) string {
	endCol := startCol + count
	lineW := lipgloss.Width(line)
	if startCol >= lineW || count <= 0 {
		return line
	}
	if endCol > lineW {
		endCol = lineW
	}

	// Left portion: everything before the span.
	left := ansi.Truncate(line, startCol, "")
	leftW := lipgloss.Width(left)
	if leftW < startCol {
		left += strings.Repeat(" ", startCol-leftW)
	}

	// Middle portion: extract characters in [startCol, endCol), restyle them.
	mid := AnsiSkipColumns(line, startCol)
	// Truncate mid to only the span width.
	midStripped := ansi.Strip(mid)
	var styled strings.Builder
	col := 0
	for i := 0; i < len(midStripped) && col < endCol-startCol; {
		r, size := utf8.DecodeRuneInString(midStripped[i:])
		styled.WriteString(newStyle.Render(string(r)))
		col++
		i += size
	}

	// Right portion: everything after the span.
	right := ""
	if endCol < lineW {
		right = AnsiSkipColumns(line, endCol)
	}

	return left + styled.String() + right
}

// recolorCell replaces the styling of the character at visual column col
// in line, keeping the character itself but applying newStyle.
func recolorCell(line string, col int, newStyle lipgloss.Style) string {
	lineW := lipgloss.Width(line)
	if col < 0 || col >= lineW {
		return line
	}

	// Get everything left of the target column.
	left := ansi.Truncate(line, col, "")
	leftW := lipgloss.Width(left)
	if leftW < col {
		left += strings.Repeat(" ", col-leftW)
	}

	// Extract the character at the target column.
	rest := AnsiSkipColumns(line, col)
	ch := " "
	if len(rest) > 0 {
		// Get just the first visible character.
		stripped := ansi.Strip(rest)
		if len(stripped) > 0 {
			r, _ := utf8.DecodeRuneInString(stripped)
			ch = string(r)
		}
	}

	// Get everything after the target column.
	right := ""
	if col+1 < lineW {
		right = AnsiSkipColumns(line, col+1)
	}

	return left + newStyle.Render(ch) + right
}

// AnsiSkipColumns returns the portion of s starting at visual column n,
// re-emitting any ANSI escape sequences established before that column so
// the right-hand text retains its styling.
func AnsiSkipColumns(s string, n int) string {
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
