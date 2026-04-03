package widget

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// Container is a borderless layout container that arranges children
// horizontally (hsplit) or vertically (vsplit). It supports nested focus
// management: when the container has focus from its parent, it routes keys
// to its internally focused child and cycles focus among focusable children.
type Container struct {
	Horizontal bool // true = side-by-side, false = stacked
	Children   []ContainerChild
	FocusIdx   int // which child has internal focus (-1 = none)

	wantTab     bool
	wantBackTab bool

	// Cached from last Render for click hit-testing.
	cachedSizes []int
}

// ContainerChild pairs a Control with its sizing info.
type ContainerChild struct {
	Control Control
	Weight  float64 // flex weight (0 = use Fixed)
	Fixed   int     // fixed size (0 = use Weight)
}

func (c *Container) Focusable() bool {
	// Focusable if any child is focusable.
	for _, child := range c.Children {
		if child.Control.Focusable() {
			return true
		}
	}
	return false
}

func (c *Container) MinSize() (int, int) {
	w, h := 0, 0
	for _, child := range c.Children {
		cw, ch := child.Control.MinSize()
		if child.Fixed > 0 && child.Fixed > cw {
			cw = child.Fixed
		}
		if c.Horizontal {
			w += cw
			if ch > h {
				h = ch
			}
		} else {
			if cw > w {
				w = cw
			}
			h += ch
		}
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func (c *Container) TabWant() (bool, bool) {
	fwd, back := c.wantTab, c.wantBackTab
	c.wantTab = false
	c.wantBackTab = false
	return fwd, back
}

func (c *Container) Update(msg tea.Msg) {
	c.wantTab = false
	c.wantBackTab = false

	// If no child has focus, try to focus the first focusable child.
	if c.FocusIdx < 0 || c.FocusIdx >= len(c.Children) {
		c.focusFirst()
	}
	if c.FocusIdx < 0 {
		return
	}

	child := c.Children[c.FocusIdx].Control
	if !child.Focusable() {
		return
	}
	child.Update(msg)

	// Check if the child wants to cycle focus.
	if tw, ok := child.(TabWanter); ok {
		fwd, back := tw.TabWant()
		if fwd {
			if !c.cycleFocus(+1) {
				// Wrapped past end — propagate Tab to parent.
				c.wantTab = true
			}
		} else if back {
			if !c.cycleFocus(-1) {
				c.wantBackTab = true
			}
		}
	}
}

// focusFirst sets FocusIdx to the first focusable child.
func (c *Container) focusFirst() {
	for i, child := range c.Children {
		if child.Control.Focusable() {
			c.FocusIdx = i
			return
		}
	}
	c.FocusIdx = -1
}

// cycleFocus moves to the next/prev focusable child. Returns false if
// it would wrap (caller should propagate Tab to parent).
func (c *Container) cycleFocus(dir int) bool {
	n := len(c.Children)
	if n == 0 {
		return false
	}
	start := c.FocusIdx
	idx := start
	for {
		idx = (idx + dir + n) % n
		if idx == start {
			return false // wrapped around
		}
		if c.Children[idx].Control.Focusable() {
			old := c.FocusIdx
			c.FocusIdx = idx
			c.blurTextInput(old)
			c.activateTextInput(idx)
			return true
		}
	}
}

func (c *Container) blurTextInput(idx int) {
	if idx < 0 || idx >= len(c.Children) {
		return
	}
	switch ti := c.Children[idx].Control.(type) {
	case *TextInput:
		ti.Model.Blur()
	case *CommandInput:
		ti.Model.Blur()
	}
}

func (c *Container) activateTextInput(idx int) {
	if idx < 0 || idx >= len(c.Children) {
		return
	}
	switch ti := c.Children[idx].Control.(type) {
	case *TextInput:
		ti.Model.Focus()
	case *CommandInput:
		ti.Model.Focus()
	}
}

// CursorPosition returns the cursor position if a text input has focus.
func (c *Container) CursorPosition(bx, by int, sizes []int) (cx, cy int, visible bool) {
	if c.FocusIdx < 0 || c.FocusIdx >= len(c.Children) {
		return 0, 0, false
	}
	child := c.Children[c.FocusIdx].Control

	var model *textinput.Model
	switch ti := child.(type) {
	case *TextInput:
		model = ti.Model
	case *CommandInput:
		model = ti.Model
	}
	if model == nil {
		return 0, 0, false
	}
	cursor := model.Cursor()
	if cursor == nil {
		return 0, 0, false
	}

	// Calculate child position from sizes.
	offset := 0
	for i := 0; i < c.FocusIdx && i < len(sizes); i++ {
		offset += sizes[i]
	}
	if c.Horizontal {
		cx = bx + offset + 1 + cursor.Position.X // +1 for "[" bracket
		cy = by
	} else {
		cx = bx + 1 + cursor.Position.X
		cy = by + offset
	}
	return cx, cy, true
}

func (c *Container) Render(buf *render.ImageBuffer, bx, by, width, height int, focused bool, layer *theme.Layer) {
	if len(c.Children) == 0 {
		return
	}

	sizes := c.allocate(width, height)
	c.cachedSizes = sizes

	if c.Horizontal {
		col := bx
		for i, child := range c.Children {
			cw := sizes[i]
			if cw > 0 {
				hasFocus := focused && i == c.FocusIdx
				child.Control.Render(buf, col, by, cw, height, hasFocus, layer)
			}
			col += cw
		}
	} else {
		row := by
		for i, child := range c.Children {
			ch := sizes[i]
			if ch > 0 {
				hasFocus := focused && i == c.FocusIdx
				child.Control.Render(buf, bx, row, width, ch, hasFocus, layer)
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

// HandleClick routes a mouse click to the correct child control.
// (rx, ry) are relative to the container's top-left corner.
func (c *Container) HandleClick(rx, ry int) {
	if len(c.Children) == 0 || len(c.cachedSizes) != len(c.Children) {
		return
	}
	offset := 0
	for i, child := range c.Children {
		sz := c.cachedSizes[i]
		end := offset + sz
		hit := false
		if c.Horizontal {
			hit = rx >= offset && rx < end
		} else {
			hit = ry >= offset && ry < end
		}
		if hit {
			if child.Control.Focusable() {
				c.FocusIdx = i
			}
			if cl, ok := child.Control.(Clickable); ok {
				if c.Horizontal {
					cl.HandleClick(rx-offset, ry)
				} else {
					cl.HandleClick(rx, ry-offset)
				}
			}
			return
		}
		offset = end
	}
}

// Ensure Container implements TabWanter at compile time.
var _ TabWanter = (*Container)(nil)
