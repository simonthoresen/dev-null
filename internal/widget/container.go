package widget

import (
	tea "charm.land/bubbletea/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// Container is a borderless layout container that arranges children
// horizontally (hsplit) or vertically (vsplit).
type Container struct {
	Horizontal bool // true = side-by-side, false = stacked
	Children   []ContainerChild
}

// ContainerChild pairs a Control with its sizing info.
type ContainerChild struct {
	Control Control
	Weight  float64 // flex weight (0 = use Fixed)
	Fixed   int     // fixed size (0 = use Weight)
}

func (c *Container) Update(_ tea.Msg)     {}
func (c *Container) Focusable() bool      { return false }
func (c *Container) MinSize() (int, int)  { return 1, 1 }

func (c *Container) Render(buf *common.ImageBuffer, bx, by, width, height int, _ bool, layer *theme.Layer) {
	if len(c.Children) == 0 {
		return
	}

	sizes := c.allocate(width, height)

	if c.Horizontal {
		col := bx
		for i, child := range c.Children {
			cw := sizes[i]
			if cw > 0 {
				child.Control.Render(buf, col, by, cw, height, false, layer)
			}
			col += cw
		}
	} else {
		row := by
		for i, child := range c.Children {
			ch := sizes[i]
			if ch > 0 {
				child.Control.Render(buf, bx, row, width, ch, false, layer)
			}
			row += ch
		}
	}
}

func (c *Container) allocate(width, height int) []int {
	total := height
	if c.Horizontal {
		total = width
	}
	sizes := make([]int, len(c.Children))
	remaining := total
	totalWeight := 0.0

	for i, child := range c.Children {
		if child.Fixed > 0 {
			sizes[i] = min(child.Fixed, remaining)
			remaining -= sizes[i]
		} else {
			w := child.Weight
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
	}

	if totalWeight > 0 && remaining > 0 {
		distributed := 0
		for i, child := range c.Children {
			if child.Fixed > 0 {
				continue
			}
			w := child.Weight
			if w <= 0 {
				w = 1
			}
			sizes[i] = int(float64(remaining) * w / totalWeight)
			distributed += sizes[i]
		}
		leftover := remaining - distributed
		for i, child := range c.Children {
			if child.Fixed == 0 {
				sizes[i] += leftover
				break
			}
			_ = child
		}
	}
	return sizes
}
