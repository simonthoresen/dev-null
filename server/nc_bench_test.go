package server

import (
	"flag"
	"fmt"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"

	"null-space/internal/theme"
	"null-space/internal/widget"
)

var benchDuration = flag.Duration("nc.benchtime", 10*time.Second, "how long to run NC render benchmark")

// BenchmarkComplexNCRender benchmarks rendering a fairly complicated NC widget
// layout: a window with nested panels, text inputs, labels, buttons, checkboxes,
// dividers, and a text view — similar to what a real game UI might produce.
//
// Usage:
//
//	go test ./server/ -run='^$' -bench=BenchmarkComplexNCRender
//	go test ./server/ -run='^$' -bench=BenchmarkComplexNCRender -nc.benchtime=30s
//
// Performance history (AMD Ryzen 9 9950X, 120x40):
//
//	2026-04-01  827 µs/op
//	2026-04-01  121 µs/op  (cellbuf rewrite — 6.8x speedup)
func BenchmarkComplexNCRender(b *testing.B) {
	theme := theme.Default()
	layer := theme.LayerAt(0)

	window := buildComplexWindow()

	// Sanity check: render once to make sure it doesn't panic.
	output := window.Render(0, 0, 120, 40, layer)
	if len(output) == 0 {
		b.Fatal("expected non-empty render output")
	}

	var iterations int
	deadline := time.Now().Add(*benchDuration)
	b.ResetTimer()
	for time.Now().Before(deadline) {
		_ = window.Render(0, 0, 120, 40, layer)
		iterations++
	}
	b.StopTimer()
	elapsed := *benchDuration
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(iterations), "ns/op")
	b.Logf("%d iterations in %s (%.0f µs/op)", iterations, elapsed, float64(elapsed.Microseconds())/float64(iterations))
}

