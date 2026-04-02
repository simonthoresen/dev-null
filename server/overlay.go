package server

// Re-exports from internal/widget.
// These keep existing server-package code compiling during the migration.

import (
	"charm.land/lipgloss/v2"

	"null-space/internal/widget"
)

func PlaceOverlay(col, row int, overlay, bg string) string {
	return widget.PlaceOverlay(col, row, overlay, bg)
}

func ApplyShadow(boxCol, boxRow, boxW, boxH int, bg string, shadowStyle lipgloss.Style) string {
	return widget.ApplyShadow(boxCol, boxRow, boxW, boxH, bg, shadowStyle)
}

func ansiSkipColumns(s string, n int) string {
	return widget.AnsiSkipColumns(s, n)
}
