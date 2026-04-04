package widget

import (
	"flag"
	"fmt"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/colorprofile"

	"null-space/internal/render"
	"null-space/internal/theme"
)

var benchDuration = flag.Duration("nc.benchtime", 10*time.Second, "how long to run NC render benchmark")

// BenchmarkComplexNCRender benchmarks rendering a fairly complicated NC widget
// layout: a window with nested panels, text inputs, labels, buttons, checkboxes,
// dividers, and a text view — similar to what a real game UI might produce.
//
// Usage:
//
//	go test ./internal/widget/ -run='^$' -bench=BenchmarkComplexNCRender
//	go test ./internal/widget/ -run='^$' -bench=BenchmarkComplexNCRender -nc.benchtime=30s
//
// Performance history (AMD Ryzen 9 9950X, 120x40):
//
//	2026-04-01  827 µs/op
//	2026-04-01  121 µs/op  (cellbuf rewrite — 6.8x speedup)
//	2026-04-02   64 µs/op  (buffer reuse, zero-alloc SGR, cached theme colors — 1.9x speedup)
func BenchmarkComplexNCRender(b *testing.B) {
	th := theme.Default()
	layer := th.LayerAt(0)

	window := buildComplexWindow()

	const w, h = 120, 40
	buf := render.NewImageBuffer(w, h)

	// Sanity check: render once to make sure it doesn't panic.
	window.RenderToBuf(buf, 0, 0, w, h, layer)
	output := buf.ToString(colorprofile.TrueColor)
	if len(output) == 0 {
		b.Fatal("expected non-empty render output")
	}

	var iterations int
	deadline := time.Now().Add(*benchDuration)
	b.ResetTimer()
	for time.Now().Before(deadline) {
		buf.Clear()
		window.RenderToBuf(buf, 0, 0, w, h, layer)
		_ = buf.ToString(colorprofile.TrueColor)
		iterations++
	}
	b.StopTimer()
	elapsed := *benchDuration
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(iterations), "ns/op")
	b.Logf("%d iterations in %s (%.0f µs/op)", iterations, elapsed, float64(elapsed.Microseconds())/float64(iterations))
}

