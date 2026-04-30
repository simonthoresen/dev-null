package rendertest

import (
	"dev-null/internal/domain"
	"dev-null/internal/state"
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
	// dataDir is the data directory passed to the mock APIs. Defaults to "".
	// Use a fixtures directory when the scenario needs games or saves on disk.
	dataDir string
	// playerID is the player whose chrome view is rendered. Defaults to "alice".
	// Must be lowercase (matching the server's sanitizePlayerName behaviour).
	playerID string
	// inActiveGame sends a GameLoadedMsg to the chrome model so it enters
	// the playing/splash/game-over rendering path.
	inActiveGame bool
	// gameName is used in GameLoadedMsg when inActiveGame is true.
	gameName string
	// chromeKeys is a sequence of key names (e.g. "esc", "f", "enter") sent
	// only to the chrome model after initial setup, to drive chrome overlay/menu
	// state. Leave nil for state-only scenarios or console-targeted ones.
	chromeKeys []string
	// consoleKeys is a sequence of key names sent only to the console model after
	// initial setup, to drive console overlay/menu state. Leave nil for
	// state-only scenarios or chrome-targeted ones.
	consoleKeys []string
	// noIntegration skips this scenario in TestChromeRendersIntegration.
	// Use for scenarios that cannot be reproduced over a real SSH connection:
	// playing/splash (late joiners stay in lobby) and menu/dialog scenarios
	// (the integration harness does not send keyboard input).
	noIntegration bool
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
		// Esc from desktop focuses the menu bar on the first item (File),
		// with no dropdown yet. A second Esc returns to desktop.
		name:          "lobby_menu_focused",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc"},
		noIntegration: true,
	},
	{
		// Alt+H opens the Help dropdown (the only item is About...).
		name:          "lobby_help_menu_open",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc", "h"},
		noIntegration: true,
	},
	{
		// Alt+H then Enter activates the About item and opens the About dialog.
		name:          "lobby_about_dialog",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc", "h", "enter"},
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
		name:          "playing_starting",
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
			st.GamePhase = domain.PhaseStarting
			st.StartingReady = make(map[string]bool)
			st.StartingStart = fixedTime
			st.GameTeams = []domain.Team{{Name: "alice", Players: []string{"alice"}}}
		},
	},

	// ── Warning dialogs ────────────────────────────────────────────────────────
	{
		// Alt+F → down×8 → Enter: navigates chrome File menu to Exit.
		// File menu: Games, Saves, ---, Themes, Plugins, Shaders, Synths, Fonts, ---, Invite, ---, Exit
		// 8 downs from Games → Saves → Themes → Plugins → Shaders → Synths → Fonts → Invite → Exit
		name:          "chrome_disconnect_dialog",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc", "f", "down", "down", "down", "down", "down", "down", "down", "down", "enter"},
		noIntegration: true,
	},
	{
		// Esc → F → X: navigate the console's File menu to Exit, which
		// opens the shutdown confirmation dialog.
		name:          "console_shutdown_dialog",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		consoleKeys:   []string{"esc", "f", "x"},
		noIntegration: true,
	},

	// ── Games dialog ───────────────────────────────────────────────────────────
	{
		// Alt+F → Enter: open Games dialog with no games on disk.
		name:          "games_dialog_empty",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc", "f", "enter"},
		noIntegration: true,
	},
	{
		// Games dialog with populated list. alice is admin so Add visible.
		// cube is currently loaded (shown with → indicator).
		// cube supports 1-4 teams (compatible with 2 teams); orbits 2-8 (compatible).
		name:    "games_dialog_list",
		dataDir: "testdata/fixtures",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
			st.GameName = "core:cube"
			st.Teams = []domain.Team{
				{Name: "Red"},
				{Name: "Blue"},
			}
		},
		chromeKeys:    []string{"esc", "f", "enter"},
		noIntegration: true,
	},
	{
		// Games dialog with cursor navigated to second item via Down key.
		name:    "games_dialog_navigated",
		dataDir: "testdata/fixtures",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
			st.Teams = []domain.Team{
				{Name: "Red"},
				{Name: "Blue"},
			}
		},
		chromeKeys:    []string{"esc", "f", "enter", "down"},
		noIntegration: true,
	},
	{
		// Console Games dialog with no games on disk.
		name:          "games_dialog_empty_console",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		consoleKeys:   []string{"esc", "f", "enter"},
		noIntegration: true,
	},
	{
		// Console Games list with cursor on second item; Remove button visible.
		name:    "games_dialog_navigated_console",
		dataDir: "testdata/fixtures",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Teams = []domain.Team{
				{Name: "Red"},
				{Name: "Blue"},
			}
		},
		consoleKeys: []string{"esc", "f", "enter", "down"},
		noIntegration: true,
	},

	// ── Saves dialog ───────────────────────────────────────────────────────────
	{
		// Alt+F → down → Enter: open Saves dialog with no saves on disk.
		name:          "saves_dialog_empty",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		chromeKeys:    []string{"esc", "f", "down", "enter"},
		noIntegration: true,
	},
	{
		// Saves dialog listing orbits/autosave; alice is admin so Load visible.
		name:     "saves_dialog_list",
		dataDir:  "testdata/fixtures",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
		},
		chromeKeys:    []string{"esc", "f", "down", "enter"},
		noIntegration: true,
	},
	{
		// Saves list with autosave selected; Load button becomes visible.
		name:     "saves_dialog_selected",
		dataDir:  "testdata/fixtures",
		playerID: "alice",
		setup: func(st *state.CentralState) {
			st.Lock()
			defer st.Unlock()
			st.Players["alice"] = &domain.Player{
				ID: "alice", Name: "alice", IsAdmin: true,
				TermWidth: termW, TermHeight: termH,
			}
		},
		chromeKeys:    []string{"esc", "f", "down", "enter", "enter"},
		noIntegration: true,
	},
	{
		// Console Saves dialog with no saves on disk.
		name:          "saves_dialog_empty_console",
		playerID:      "alice",
		setup:         func(st *state.CentralState) {},
		consoleKeys:   []string{"esc", "f", "down", "enter"},
		noIntegration: true,
	},
	{
		// Console Saves list with autosave selected; Remove button becomes visible.
		name:     "saves_dialog_selected_console",
		dataDir:  "testdata/fixtures",
		playerID: "alice",
		setup:    func(st *state.CentralState) {},
		consoleKeys: []string{"esc", "f", "down", "enter", "enter"},
		noIntegration: true,
	},
}
