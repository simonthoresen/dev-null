package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// ─── Core interfaces ──────────────────────────────────────────────────────────

// Control is the base interface for all NC widgets.
type Control interface {
	// Render writes the control's content into buf at position (x, y)
	// within the given (width × height) region. focused is true when this
	// control currently has keyboard focus.
	Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer)
	// Update handles a tea.Msg. Only called when this control has focus.
	Update(msg tea.Msg)
	// MinSize returns the minimum (width, height) this control needs.
	// A dimension of 0 means "no minimum" (flex in that direction).
	MinSize() (int, int)
	// Focusable returns true if this control can receive keyboard focus.
	Focusable() bool
}

// Fill controls how a control expands within its grid cell.
type Fill int

const (
	FillNone       Fill = iota // don't expand
	FillHorizontal             // expand width to fill cell
	FillVertical               // expand height to fill cell
	FillBoth                   // expand both directions
)

// GridChild pairs a control with its layout constraint.
type GridChild struct {
	Control    Control
	Constraint GridConstraint
	TabIndex   int // focus order — lower values receive focus first (default 0)
}

// GridConstraint positions a control in the grid layout.
type GridConstraint struct {
	Col, Row         int     // grid position (0-based)
	ColSpan, RowSpan int     // cells spanned (default 1)
	WeightX, WeightY float64 // share of extra space (0 = fixed, >0 = flex)
	Fill             Fill    // how to fill the allocated cell
	MinW, MinH       int     // override minimum size (0 = use control's MinSize)
}

func (c GridConstraint) ColSpanVal() int {
	if c.ColSpan <= 0 {
		return 1
	}
	return c.ColSpan
}
func (c GridConstraint) RowSpanVal() int {
	if c.RowSpan <= 0 {
		return 1
	}
	return c.RowSpan
}

// TabWanter is implemented by controls that signal tab/shift-tab to the window.
type TabWanter interface {
	TabWant() (wantTab, wantBackTab bool)
}

// Clickable is optionally implemented by Controls that handle mouse clicks.
// (rx, ry) are relative to the control's top-left corner.
type Clickable interface {
	HandleClick(rx, ry int)
}

// DistributeSpace allocates space to cells based on minimums and weights.
func DistributeSpace(mins []int, weights []float64, total int) []int {
	n := len(mins)
	sizes := make([]int, n)
	copy(sizes, mins)

	used := 0
	for _, s := range sizes {
		used += s
	}
	extra := total - used
	if extra <= 0 {
		return sizes
	}

	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}
	if totalWeight == 0 {
		// No weights — give all extra to the last cell.
		sizes[n-1] += extra
		return sizes
	}

	// Distribute proportionally by weight.
	distributed := 0
	for i, w := range weights {
		if w > 0 {
			share := int(float64(extra) * w / totalWeight)
			sizes[i] += share
			distributed += share
		}
	}
	// Give remainder to the first weighted cell.
	remainder := extra - distributed
	for i, w := range weights {
		if w > 0 && remainder > 0 {
			sizes[i] += remainder
			break
		}
		_ = w
	}
	return sizes
}
