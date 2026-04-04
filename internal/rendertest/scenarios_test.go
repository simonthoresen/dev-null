package rendertest

import (
	"null-space/internal/domain"
	"null-space/internal/state"
)

// renderScenario describes a server state to render plus the chrome player context.
type renderScenario struct {
	// name is used as the sub-test name and as the golden file prefix.
	name string
	// setup configures the CentralState before rendering. It is called once
	// and the same state is shared for console, chrome, and integration tests.
	// setup must NOT add the playerID player — renderChrome (and the integration
	// SSH connection) adds the player automatically, so the resulting state and
	// any "joined" chat messages match between unit and integration runs.
	setup func(st *state.CentralState)
	// playerID is the player whose chrome view is rendered. Defaults to "alice".
	// Must be lowercase (matching the server's sanitizePlayerName behaviour).
	playerID string
	// inActiveGame sends a GameLoadedMsg to the chrome model so it enters
	// the playing/splash/game-over rendering path.
	inActiveGame bool
	// gameName is used in GameLoadedMsg when inActiveGame is true.
	gameName string
	// keys is a sequence of key names (e.g. "f10", "alt+h", "enter") sent to
	// both the console and chrome models after initial setup to drive
	// overlay/menu state. Both models share the same widget.OverlayState key
	// handling, so the same sequences work for both.
	keys []string
	// noIntegration skips this scenario in TestChromeRendersIntegration.
	// Use for scenarios that cannot be reproduced over a real SSH connection:
	// playing/splash (late joiners stay in lobby) and menu/dialog scenarios
	// (the integration harness does not send keyboard input).
	noIntegration bool
	// consoleOnly skips this scenario in TestChromeRenders. Use for scenarios
	// that exercise console-specific UI (e.g. the shutdown confirmation dialog).
	consoleOnly bool
}

// scenarios is the curated eval set. Edit this list to add, remove, or tweak
// render test cases. Run `go test ./internal/rendertest/ -update` to
// regenerate the golden files after changing state or expected layout.
//
// Naming conventions:
//   - Player names are lowercase, matching the server's sanitizePlayerName output.
//   - setup() must never add the scenario's playerID; renderChrome does it.
var scenarios = []renderScenario{
	// ── Lobby ──────────────────────────────────────────────────────────────
	{
		name:     "lobby_empty",
		playerID: "alice",
		setup:    func(st *state.CentralState) {},
	},
	{
		// Two teams with known player IDs.  The connecting player (alice) is
		// intentionally left in Unassigned so that unit and integration tests
		// produce identical output: in integration alice gets a session-hash ID
		// that doesn't match any team's Players list.
		name:     "lobby_two_players_two_teams",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["bob"] = &domain.Player{
				ID: "bob", Name: "bob",
				TermWidth: termW, TermHeight: termH,
			}
			st.Teams = []domain.Team{
				{Name: "Red", Color: "#ff5555"},
				{Name: "Blue", Color: "#5555ff", Players: []string{"bob"}},
			}
		},
	},
	{
		name:     "lobby_chat_history",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.ChatHistory = []domain.Message{
				{Author: "", Text: "Server started."},
				{Author: "alice", Text: "hello world"},
				{Author: "alice", Text: "/help"},
				{Author: "", Text: "Available commands: /help /plugin /theme /shader", IsReply: true},
			}
		},
	},

	// ── Lobby: menu navigation ─────────────────────────────────────────────
	{
		// F10 focuses the menu bar on the first item (File), no dropdown yet.
		name:          "lobby_menu_focused",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		keys:    []string{"f10"},
		noIntegration: true,
	},
	{
		// Alt+H opens the Help dropdown (the only item is About...).
		name:          "lobby_help_menu_open",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		keys:    []string{"alt+h"},
		noIntegration: true,
	},
	{
		// Alt+H then Enter activates the About item and opens the About dialog.
		name:          "lobby_about_dialog",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		keys:    []string{"alt+h", "enter"},
		noIntegration: true,
	},

	// ── Playing ────────────────────────────────────────────────────────────
	// Playing scenarios use noIntegration: true because late-joining SSH
	// clients remain in the lobby rather than receiving the playing view.
	{
		name:          "playing_game",
		playerID:      "alice",
		inActiveGame:  true,
		gameName:      "testgame",
		noIntegration: true,
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
			st.Players["bob"] = &domain.Player{
				ID: "bob", Name: "bob",
				TermWidth: termW, TermHeight: termH,
			}
			st.ActiveGame = &mockGame{}
			st.GameName = "testgame"
			st.GamePhase = domain.PhasePlaying
		},
	},
	{
		name:          "playing_splash",
		playerID:      "alice",
		inActiveGame:  true,
		gameName:      "testgame",
		noIntegration: true,
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
			st.ActiveGame = &mockGame{}
			st.GameName = "testgame"
			st.GamePhase = domain.PhaseSplash
		},
	},

	// ── Console-only ───────────────────────────────────────────────────────────
	{
		// Ctrl+Q triggers the shutdown confirmation dialog in the console.
		name:          "console_shutdown_dialog",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		keys:          []string{"ctrl+q"},
		noIntegration: true,
		consoleOnly:   true,
	},
}
