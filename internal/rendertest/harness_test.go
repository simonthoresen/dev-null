// Package rendertest provides golden-file render tests for the server console and
// player chrome views. Run with -update to regenerate expected outputs:
//
//	go test ./internal/rendertest/ -update
package rendertest

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"null-space/internal/chrome"
	"null-space/internal/console"
	"null-space/internal/domain"
	"null-space/internal/render"
	"null-space/internal/state"
)

var update = flag.Bool("update", false, "regenerate golden files instead of comparing")

// fixedTime is the deterministic wall-clock value used across all render tests.
var fixedTime = time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)

// Dynamic-value patterns shared by both unit and integration sanitisation.
var (
	tsPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

	// lobbyStatusBarPattern matches the entire lobby status bar line.
	// The line is: " null-space | N players | uptime T   DATE"
	// T and DATE have variable widths / values.
	lobbyStatusBarPattern = regexp.MustCompile(` null-space \| (\d+) players \| uptime \S+ +\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} *`)
)

// sanitize replaces wall-clock timestamps and uptime values with fixed
// placeholders so golden files remain stable across runs. Applied to both
// unit chrome output (where MockClock gives a known value) and integration
// output (where real time is used), making their golden files identical.
//
// The lobby status bar line is replaced atomically (including its interior
// padding) so that changes in uptime string length don't shift the timestamp.
// The playing view's timestamp-only status bar is handled by tsPattern.
func sanitize(s string) string {
	const fixedTimestamp = "XXXX-XX-XX XX:XX:XX" // 19 chars
	// Normalise the lobby status bar line atomically.
	out := lobbyStatusBarPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := lobbyStatusBarPattern.FindStringSubmatch(match)
		players := sub[1]
		left := fmt.Sprintf(" null-space | %s players | uptime XX", players)
		pad := termW - len(left) - len(fixedTimestamp) - 1
		if pad < 1 {
			pad = 1
		}
		return left + strings.Repeat(" ", pad) + fixedTimestamp
	})
	// Replace any remaining datetime stamps (e.g. playing view's status bar).
	out = tsPattern.ReplaceAllString(out, fixedTimestamp)
	return out
}

// ─── Mock console API ────────────────────────────────────────────────────────

type mockConsoleAPI struct {
	st     *state.CentralState
	clock  *domain.MockClock
	chatCh chan domain.Message
	slogCh chan console.SlogLine
}

func newMockConsoleAPI(st *state.CentralState) *mockConsoleAPI {
	return &mockConsoleAPI{
		st:     st,
		clock:  &domain.MockClock{T: fixedTime},
		chatCh: make(chan domain.Message),
		slogCh: make(chan console.SlogLine),
	}
}

func (a *mockConsoleAPI) State() *state.CentralState                        { return a.st }
func (a *mockConsoleAPI) Clock() domain.Clock                                { return a.clock }
func (a *mockConsoleAPI) DataDir() string                                    { return "" }
func (a *mockConsoleAPI) Uptime() string                                     { return "0s" }
func (a *mockConsoleAPI) BroadcastChat(msg domain.Message)                   {}
func (a *mockConsoleAPI) ChatCh() <-chan domain.Message                      { return a.chatCh }
func (a *mockConsoleAPI) SlogCh() <-chan console.SlogLine                    { return a.slogCh }
func (a *mockConsoleAPI) TabCandidates(string, []string) (string, []string) { return "", nil }
func (a *mockConsoleAPI) DispatchCommand(string, domain.CommandContext)      {}
func (a *mockConsoleAPI) SetConsoleWidth(int)                                {}

// ─── Mock chrome API ─────────────────────────────────────────────────────────

type mockChromeAPI struct {
	st    *state.CentralState
	clock *domain.MockClock
}

func newMockChromeAPI(st *state.CentralState) *mockChromeAPI {
	return &mockChromeAPI{
		st:    st,
		clock: &domain.MockClock{T: fixedTime},
	}
}

