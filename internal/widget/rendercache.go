package widget

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// RenderCached wraps a Control and caches its rendered pixels. If the hash
// matches the previous frame and the size hasn't changed, the cached pixels
// are blitted directly without calling the inner Control's Render.
type RenderCached struct {
	Inner   Control
	Hash    uint64
	cached  *render.ImageBuffer
	cachedW int
	cachedH int
	prevHash uint64
}

func (rc *RenderCached) Focusable() bool      { return rc.Inner.Focusable() }
func (rc *RenderCached) MinSize() (int, int)   { return rc.Inner.MinSize() }
func (rc *RenderCached) Update(msg tea.Msg)    { rc.Inner.Update(msg) }

// TabWant delegates to Inner if it implements TabWanter.
func (rc *RenderCached) TabWant() (bool, bool) {
	if tw, ok := rc.Inner.(TabWanter); ok {
		return tw.TabWant()
	}
	return false, false
}

func (rc *RenderCached) Render(buf *render.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	// Cache hit: hash matches, size matches, not focused (focus changes appearance).
	if rc.Hash != 0 && rc.Hash == rc.prevHash && !focused &&
		rc.cached != nil && rc.cachedW == width && rc.cachedH == height {
		buf.Blit(x, y, rc.cached)
		return
	}

	// Cache miss: render into a sub-buffer, then blit to main buffer and cache.
	if rc.Hash != 0 && !focused {
		sub := render.NewImageBuffer(width, height)
		rc.Inner.Render(sub, 0, 0, width, height, false, layer)
		buf.Blit(x, y, sub)
		rc.cached = sub
		rc.cachedW = width
		rc.cachedH = height
		rc.prevHash = rc.Hash
	} else {
		// Uncacheable (hash=0 or focused): render directly, invalidate cache.
		rc.Inner.Render(buf, x, y, width, height, focused, layer)
		rc.cached = nil
		rc.prevHash = 0
	}
}

// HandleClick delegates to Inner if it implements Clickable.
func (rc *RenderCached) HandleClick(rx, ry int) {
	if cl, ok := rc.Inner.(Clickable); ok {
		cl.HandleClick(rx, ry)
	}
}

// Ensure RenderCached implements TabWanter.
var _ TabWanter = (*RenderCached)(nil)