// buildComplexWindow constructs a realistic NC widget layout with multiple
// control types arranged in a grid.
func buildComplexWindow() *widget.Window {
	// Header row: title label spanning full width
	headerLabel := &widget.Label{Text: "Game Dashboard — Round 3 of 10", Align: "center"}

	// Horizontal divider
	divider1 := &widget.HDivider{Connected: true}

	// Left panel: player stats with labels and a text view
	statsLines := make([]string, 20)
	for i := range statsLines {
		statsLines[i] = fmt.Sprintf("Player %02d  HP: %3d  Score: %5d", i+1, 100-i*4, 1000+i*200)
	}
	statsView := &widget.TextView{
		Lines:       statsLines,
		BottomAlign: false,
		Scrollable:  true,
	}

	statsPanel := &widget.Panel{
		Title: "Player Stats",
		Children: []widget.GridChild{
			{
				Control:    &widget.Label{Text: "Leaderboard", Align: "center"},
				Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal},
			},
			{
				Control:    &widget.HDivider{Connected: true},
				Constraint: widget.GridConstraint{Col: 0, Row: 1, WeightX: 1, Fill: widget.FillHorizontal},
			},
			{
				Control:    statsView,
				Constraint: widget.GridConstraint{Col: 0, Row: 2, WeightX: 1, WeightY: 1, Fill: widget.FillBoth},
			},
		},
	}

	// Right panel: controls — inputs, buttons, checkboxes
	nameInput := &widget.TextInput{Model: newBenchTextInput("Enter name...")}
	chatInput := &widget.TextInput{Model: newBenchTextInput("Type a message...")}

	controlsPanel := &widget.Panel{
		Title: "Controls",
		Children: []widget.GridChild{
			{
				Control:    &widget.Label{Text: "Name:", Align: "left"},
				Constraint: widget.GridConstraint{Col: 0, Row: 0},
			},
			{
				Control:    nameInput,
				Constraint: widget.GridConstraint{Col: 1, Row: 0, WeightX: 1, Fill: widget.FillHorizontal},
				TabIndex:   1,
			},
			{
				Control:    &widget.Label{Text: "Chat:", Align: "left"},
				Constraint: widget.GridConstraint{Col: 0, Row: 1},
			},
			{
				Control:    chatInput,
				Constraint: widget.GridConstraint{Col: 1, Row: 1, WeightX: 1, Fill: widget.FillHorizontal},
				TabIndex:   2,
			},
			{
				Control:    &widget.HDivider{Connected: true},
				Constraint: widget.GridConstraint{Col: 0, Row: 2, ColSpan: 2, WeightX: 1, Fill: widget.FillHorizontal},
			},
			{
				Control:    &widget.Checkbox{Label: "Ready", Checked: true},
				Constraint: widget.GridConstraint{Col: 0, Row: 3, ColSpan: 2},
				TabIndex:   3,
			},
			{
				Control:    &widget.Checkbox{Label: "Spectator Mode", Checked: false},
				Constraint: widget.GridConstraint{Col: 0, Row: 4, ColSpan: 2},
				TabIndex:   4,
			},
			{
				Control:    &widget.Button{Label: "Start Game"},
				Constraint: widget.GridConstraint{Col: 0, Row: 5},
				TabIndex:   5,
			},
			{
				Control:    &widget.Button{Label: "Leave"},
				Constraint: widget.GridConstraint{Col: 1, Row: 5},
				TabIndex:   6,
			},
		},
	}

	// Vertical divider between panels
	vdivider := &widget.VDivider{Connected: true}

	// Bottom section: chat log
	chatLines := make([]string, 30)
	for i := range chatLines {
		chatLines[i] = fmt.Sprintf("[%02d:%02d] Player%d: This is chat message number %d with some text", i/60, i%60, i%5+1, i+1)
	}
	chatView := &widget.TextView{
		Lines:       chatLines,
		BottomAlign: true,
		Scrollable:  true,
	}

	// Bottom divider
	divider2 := &widget.HDivider{Connected: true}

	// Status bar labels
	statusLeft := &widget.Label{Text: "Game: Invaders | Teams: 3 | Phase: Playing", Align: "left"}
	statusRight := &widget.Label{Text: "Server time: 12:34:56", Align: "right"}

	// Assemble the full window
	return &widget.Window{
		Title: "null-space",
		Children: []widget.GridChild{
			// Row 0: header
			{
				Control:    headerLabel,
				Constraint: widget.GridConstraint{Col: 0, Row: 0, ColSpan: 3, WeightX: 1, Fill: widget.FillHorizontal},
			},
			// Row 1: divider
			{
				Control:    divider1,
				Constraint: widget.GridConstraint{Col: 0, Row: 1, ColSpan: 3, WeightX: 1, Fill: widget.FillHorizontal},
			},
			// Row 2: left panel | vdivider | right panel
			{
				Control:    statsPanel,
				Constraint: widget.GridConstraint{Col: 0, Row: 2, WeightX: 2, WeightY: 1, Fill: widget.FillBoth},
			},
			{
				Control:    vdivider,
				Constraint: widget.GridConstraint{Col: 1, Row: 2, Fill: widget.FillVertical},
			},
			{
				Control:    controlsPanel,
				Constraint: widget.GridConstraint{Col: 2, Row: 2, WeightX: 1, WeightY: 1, Fill: widget.FillBoth},
			},
			// Row 3: divider
			{
				Control:    divider2,
				Constraint: widget.GridConstraint{Col: 0, Row: 3, ColSpan: 3, WeightX: 1, Fill: widget.FillHorizontal},
			},
			// Row 4: chat
			{
				Control:    chatView,
				Constraint: widget.GridConstraint{Col: 0, Row: 4, ColSpan: 3, WeightX: 1, WeightY: 0.5, Fill: widget.FillBoth},
			},
			// Row 5: status bar
			{
				Control:    statusLeft,
				Constraint: widget.GridConstraint{Col: 0, Row: 5, ColSpan: 2, WeightX: 1, Fill: widget.FillHorizontal},
			},
			{
				Control:    statusRight,
				Constraint: widget.GridConstraint{Col: 2, Row: 5, Fill: widget.FillHorizontal},
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