func (a *mockChromeAPI) State() *state.CentralState                        { return a.st }
func (a *mockChromeAPI) Clock() domain.Clock                                { return a.clock }
func (a *mockChromeAPI) DataDir() string                                    { return "" }
func (a *mockChromeAPI) Uptime() string                                     { return "0s" }
func (a *mockChromeAPI) BroadcastChat(domain.Message)                       {}
func (a *mockChromeAPI) BroadcastMsg(tea.Msg)                               {}
func (a *mockChromeAPI) SendToPlayer(string, tea.Msg)                       {}
func (a *mockChromeAPI) ServerLog(string)                                   {}
func (a *mockChromeAPI) TabCandidates(string, []string) (string, []string) { return "", nil }
func (a *mockChromeAPI) DispatchCommand(string, domain.CommandContext)      {}
func (a *mockChromeAPI) StartGame()                                         {}
func (a *mockChromeAPI) AcknowledgeGameOver(string)                         {}
func (a *mockChromeAPI) SuspendGame(string) error                           { return nil }
func (a *mockChromeAPI) ResumeGame(string, string) error                    { return nil }
func (a *mockChromeAPI) ListSuspends() []state.SuspendInfo                  { return nil }
func (a *mockChromeAPI) KickPlayer(string) error                            { return nil }

// ─── Mock game ───────────────────────────────────────────────────────────────

// mockGame is a minimal domain.Game that renders a fixed ASCII frame so that
// render tests don't depend on a real JS runtime.
type mockGame struct{}

func (g *mockGame) GameName() string                                              { return "Test Game" }
func (g *mockGame) TeamRange() domain.TeamRange                                  { return domain.TeamRange{} }
func (g *mockGame) Init(any)                                                      {}
func (g *mockGame) Start()                                                        {}
func (g *mockGame) Update(float64)                                                {}
func (g *mockGame) OnPlayerLeave(string)                                          {}
func (g *mockGame) OnInput(string, string)                                        {}
func (g *mockGame) StatusBar(string) string                                       { return "score: 42 | level: 3" }
func (g *mockGame) CommandBar(string) string                                      { return "" }
func (g *mockGame) Commands() []domain.Command                                    { return nil }
func (g *mockGame) Menus() []domain.MenuDef                                       { return nil }
func (g *mockGame) CharMap() *render.CharMapDef                                   { return nil }
func (g *mockGame) RenderCanvas(string, int, int) []byte                          { return nil }
func (g *mockGame) RenderCanvasImage(string, int, int) *image.RGBA                { return nil }
func (g *mockGame) HasCanvasMode() bool                                           { return false }
func (g *mockGame) Unload()                                                       {}
func (g *mockGame) State() any                                                    { return nil }
func (g *mockGame) SetState(any)                                                  {}
func (g *mockGame) GameSource() []domain.GameSourceFile                           { return nil }
func (g *mockGame) GameAssets() []domain.GameAsset                                { return nil }
func (g *mockGame) Layout(string, int, int) *domain.WidgetNode                   { return nil }

func (g *mockGame) Render(buf *render.ImageBuffer, _ string, x, y, w, h int) {
	// Draw a simple bordered box with fixed content.
	if w < 4 || h < 3 {
		return
	}
	buf.WriteString(x, y, "[ Test Game Output ]", nil, nil, 0)
	for row := 1; row < h-1; row++ {
		buf.WriteString(x, y+row, strings.Repeat(".", w), nil, nil, 0)
	}
	buf.WriteString(x, y+h-1, "[ game over: press enter ]", nil, nil, 0)
}

func (g *mockGame) RenderSplash(_ *render.ImageBuffer, _ string, _, _, _, _ int) bool {
	return false // use framework default
}

func (g *mockGame) RenderGameOver(_ *render.ImageBuffer, _ string, _, _, _, _ int, _ []domain.GameResult) bool {
	return false // use framework default
}

// ─── Golden file helpers ─────────────────────────────────────────────────────

// goldenPath returns the path for a golden file.
// All golden files live flat in testdata/golden/ as <scenario>_<kind>.txt.
func goldenPath(scenario, kind string) string {
	return filepath.Join("testdata", scenario+"_"+kind+".txt")
}

// checkOrUpdate either writes the golden file (when -update is set) or
// asserts that the current output matches it.
func checkOrUpdate(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file missing: %s\n  run with -update to generate it", path)
	}
	want := string(raw)
	if got != want {
		t.Errorf("render mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
			path, got, want)
	}
}

// stripRender strips all ANSI/OSC escape codes and returns the plain text.
func stripRender(s string) string {
	return ansi.Strip(s)
}

// normalizeScreen trims trailing spaces from every line and drops trailing
// blank lines — matching the output of vt100.String() used in integration
// tests, so unit and integration golden files are directly comparable.
func normalizeScreen(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// renderConsole creates a console model with the given API, sends a window-size
// message, applies any key sequences, and returns the ANSI-stripped render content.
func renderConsole(api console.ServerAPI, keys []string, profile colorprofile.Profile, w, h int) string {
	m := console.NewModel(api, func() {}, profile)
	cur, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		cur, _ = cur.Update(parseKey(k))
	}
	return normalizeScreen(stripRender(cur.View().Content))
}

