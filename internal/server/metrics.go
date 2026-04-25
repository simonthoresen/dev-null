package server

import (
	"sync/atomic"
	"time"
)

// Metrics holds atomic counters for per-tick server-side timing.
// All durations are cumulative nanoseconds; rates are derived by the reader.
//
// Used by the profile-load harness to identify hot spots in the tick loop.
// Read via Server.MetricsSnapshot(). Counters are never reset — the harness
// snapshots before/after a measurement window and subtracts.
type metrics struct {
	tickCount         atomic.Uint64
	tickDurationNs    atomic.Uint64
	updateDurationNs  atomic.Uint64
	preRenderNs       atomic.Uint64
	snapshotNs        atomic.Uint64
	broadcastNs       atomic.Uint64
	canvasRenders     atomic.Uint64 // total RenderCanvasImage calls
	canvasRenderNs    atomic.Uint64
	viewCalls         atomic.Uint64 // total chrome View() calls
	viewNs            atomic.Uint64
	preRenderCanvases atomic.Uint64 // canvas pre-renders done in tick loop
	preRenderCanvasNs atomic.Uint64
}

// MetricsSnapshot is a point-in-time copy of all server metrics.
type MetricsSnapshot struct {
	TickCount         uint64
	TickDurationNs    uint64
	UpdateDurationNs  uint64
	PreRenderNs       uint64
	SnapshotNs        uint64
	BroadcastNs       uint64
	CanvasRenders     uint64
	CanvasRenderNs    uint64
	ViewCalls         uint64
	ViewNs            uint64
	PreRenderCanvases uint64
	PreRenderCanvasNs uint64
	At                time.Time
}

func (a *Server) MetricsSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		TickCount:         a.metrics.tickCount.Load(),
		TickDurationNs:    a.metrics.tickDurationNs.Load(),
		UpdateDurationNs:  a.metrics.updateDurationNs.Load(),
		PreRenderNs:       a.metrics.preRenderNs.Load(),
		SnapshotNs:        a.metrics.snapshotNs.Load(),
		BroadcastNs:       a.metrics.broadcastNs.Load(),
		CanvasRenders:     a.metrics.canvasRenders.Load(),
		CanvasRenderNs:    a.metrics.canvasRenderNs.Load(),
		ViewCalls:         a.metrics.viewCalls.Load(),
		ViewNs:            a.metrics.viewNs.Load(),
		PreRenderCanvases: a.metrics.preRenderCanvases.Load(),
		PreRenderCanvasNs: a.metrics.preRenderCanvasNs.Load(),
		At:                time.Now(),
	}
}

// CountCanvasRender adds one canvas-render observation. Called from chrome's
// per-frame canvas path so the harness can see how often per-player canvas
// renders happen (and their cost) without introducing a new dependency cycle.
func (a *Server) CountCanvasRender(d time.Duration) {
	a.metrics.canvasRenders.Add(1)
	a.metrics.canvasRenderNs.Add(uint64(d.Nanoseconds()))
}

// CountView adds one chrome-View observation.
func (a *Server) CountView(d time.Duration) {
	a.metrics.viewCalls.Add(1)
	a.metrics.viewNs.Add(uint64(d.Nanoseconds()))
}
