package server

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"null-space/common"
)

// ─── Render cache ─────────────────────────────────────────────────────

// ncRenderCacheKey identifies a cached subtree render.
type ncRenderCacheKey struct {
	hash          uint64
	width, height int
}

// ncRenderCache caches rendered []string output for WidgetNode subtrees
// whose Hash() is non-zero (i.e. subtrees with no gameview descendants).
// The cache is per-player and lives on chromeModel, so it persists across
// frames but is scoped to one connection.
type ncRenderCache struct {
	entries map[ncRenderCacheKey][]string
}

func newNCRenderCache() *ncRenderCache {
	return &ncRenderCache{entries: make(map[ncRenderCacheKey][]string)}
}

// get returns a cached render if available.
func (c *ncRenderCache) get(hash uint64, width, height int) ([]string, bool) {
	if c == nil || hash == 0 {
		return nil, false
	}
	lines, ok := c.entries[ncRenderCacheKey{hash, width, height}]
	return lines, ok
}

// put stores a rendered subtree. Only called for hash != 0.
func (c *ncRenderCache) put(hash uint64, width, height int, lines []string) {
	if c == nil || hash == 0 {
		return
	}
	c.entries[ncRenderCacheKey{hash, width, height}] = lines
}

// reset clears all cached entries (e.g. on theme change).
func (c *ncRenderCache) reset() {
	if c == nil {
		return
	}
	clear(c.entries)
}

// ─── Tree rendering ───────────────────────────────────────────────────

