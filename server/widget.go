package server

// Type aliases and re-exports from internal/widget.
// These keep existing server-package code compiling during the migration.

import (
	"null-space/common"
	"null-space/internal/widget"
)

type overlayState = widget.OverlayState
type showDialogMsg = widget.ShowDialogMsg
type overlayBox = widget.OverlayBox

func stripAmpersand(label string) (string, rune) {
	return widget.StripAmpersand(label)
}

func menuShortcut(m common.MenuDef) rune {
	return widget.MenuShortcut(m)
}

func itemShortcut(it common.MenuItemDef) rune {
	return widget.ItemShortcut(it)
}

func hotkeyDisplay(key string) string {
	return widget.HotkeyDisplay(key)
}

func isSeparator(item common.MenuItemDef) bool {
	return widget.IsSeparator(item)
}

func firstSelectable(items []common.MenuItemDef) int {
	return widget.FirstSelectable(items)
}

func nextSelectable(items []common.MenuItemDef, cur int) int {
	return widget.NextSelectable(items, cur)
}

func prevSelectable(items []common.MenuItemDef, cur int) int {
	return widget.PrevSelectable(items, cur)
}

func ncBarMenuPositions(menus []common.MenuDef) []int {
	return widget.MenuBarPositions(menus)
}
