package server

// Type aliases and re-exports from internal/widget.
// These keep existing server-package code compiling during the migration.

import "null-space/internal/widget"

// Control types.
type NCLabel = widget.Label
type NCTextInput = widget.TextInput
type NCCommandInput = widget.CommandInput
type NCTextView = widget.TextView
type NCTextArea = widget.TextArea
type NCButton = widget.Button
type NCCheckbox = widget.Checkbox
type NCHDivider = widget.HDivider
type NCVDivider = widget.VDivider
type NCPanel = widget.Panel
type NCGameView = widget.GameView
type NCTable = widget.Table
type NCContainer = widget.Container
type ContainerChild = widget.ContainerChild
type NCTeamPanel = widget.TeamPanel

func truncateStr(s string, maxW int) string {
	return widget.TruncateStr(s, maxW)
}

func renderScrollbar(total, visible, offset int, style interface{ Render(strs ...string) string }) []string {
	return widget.RenderScrollbar(total, visible, offset, style)
}