// renderWidgetTree renders a WidgetNode tree into a string of exactly width×height.
// viewFn is called when a "gameview" node is encountered — it renders the raw game view.
// cache may be nil (no caching).
func renderWidgetTree(node *common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) string {
	lines := renderNode(node, width, height, layer, viewFn, cache)
	// Ensure exactly height lines, each exactly width visible chars
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = fitLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

// renderNode dispatches rendering based on node type.
// If the node's hash is non-zero and a cached result exists, it is returned directly.
func renderNode(node *common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) []string {
	if node == nil || width <= 0 || height <= 0 {
		return emptyLines(width, height)
	}

	hash := node.Hash()
	if lines, ok := cache.get(hash, width, height); ok {
		return lines
	}

	var lines []string
	switch node.Type {
	case "gameview":
		lines = renderGameViewNode(width, height, viewFn)
	case "panel":
		lines = renderPanelNode(node, width, height, layer, viewFn, cache)
	case "label":
		lines = renderLabelNode(node, width, height)
	case "hsplit":
		lines = renderHSplitNode(node, width, height, layer, viewFn, cache)
	case "vsplit":
		lines = renderVSplitNode(node, width, height, layer, viewFn, cache)
	case "divider":
		lines = renderDividerNode(width, height, layer)
	case "table":
		lines = renderTableNode(node, width, height)
	default:
		// Unknown type: treat as gameview fallback
		lines = renderGameViewNode(width, height, viewFn)
	}

	cache.put(hash, width, height, lines)
	return lines
}

// ─── Node Renderers ────────────────────────────────────────────────────

func renderGameViewNode(width, height int, viewFn func(w, h int) string) []string {
	if viewFn == nil {
		return emptyLines(width, height)
	}
	raw := viewFn(width, height)
	lines := strings.Split(raw, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func renderPanelNode(node *common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) []string {
	if width < 4 || height < 3 {
		return emptyLines(width, height)
	}

	oh, ov := layer.OH(), layer.OV()
	otl, otr := layer.OTL(), layer.OTR()
	obl, obr := layer.OBL(), layer.OBR()

	innerW := width - 2 // left border + right border
	innerH := height - 2 // top border + bottom border

	// Top border with optional title
	topBar := strings.Repeat(oh, innerW)
	if node.Title != "" {
		t := " " + node.Title + " "
		tLen := ansi.StringWidth(t)
		if tLen > innerW {
			t = t[:innerW]
			tLen = innerW
		}
		pad := (innerW - tLen) / 2
		topBar = strings.Repeat(oh, pad) + t + strings.Repeat(oh, innerW-pad-tLen)
	}
	result := []string{otl + topBar + otr}

	// Render children vertically within the panel interior
	var contentLines []string
	if len(node.Children) > 0 {
		contentLines = renderChildrenVertical(node.Children, innerW, innerH, layer, viewFn, cache)
	}

	// Pad/clip to innerH
	for len(contentLines) < innerH {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerH {
		contentLines = contentLines[:innerH]
	}
	for _, cl := range contentLines {
		result = append(result, ov+fitLine(cl, innerW)+ov)
	}

	// Bottom border
	result = append(result, obl+strings.Repeat(oh, innerW)+obr)
	return result
}

func renderLabelNode(node *common.WidgetNode, width, height int) []string {
	text := node.Text
	var line string
	switch node.Align {
	case "center":
		vis := ansi.StringWidth(text)
		if vis >= width {
			line = text
		} else {
			pad := (width - vis) / 2
			line = strings.Repeat(" ", pad) + text + strings.Repeat(" ", width-vis-pad)
		}
	case "right":
		vis := ansi.StringWidth(text)
		if vis >= width {
			line = text
		} else {
			line = strings.Repeat(" ", width-vis) + text
		}
	default: // left
		line = text
	}

	result := []string{fitLine(line, width)}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}

func renderHSplitNode(node *common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) []string {
	if len(node.Children) == 0 {
		return emptyLines(width, height)
	}

	// Calculate widths for each child
	childWidths := allocateSizes(node.Children, width, true)

	// Render each child
	var childColumns [][]string
	for i, child := range node.Children {
		cw := childWidths[i]
		cols := renderNode(child, cw, height, layer, viewFn, cache)
		childColumns = append(childColumns, cols)
	}

	// Merge columns side by side
	result := make([]string, height)
	for y := 0; y < height; y++ {
		var row strings.Builder
		for i, cols := range childColumns {
			cw := childWidths[i]
			if y < len(cols) {
				row.WriteString(fitLine(cols[y], cw))
			} else {
				row.WriteString(strings.Repeat(" ", cw))
			}
		}
		result[y] = row.String()
	}
	return result
}

func renderVSplitNode(node *common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) []string {
	if len(node.Children) == 0 {
		return emptyLines(width, height)
	}

	// Calculate heights for each child
	childHeights := allocateSizes(node.Children, height, false)

	// Render and stack vertically
	var result []string
	for i, child := range node.Children {
		ch := childHeights[i]
		lines := renderNode(child, width, ch, layer, viewFn, cache)
		result = append(result, lines...)
	}

	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	if len(result) > height {
		result = result[:height]
	}
	return result
}

func renderDividerNode(width, height int, layer *ThemeLayer) []string {
	line := strings.Repeat(layer.IH(), width)
	result := []string{line}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}

func renderTableNode(node *common.WidgetNode, width, height int) []string {
	if len(node.Rows) == 0 {
		return emptyLines(width, height)
	}

	// Calculate column widths
	cols := 0
	for _, row := range node.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	colWidths := make([]int, cols)
	for _, row := range node.Rows {
		for c, cell := range row {
			w := ansi.StringWidth(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	// Add 1 space between columns, then distribute remaining to last column
	totalUsed := 0
	for _, cw := range colWidths {
		totalUsed += cw
	}
	totalUsed += cols - 1 // spaces between columns

	var result []string
	for _, row := range node.Rows {
		var line strings.Builder
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			line.WriteString(fitLine(cell, colWidths[c]))
			if c < cols-1 {
				line.WriteByte(' ')
			}
		}
		result = append(result, fitLine(line.String(), width))
	}

	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	if len(result) > height {
		result = result[:height]
	}
	return result
}

// ─── Layout Helpers ────────────────────────────────────────────────────

// allocateSizes distributes `total` among children based on fixed sizes and weights.
// If horizontal=true, uses Width; otherwise uses Height.
func allocateSizes(children []*common.WidgetNode, total int, horizontal bool) []int {
	sizes := make([]int, len(children))
	remaining := total
	totalWeight := 0.0

	// First pass: allocate fixed sizes
	for i, child := range children {
		fixed := 0
		if horizontal {
			fixed = child.Width
		} else {
			fixed = child.Height
		}
		if fixed > 0 {
			sizes[i] = min(fixed, remaining)
			remaining -= sizes[i]
		} else {
			w := child.Weight
			if w <= 0 {
				w = 1 // default weight
			}
			totalWeight += w
		}
	}

	// Second pass: distribute remaining by weight
	if totalWeight > 0 && remaining > 0 {
		distributed := 0
		for i, child := range children {
			fixed := 0
			if horizontal {
				fixed = child.Width
			} else {
				fixed = child.Height
			}
			if fixed > 0 {
				continue
			}
			w := child.Weight
			if w <= 0 {
				w = 1
			}
			sizes[i] = int(float64(remaining) * w / totalWeight)
			distributed += sizes[i]
		}
		// Give remainder to first weighted child
		leftover := remaining - distributed
		for i, child := range children {
			fixed := 0
			if horizontal {
				fixed = child.Width
			} else {
				fixed = child.Height
			}
			if fixed == 0 {
				sizes[i] += leftover
				break
			}
		}
	}

	return sizes
}

// renderChildrenVertical stacks children vertically, auto-distributing height.
func renderChildrenVertical(children []*common.WidgetNode, width, height int, layer *ThemeLayer, viewFn func(w, h int) string, cache *ncRenderCache) []string {
	heights := allocateSizes(children, height, false)
	var result []string
	for i, child := range children {
		lines := renderNode(child, width, heights[i], layer, viewFn, cache)
		result = append(result, lines...)
	}
	return result
}

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

func emptyLines(width, height int) []string {
	blank := strings.Repeat(" ", width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = blank
	}
	return lines
}