// buildComplexWindow constructs a realistic NC widget layout with multiple
// control types arranged in a grid.
func buildComplexWindow() *Window {
	// Header row: title label spanning full width
	headerLabel := &Label{Text: "Game Dashboard — Round 3 of 10", Align: "center"}

	// Horizontal divider
	divider1 := &HDivider{Connected: true}

	// Left panel: player stats with labels and a text view
	statsLines := make([]string, 20)
	for i := range statsLines {
		statsLines[i] = fmt.Sprintf("Player %02d  HP: %3d  Score: %5d", i+1, 100-i*4, 1000+i*200)
	}
	statsView := &TextView{
		Lines:       statsLines,
		BottomAlign: false,
		Scrollable:  true,
	}

	statsPanel := &Panel{
		Title: "Player Stats",
		Children: []GridChild{
			{
				Control:    &Label{Text: "Leaderboard", Align: "center"},
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    &HDivider{Connected: true},
				Constraint: GridConstraint{Col: 0, Row: 1, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    statsView,
				Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 1, WeightY: 1, Fill: FillBoth},
			},
		},
	}

	// Right panel: controls — inputs, buttons, checkboxes
	nameInput := &TextInput{Model: newBenchTextInput("Enter name...")}
	chatInput := &TextInput{Model: newBenchTextInput("Type a message...")}

	controlsPanel := &Panel{
		Title: "Controls",
		Children: []GridChild{
			{
				Control:    &Label{Text: "Name:", Align: "left"},
				Constraint: GridConstraint{Col: 0, Row: 0},
			},
			{
				Control:    nameInput,
				Constraint: GridConstraint{Col: 1, Row: 0, WeightX: 1, Fill: FillHorizontal},
				TabIndex:   1,
			},
			{
				Control:    &Label{Text: "Chat:", Align: "left"},
				Constraint: GridConstraint{Col: 0, Row: 1},
			},
			{
				Control:    chatInput,
				Constraint: GridConstraint{Col: 1, Row: 1, WeightX: 1, Fill: FillHorizontal},
				TabIndex:   2,
			},
			{
				Control:    &HDivider{Connected: true},
				Constraint: GridConstraint{Col: 0, Row: 2, ColSpan: 2, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    &Checkbox{Label: "Ready", Checked: true},
				Constraint: GridConstraint{Col: 0, Row: 3, ColSpan: 2},
				TabIndex:   3,
			},
			{
				Control:    &Checkbox{Label: "Spectator Mode", Checked: false},
				Constraint: GridConstraint{Col: 0, Row: 4, ColSpan: 2},
				TabIndex:   4,
			},
			{
				Control:    &Button{Label: "Start Game"},
				Constraint: GridConstraint{Col: 0, Row: 5},
				TabIndex:   5,
			},
			{
				Control:    &Button{Label: "Leave"},
				Constraint: GridConstraint{Col: 1, Row: 5},
				TabIndex:   6,
			},
		},
	}

	// Vertical divider between panels
	vdivider := &VDivider{Connected: true}

	// Bottom section: chat log
	chatLines := make([]string, 30)
	for i := range chatLines {
		chatLines[i] = fmt.Sprintf("[%02d:%02d] Player%d: This is chat message number %d with some text", i/60, i%60, i%5+1, i+1)
	}
	chatView := &TextView{
		Lines:       chatLines,
		BottomAlign: true,
		Scrollable:  true,
	}

	// Bottom divider
	divider2 := &HDivider{Connected: true}

	// Status bar labels
	statusLeft := &Label{Text: "Game: Invaders | Teams: 3 | Phase: Playing", Align: "left"}
	statusRight := &Label{Text: "Server time: 12:34:56", Align: "right"}

	// Assemble the full window
	return &Window{
		Title: "null-space",
		Children: []GridChild{
			// Row 0: header
			{
				Control:    headerLabel,
				Constraint: GridConstraint{Col: 0, Row: 0, ColSpan: 3, WeightX: 1, Fill: FillHorizontal},
			},
			// Row 1: divider
			{
				Control:    divider1,
				Constraint: GridConstraint{Col: 0, Row: 1, ColSpan: 3, WeightX: 1, Fill: FillHorizontal},
			},
			// Row 2: left panel | vdivider | right panel
			{
				Control:    statsPanel,
				Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 2, WeightY: 1, Fill: FillBoth},
			},
			{
				Control:    vdivider,
				Constraint: GridConstraint{Col: 1, Row: 2, Fill: FillVertical},
			},
			{
				Control:    controlsPanel,
				Constraint: GridConstraint{Col: 2, Row: 2, WeightX: 1, WeightY: 1, Fill: FillBoth},
			},
			// Row 3: divider
			{
				Control:    divider2,
				Constraint: GridConstraint{Col: 0, Row: 3, ColSpan: 3, WeightX: 1, Fill: FillHorizontal},
			},
			// Row 4: chat
			{
				Control:    chatView,
				Constraint: GridConstraint{Col: 0, Row: 4, ColSpan: 3, WeightX: 1, WeightY: 0.5, Fill: FillBoth},
			},
			// Row 5: status bar
			{
				Control:    statusLeft,
				Constraint: GridConstraint{Col: 0, Row: 5, ColSpan: 2, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    statusRight,
				Constraint: GridConstraint{Col: 2, Row: 5, Fill: FillHorizontal},
			},
		},
	}
}

func newBenchTextInput(placeholder string) *textinput.Model {
	m := new(textinput.Model)
	*m = textinput.New()
	m.Prompt = ""
	m.Placeholder = placeholder
	m.CharLimit = 256
	return m
}
