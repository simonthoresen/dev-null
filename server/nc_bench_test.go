package server

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/textinput"
)

// BenchmarkComplexNCRender benchmarks rendering a fairly complicated NC widget
// layout: a window with nested panels, text inputs, labels, buttons, checkboxes,
// dividers, and a text view — similar to what a real game UI might produce.
func BenchmarkComplexNCRender(b *testing.B) {
	theme := DefaultTheme()
	layer := theme.LayerAt(0)

	window := buildComplexWindow()

	// Sanity check: render once to make sure it doesn't panic.
	output := window.Render(0, 0, 120, 40, layer)
	if len(output) == 0 {
		b.Fatal("expected non-empty render output")
	}

	b.ResetTimer()
	for b.Loop() {
		_ = window.Render(0, 0, 120, 40, layer)
	}
}

// buildComplexWindow constructs a realistic NC widget layout with multiple
// control types arranged in a grid.
func buildComplexWindow() *NCWindow {
	// Header row: title label spanning full width
	headerLabel := &NCLabel{Text: "Game Dashboard — Round 3 of 10", Align: "center"}

	// Horizontal divider
	divider1 := &NCHDivider{Connected: true}

	// Left panel: player stats with labels and a text view
	statsLines := make([]string, 20)
	for i := range statsLines {
		statsLines[i] = fmt.Sprintf("Player %02d  HP: %3d  Score: %5d", i+1, 100-i*4, 1000+i*200)
	}
	statsView := &NCTextView{
		Lines:      statsLines,
		BottomAlign: false,
		Scrollable: true,
	}

	statsPanel := &NCPanel{
		Title: "Player Stats",
		Children: []GridChild{
			{
				Control:    &NCLabel{Text: "Leaderboard", Align: "center"},
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    &NCHDivider{Connected: true},
				Constraint: GridConstraint{Col: 0, Row: 1, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    statsView,
				Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 1, WeightY: 1, Fill: FillBoth},
			},
		},
	}

	// Right panel: controls — inputs, buttons, checkboxes
	nameInput := &NCTextInput{Model: newBenchTextInput("Enter name...")}
	chatInput := &NCTextInput{Model: newBenchTextInput("Type a message...")}

	controlsPanel := &NCPanel{
		Title: "Controls",
		Children: []GridChild{
			{
				Control:    &NCLabel{Text: "Name:", Align: "left"},
				Constraint: GridConstraint{Col: 0, Row: 0},
			},
			{
				Control:    nameInput,
				Constraint: GridConstraint{Col: 1, Row: 0, WeightX: 1, Fill: FillHorizontal},
				TabIndex:   1,
			},
			{
				Control:    &NCLabel{Text: "Chat:", Align: "left"},
				Constraint: GridConstraint{Col: 0, Row: 1},
			},
			{
				Control:    chatInput,
				Constraint: GridConstraint{Col: 1, Row: 1, WeightX: 1, Fill: FillHorizontal},
				TabIndex:   2,
			},
			{
				Control:    &NCHDivider{Connected: true},
				Constraint: GridConstraint{Col: 0, Row: 2, ColSpan: 2, WeightX: 1, Fill: FillHorizontal},
			},
			{
				Control:    &NCCheckbox{Label: "Ready", Checked: true},
				Constraint: GridConstraint{Col: 0, Row: 3, ColSpan: 2},
				TabIndex:   3,
			},
			{
				Control:    &NCCheckbox{Label: "Spectator Mode", Checked: false},
				Constraint: GridConstraint{Col: 0, Row: 4, ColSpan: 2},
				TabIndex:   4,
			},
			{
				Control:    &NCButton{Label: "Start Game"},
				Constraint: GridConstraint{Col: 0, Row: 5},
				TabIndex:   5,
			},
			{
				Control:    &NCButton{Label: "Leave"},
				Constraint: GridConstraint{Col: 1, Row: 5},
				TabIndex:   6,
			},
		},
	}

	// Vertical divider between panels
	vdivider := &NCVDivider{Connected: true}

	// Bottom section: chat log
	chatLines := make([]string, 30)
	for i := range chatLines {
		chatLines[i] = fmt.Sprintf("[%02d:%02d] Player%d: This is chat message number %d with some text", i/60, i%60, i%5+1, i+1)
	}
	chatView := &NCTextView{
		Lines:      chatLines,
		BottomAlign: true,
		Scrollable: true,
	}

	// Bottom divider
	divider2 := &NCHDivider{Connected: true}

	// Status bar labels
	statusLeft := &NCLabel{Text: "Game: Invaders | Teams: 3 | Phase: Playing", Align: "left"}
	statusRight := &NCLabel{Text: "Server time: 12:34:56", Align: "right"}

	// Assemble the full window
	return &NCWindow{
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
