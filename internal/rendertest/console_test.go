package rendertest

import (
	"testing"

	"null-space/internal/domain"
	"null-space/internal/state"
)

// TestConsoleRenders runs each scenario through the server console view and
// compares the stripped output against the golden file at
// testdata/<scenario>_console.txt.
//
// Two color-mode subtests are run per scenario:
//   - ascii  — NoTTY profile: no escape codes in the output at all.
//   - ansi   — TrueColor profile: full ANSI emitted then stripped before comparison.
//
// Both subtests compare against the same golden file because stripping produces
// identical plain text regardless of the color profile used.
//
// Scenarios with keys defined apply those key sequences to the console model
// too, exercising the console's own overlay/menu system.
func TestConsoleRenders(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for _, cv := range colorVariants {
				t.Run(cv.name, func(t *testing.T) {
					st := state.New("")
					sc.setup(st)

					// Mirror renderChrome's auto-join: the console sees all
					// connected players, including the scenario's playerID.
					playerID := sc.playerID
					if playerID == "" {
						playerID = "alice"
					}
					st.Lock()
					if _, exists := st.Players[playerID]; !exists {
						st.Players[playerID] = &domain.Player{
							ID:         playerID,
							Name:       playerID,
							TermWidth:  termW,
							TermHeight: termH,
						}
					}
					st.Unlock()

					api := newMockConsoleAPI(st)
					got := renderConsole(api, sc.keys, cv.profile, termW, termH)

					path := goldenPath(sc.name, "console")
					checkOrUpdate(t, path, got)
				})
			}
		})
	}
}
