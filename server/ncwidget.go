package server

// Type aliases and re-exports from internal/widget.
// These keep existing server-package code compiling during the migration.

import "null-space/internal/widget"

// Core types.
type NCControl = widget.Control
type Fill = widget.Fill
type GridChild = widget.GridChild
type GridConstraint = widget.GridConstraint
type NCWindow = widget.Window
type TabWanter = widget.TabWanter
type Clickable = widget.Clickable

// Fill constants.
const (
	FillNone       = widget.FillNone
	FillHorizontal = widget.FillHorizontal
	FillVertical   = widget.FillVertical
	FillBoth       = widget.FillBoth
)

func distributeSpace(mins []int, weights []float64, total int) []int {
	return widget.DistributeSpace(mins, weights, total)
}
