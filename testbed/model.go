package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

var flagFrames = flag.Int("frames", 0, "quit after N frames (0 = run until q/ctrl+c)")

// ANSI 256-color foreground codes for the 15 rows.
var rowColors = [15]int{
	196, 202, 208, 214, 220, // reds → oranges → yellow
	118, 46, 47, 48, 49,     // greens
	51, 45, 21, 57, 201,     // cyans → blues → magentas
}

// scrollMsg is the text that scrolls across the middle of the screen.
// Padded with spaces so the loop seam is invisible.
const scrollMsg = "    >>> SSH DELTA RENDER TEST — colors + leading-spaces + per-frame scroll OK <<<    "

// scrollWidth is the visible width of the marquee window (columns).
const scrollWidth = 60

type model struct{ frame int }

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case time.Time:
		m.frame++
		if *flagFrames > 0 && m.frame >= *flagFrames {
			return m, tea.Quit
		}
		return m, tick()
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders:
//   - A bold header with frame counter (changes every tick → delta repaints it)
//   - A blank line
//   - Rows 0–6   with colors and 0–6 leading spaces  (static)
//   - The scrolling marquee line                      (changes every tick)
//   - Rows 7–14  with colors and 7–14 leading spaces (static)
//   - A footer
//
// The static rows exercise that delta rendering leaves undisturbed content
// alone; the header and marquee exercise that only changed lines are repainted.
func (m model) View() tea.View {
	var b strings.Builder

	// Header — frame counter changes every tick.
	fmt.Fprintf(&b, "\x1b[1mSSH delta render test — frame %04d\x1b[0m\n\n", m.frame)

	// Top half of static rows (0–6).
	for i := 0; i < 7; i++ {
		indent := strings.Repeat(" ", i)
		content := strings.Repeat(string(rune('A'+i%26)), 40-i)
		fmt.Fprintf(&b, "%s\x1b[38;5;%dm[Row %02d] %s\x1b[0m\n",
			indent, rowColors[i], i, content)
	}

	// Scrolling marquee — advances 1 character left per frame.
	runes := []rune(scrollMsg)
	msgLen := len(runes)
	offset := m.frame % msgLen
	// Build a scrollWidth-wide window by doubling the message for wrap-around.
	doubled := append(runes, runes...)
	window := string(doubled[offset : offset+scrollWidth])
	fmt.Fprintf(&b, "\x1b[1;38;5;226m[ %-*s ]\x1b[0m\n", scrollWidth, window)

	// Bottom half of static rows (7–14).
	for i := 7; i < 15; i++ {
		indent := strings.Repeat(" ", i)
		content := strings.Repeat(string(rune('A'+i%26)), 40-i)
		fmt.Fprintf(&b, "%s\x1b[38;5;%dm[Row %02d] %s\x1b[0m\n",
			indent, rowColors[i], i, content)
	}

	fmt.Fprintf(&b, "\n(q=quit)\n")
	return tea.NewView(b.String())
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return t })
}
