package rendertest

import (
	"testing"

	"null-space/internal/state"
)

// TestChromeRenders runs each scenario through all four chrome execution
// contexts, each with two color modes (ascii / ansi). The four execution-context
// sub-tests for a given color mode all compare against the same golden file
// (testdata/<scenario>_chrome_<colorMode>.txt) because stripping ANSI/OSC
// produces identical plain text regardless of execution context.
//
// ascii and ansi golden files differ: monochrome terminals show ► cursor glyphs
// while color terminals rely on background highlight and use plain spaces.
//
// The four execution contexts mirror the real deployment paths:
//
//	a) server_local    — server --local (SSH pipe to local terminal)
//	b) server_ssh      — server + plain ssh client (SSH pipe to remote terminal)
//	c) client_local    — client --local (enhanced terminal-mode client process)
//	d) client_remote   — server + client.exe (enhanced graphical client)
func TestChromeRenders(t *testing.T) {
	for _, sc := range scenarios {
		if len(sc.consoleKeys) > 0 && len(sc.chromeKeys) == 0 {
			continue
		}
		t.Run(sc.name, func(t *testing.T) {
			for _, variant := range chromeVariants {
				t.Run(variant.name, func(t *testing.T) {
					for _, cv := range colorVariants {
						t.Run(cv.name, func(t *testing.T) {
							st := state.New("")
							sc.setup(st)

							api := newMockChromeAPI(st)
							playerID := sc.playerID
							if playerID == "" {
								playerID = "alice"
							}
							got := renderChrome(
								api,
								playerID,
								sc.inActiveGame,
								sc.gameName,
								sc.chromeKeys,
								variant,
								cv.profile,
								termW, termH,
							)

							path := goldenPath(sc.name, "chrome", cv.name)
							checkOrUpdate(t, path, got)
						})
					}
				})
			}
		})
	}
}
