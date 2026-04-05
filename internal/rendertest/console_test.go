package rendertest

import (
	"testing"

	"dev-null/internal/domain"
	"dev-null/internal/state"
)

// TestConsoleRenders runs each scenario through the server console view and
// compares the stripped output against testdata/<scenario>_console_<colorMode>.txt.
//
// Two color-mode subtests are run per scenario:
//   - ascii  — NoTTY profile: monochrome; cursor glyphs (►) are shown.
//   - ansi   — TrueColor profile: color; background highlight replaces glyphs.
//
// The two files differ wherever a ► cursor glyph would appear.
//
// Scenarios with keys defined apply those key sequences to the console model
// too, exercising the console's own overlay/menu system.
func TestConsoleRenders(t *testing.T) {
	for _, sc := range scenarios {
		if len(sc.chromeKeys) > 0 && len(sc.consoleKeys) == 0 {
			continue
		}
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
					got := renderConsole(api, sc.consoleKeys, cv.profile, termW, termH)

					path := goldenPath(sc.name, "console", cv.name)
					checkOrUpdate(t, path, got)
				})
			}
		})
	}
}
