package rendertest

import (
	"testing"

	"null-space/internal/state"
)

// TestChromeRenders runs each scenario through all four chrome execution
// contexts, each with two color modes (ascii / ansi). All eight sub-tests
// compare against the same golden file at testdata/golden/<scenario>_chrome.txt
// because stripping ANSI/OSC produces identical plain text regardless of the
// execution context or color profile.
//
// The four execution contexts mirror the real deployment paths:
//
//	a) server_local    — server --local (SSH pipe to local terminal)
//	b) server_ssh      — server + plain ssh client (SSH pipe to remote terminal)
//	c) client_local    — client --local (enhanced terminal-mode client process)
//	d) client_remote   — server + client.exe (enhanced graphical client)
func TestChromeRenders(t *testing.T) {
	for _, sc := range scenarios {
		if sc.consoleOnly {
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
								sc.keys,
								variant,
								cv.profile,
								termW, termH,
							)

							path := goldenPath(sc.name, "chrome")
							checkOrUpdate(t, path, got)
						})
					}
				})
			}
		})
	}
}
