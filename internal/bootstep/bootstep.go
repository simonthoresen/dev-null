// Package bootstep provides "Label ..... [ DONE ]" boot-step output used
// during server and client startup. Width is read from DEV_NULL_TERM_WIDTH
// (set by the launcher script) or detected from the terminal.
package bootstep

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"
)

// statusTokenWidth is the fixed display width of every status token: "[ DONE ]" = 8.
const statusTokenWidth = 8

var (
	currentLabel string
	cachedWidth  int
	profile      = colorprofile.TrueColor
)

// Init reads DEV_NULL_TERM_WIDTH from the environment and sets the color
// profile from termFlag (empty = auto-detect from the terminal).
// Call once after flag parsing, before any Start/Finish calls.
func Init(termFlag string) {
	if s := os.Getenv("DEV_NULL_TERM_WIDTH"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 40 {
			cachedWidth = n
		}
	}
	profile = detectProfile(termFlag)
}

// Profile returns the color profile chosen by Init.
// Useful for passing to Bubble Tea or other UI components.
func Profile() colorprofile.Profile {
	return profile
}

// Start prints the step label with dots but no status.
// Finish must be called after the operation completes.
func Start(label string) {
	currentLabel = label
	w := termWidth()
	fmt.Printf("%s %s", label, strings.Repeat(".", dots(label, w)))
}

// Finish overwrites the current boot step line with the final status.
// Status should be "DONE", "FAIL", or "SKIP".
func Finish(status string) {
	w := termWidth()
	fmt.Printf("\r%s %s %s\n", currentLabel, strings.Repeat(".", dots(currentLabel, w)), colorToken(status))
}

func dots(label string, w int) int {
	// layout: label + " " + dots + " " + token
	n := w - len(label) - 1 - 1 - statusTokenWidth
	if n < 1 {
		n = 1
	}
	return n
}

func termWidth() int {
	if cachedWidth > 0 {
		return cachedWidth
	}
	w, _, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil || w < 40 {
		w = 80
	}
	cachedWidth = w
	return w
}

func colorToken(status string) string {
	const inner = 4
	pad := inner - len(status)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	right := pad - left
	plain := "[ " + strings.Repeat(" ", left) + status + strings.Repeat(" ", right) + " ]"

	if profile <= colorprofile.ASCII {
		return plain
	}
	var code string
	switch status {
	case "DONE":
		code = "\033[92m"
	case "FAIL":
		code = "\033[91m"
	case "SKIP":
		code = "\033[93m"
	default:
		return plain
	}
	return "[ " + strings.Repeat(" ", left) + code + status + "\033[0m" + strings.Repeat(" ", right) + " ]"
}

func detectProfile(termFlag string) colorprofile.Profile {
	if termFlag != "" {
		switch strings.ToLower(termFlag) {
		case "truecolor", "24bit":
			return colorprofile.TrueColor
		case "256color", "256":
			return colorprofile.ANSI256
		case "ansi", "16color", "16":
			return colorprofile.ANSI
		case "ascii", "none", "no-color":
			return colorprofile.ASCII
		default:
			fmt.Fprintf(os.Stderr, "unknown --term value %q (valid: truecolor, 256color, ansi, ascii)\n", termFlag)
		}
	}
	return colorprofile.Detect(os.Stderr, os.Environ())
}
