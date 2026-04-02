package server

// Type aliases and re-exports from internal/widget.
// These keep existing server-package code compiling during the migration.

import (
	"null-space/common"
	"null-space/internal/widget"
)

type GameNCWindow = widget.GameWindow

func ReconcileGameWindow(
	prev *widget.GameWindow,
	tree *common.WidgetNode,
	renderFn func(buf *common.ImageBuffer, x, y, w, h int),
	onInput func(action string),
) *widget.GameWindow {
	return widget.ReconcileGameWindow(prev, tree, renderFn, onInput)
}
