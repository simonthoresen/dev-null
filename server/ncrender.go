package server

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ─── String Helpers ────────────────────────────────────────────────────

// fitLine pads or truncates a string to exactly `width` visible characters.
func fitLine(s string, width int) string {
	vis := ansi.StringWidth(s)
	if vis == width {
		return s
	}
	if vis > width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-vis)
}