// renderChrome creates a chrome model for the given player, applies variant
// settings, optionally marks it as active in-game, sends any extra key
// messages, then returns the sanitized ANSI-stripped render content.
//
// For lobby scenarios (inActiveGame=false), if playerID is not already in
// state, it is added automatically with a corresponding "[playerID] joined."
// system message — mirroring what the server does when an SSH client connects.
// This keeps unit and integration golden files identical.
func renderChrome(
	api chrome.ServerAPI,
	playerID string,
	inActiveGame bool,
	gameName string,
	keys []string,
	variant chromeVariant,
	profile colorprofile.Profile,
	w, h int,
) string {
	// Auto-join: simulate the player connecting if they're not yet in state.
	// Only for lobby scenarios; playing scenarios pre-populate state manually.
	if !inActiveGame {
		st := api.State()
		st.Lock()
		if _, exists := st.Players[playerID]; !exists {
			st.Players[playerID] = &domain.Player{
				ID:         playerID,
				Name:       playerID,
				TermWidth:  w,
				TermHeight: h,
			}
			st.ChatHistory = append(st.ChatHistory, domain.Message{
				Author: "",
				Text:   playerID + " joined.",
			})
		}
		st.Unlock()
	}

	m := chrome.NewModel(api, playerID)
	m.IsEnhancedClient = variant.isEnhancedClient
	m.IsTerminalClient = variant.isTerminalClient
	m.ColorProfile = profile

	cur, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if inActiveGame {
		cur, _ = cur.Update(domain.GameLoadedMsg{Name: gameName})
	}
	for _, k := range keys {
		cur, _ = cur.Update(parseKey(k))
	}
	return sanitize(normalizeScreen(stripRender(cur.View().Content)))
}

// parseKey converts a human-readable key name (e.g. "f10", "alt+h", "enter")
// into a tea.KeyPressMsg that the chrome model's Update method understands.
func parseKey(s string) tea.KeyPressMsg {
	switch s {
	case "f10":
		return tea.KeyPressMsg{Code: tea.KeyF10}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	if strings.HasPrefix(s, "alt+") && len(s) == 5 {
		return tea.KeyPressMsg{Mod: tea.ModAlt, Code: rune(s[4])}
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		return tea.KeyPressMsg{Mod: tea.ModCtrl, Code: rune(s[5])}
	}
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0])}
	}
	return tea.KeyPressMsg{Text: s}
}

// ─── Chrome variant definitions ──────────────────────────────────────────────

// chromeVariant describes an execution context for the player chrome.
type chromeVariant struct {
	// name is used as the sub-test name.
	name string
	// label is a comment shown in the golden file header.
	label            string
	isEnhancedClient bool
	isTerminalClient bool
}

// chromeVariants lists the four execution contexts the developer cares about,
// in order:
//
//	a) server --local (plain SSH pipe to local terminal)
//	b) server + plain ssh client (SSH pipe to remote terminal)
//	c) client --local (enhanced, terminal-mode client process)
//	d) server + client.exe (enhanced graphical client)
var chromeVariants = []chromeVariant{
	{
		name:             "server_local",
		label:            "a) server --local (SSH pipe to local terminal)",
		isEnhancedClient: false,
		isTerminalClient: false,
	},
	{
		name:             "server_ssh",
		label:            "b) server + plain SSH client (SSH pipe to remote terminal)",
		isEnhancedClient: false,
		isTerminalClient: false,
	},
	{
		name:             "client_local",
		label:            "c) client --local (enhanced terminal-mode client)",
		isEnhancedClient: true,
		isTerminalClient: true,
	},
	{
		name:             "client_remote",
		label:            "d) server + client.exe (enhanced graphical client)",
		isEnhancedClient: true,
		isTerminalClient: false,
	},
}

// colorVariants defines the two terminal color modes tested per variant.
// "ascii" uses the NoTTY profile (no escape codes at all).
// "ansi"  uses the TrueColor profile then strips codes — exercises the full
// ANSI serialization path while still yielding plain-text for comparison.
type colorVariantDef struct {
	name    string
	profile colorprofile.Profile
}

var colorVariants = []colorVariantDef{
	{name: "ascii", profile: colorprofile.NoTTY},
	{name: "ansi", profile: colorprofile.TrueColor},
}

// termW and termH are the fixed terminal dimensions used in all render tests.
const termW, termH = 80, 24
