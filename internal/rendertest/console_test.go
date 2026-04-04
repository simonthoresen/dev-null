package rendertest

import (
	"testing"

	"null-space/internal/state"
)

// TestConsoleRenders runs each scenario through the server console view and
// compares the stripped output against the golden file at
// testdata/golden/<scenario>_console.txt.
//
// Two color-mode subtests are run per scenario:
//   - ascii  — NoTTY profile: no escape codes in the output at all.
//   - ansi   — TrueColor profile: full ANSI emitted then stripped before comparison.
//
// Both subtests compare against the same golden file because stripping produces
// identical plain text regardless of the color profile used.
func TestConsoleRenders(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for _, cv := range colorVariants {
				t.Run(cv.name, func(t *testing.T) {
					st := state.New("")
					sc.setup(st)

					api := newMockConsoleAPI(st)
					got := renderConsole(api, cv.profile, termW, termH)

					path := goldenPath(sc.name, "console")
					checkOrUpdate(t, path, got)
				})
			}
		})
	}
}
