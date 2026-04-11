package engine

// Game render eval-file harness.
//
// Each file in testdata/*.txt describes a set of render tests for one game
// rendering mode. The format inside each file is:
//
//	=== test_name
//	# comment (preserved in rewrites)
//	mode:      text|canvas|layout
//	width:     <int>              (default 20)
//	height:    <int>              (default 5)
//	player_id: <string>           (default "player1")
//	--- js
//	#import "shared.js"           (optional — inlines a file from testdata/)
//	<javascript game source>
//	---
//	===
//	<expected output — ANSI stripped, trailing spaces trimmed per line>
//
//	=== next_test
//	...
//
// Multi-line values use a block syntax: "--- key" opens a block and "---"
// closes it. The block content is stored in the lists map under that key.
//
// JS blocks may contain "#import" directives on their own line:
//
//	#import "filename.js"
//
// The named file is read from the same testdata/ directory as the eval file
// and its contents are inlined at that position. Only simple filenames are
// allowed (no path separators or "..").
//
// Mode semantics:
//
//	text   — calls game.RenderAscii(buf, playerID, 0, 0, w, h); compare ANSI-stripped buf
//	canvas — calls game.RenderCanvasImage(playerID, w*2, h*2); converts to quadrant
//	         block chars via render.ImageToQuadrants; compare ANSI-stripped result
//	layout — calls game.Layout(playerID, w, h); reconciles via
//	         widget.ReconcileGameWindow; renders gw.Window.RenderToBuf; compare
//
// All modes wrap their output in a Window border via ReconcileGameWindow.
//
// Run with -update-engine to regenerate all expected outputs:
//
//	go test ./internal/engine/ -run TestGameRenderHarness -update-engine

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/domain"
	"dev-null/internal/render"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

var updateEngine = flag.Bool("update-engine", false, "regenerate game render eval file golden outputs")

// ─── Data structures ─────────────────────────────────────────────────────────

type gameCase struct {
	name        string
	configRaw   string // raw config text (preserved for -update rewrites)
	props       map[string]string
	blocks      map[string]string // multi-line blocks (e.g. "js")
	expected    string
	hasExpected bool // true once the === separator has been seen
}

// ─── Parser ──────────────────────────────────────────────────────────────────

// parseGameEvalFile splits an eval file into test cases.
// It extends the widget harness format with "--- key" / "---" blocks for
// multi-line values (e.g. embedded JavaScript).
func parseGameEvalFile(data string) []gameCase {
	var cases []gameCase

	type parseState int
	const (
		stateNone     parseState = iota
		stateConfig              // reading game config
		stateBlock               // inside a --- key / --- block
		stateExpected            // reading expected output
	)

	state := stateNone
	var cur *gameCase
	var configLines []string
	var expectedLines []string
	var blockKey string
	var blockLines []string

	closeBlock := func() {
		if cur == nil || blockKey == "" {
			return
		}
		// Store block content.
		cur.blocks[blockKey] = strings.Join(blockLines, "\n")
		// Also record block lines + closing "---" in configRaw so the rewriter
		// can reproduce the original config section faithfully.
		configLines = append(configLines, blockLines...)
		configLines = append(configLines, "---")
		blockKey = ""
		blockLines = nil
		state = stateConfig
	}

	flush := func() {
		if cur == nil {
			return
		}
		closeBlock()
		cur.configRaw = strings.Join(configLines, "\n")
		exp := strings.Join(expectedLines, "\n")
		cur.expected = strings.TrimRight(exp, "\n")
		cases = append(cases, *cur)
		cur = nil
		configLines = nil
		expectedLines = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "=== ") {
			// Start of a new test block.
			flush()
			state = stateConfig
			cur = &gameCase{
				name:   strings.TrimPrefix(line, "=== "),
				props:  make(map[string]string),
				blocks: make(map[string]string),
			}
		} else if line == "===" {
			// Separator between config and expected.
			closeBlock()
			state = stateExpected
			if cur != nil {
				cur.hasExpected = true
			}
		} else if state == stateBlock {
			if line == "---" {
				closeBlock()
			} else {
				blockLines = append(blockLines, line)
			}
		} else if state == stateConfig {
			if strings.HasPrefix(line, "#") {
				configLines = append(configLines, line)
				continue
			}
			configLines = append(configLines, line)
			// Check for block opener: "--- key"
			if strings.HasPrefix(line, "--- ") {
				blockKey = strings.TrimPrefix(line, "--- ")
				blockLines = nil
				state = stateBlock
				continue
			}
			idx := strings.Index(line, ":")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			cur.props[key] = val
		} else if state == stateExpected {
			expectedLines = append(expectedLines, line)
		}
	}
	flush()
	return cases
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func gamePropStr(props map[string]string, key, def string) string {
	if v, ok := props[key]; ok {
		return v
	}
	return def
}

func gamePropInt(props map[string]string, key string, def int) int {
	if v, ok := props[key]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return def
}

func engineTestLayer() *theme.Layer {
	return theme.Default().LayerAt(0)
}

// ─── Import resolution ───────────────────────────────────────────────────────

// resolveImports scans js for "#import" directives and inlines the named files.
// Each directive must appear on its own line as:
//
//	#import "filename.js"
//
// The file is read from baseDir. Only simple filenames (no "/" or "..") are
// permitted to prevent path traversal.
func resolveImports(js, baseDir string) (string, error) {
	var sb strings.Builder
	for _, line := range strings.Split(js, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#import ") {
			sb.WriteString(line)
			sb.WriteByte('\n')
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "#import "))
		if len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' {
			return "", fmt.Errorf("malformed #import (expected quoted filename): %q", line)
		}
		name := rest[1 : len(rest)-1]
		if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
			return "", fmt.Errorf("#import %q: only simple filenames are allowed", name)
		}
		content, err := os.ReadFile(filepath.Join(baseDir, name))
		if err != nil {
			return "", fmt.Errorf("#import %q: %w", name, err)
		}
		sb.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), nil
}

// ─── Rendering ───────────────────────────────────────────────────────────────

// renderGameCase renders tc and returns normalised plain-text output.
// baseDir is the directory containing the eval file; #import paths are resolved
// relative to it.
func renderGameCase(t *testing.T, tc gameCase, profile colorprofile.Profile, baseDir string) string {
	t.Helper()

	js, ok := tc.blocks["js"]
	if !ok || strings.TrimSpace(js) == "" {
		t.Fatalf("test case %q: missing --- js block", tc.name)
	}

	var err error
	js, err = resolveImports(js, baseDir)
	if err != nil {
		t.Fatalf("test case %q: %v", tc.name, err)
	}

	w := gamePropInt(tc.props, "width", 20)
	h := gamePropInt(tc.props, "height", 5)
	playerID := gamePropStr(tc.props, "player_id", "player1")
	mode := strings.TrimSpace(tc.props["mode"])

	// Write JS to a temp file so LoadGame can load it.
	dir := t.TempDir()
	jsPath := filepath.Join(dir, "game.js")
	if err := os.WriteFile(jsPath, []byte(js), 0o644); err != nil {
		t.Fatalf("write temp JS: %v", err)
	}

	chatCh := make(chan domain.Message, 8)
	game, err := LoadGame(jsPath, func(string) {}, chatCh, domain.RealClock{}, dir)
	if err != nil {
		t.Fatalf("LoadGame: %v", err)
	}
	defer game.Unload()

	game.Load(nil)
	game.Begin()

	// All modes wrap their render output in a GameWindow so the expected
	// output includes the Window border, matching the in-game presentation.
	var renderFn func(rbuf *render.ImageBuffer, rx, ry, rw, rh int)
	var tree *domain.WidgetNode

	switch mode {
	case "text":
		renderFn = func(rbuf *render.ImageBuffer, rx, ry, rw, rh int) {
			game.RenderAscii(rbuf, playerID, rx, ry, rw, rh)
		}
		tree = &domain.WidgetNode{Type: "gameview"}

	case "canvas":
		renderFn = func(rbuf *render.ImageBuffer, rx, ry, rw, rh int) {
			img := game.RenderCanvasImage(playerID, rw*2, rh*4)
			if img == nil {
				return
			}
			render.ImageToQuadrants(img, rbuf, rx, ry, rw, rh)
		}
		tree = &domain.WidgetNode{Type: "gameview"}

	case "layout":
		tree = game.Layout(playerID, w, h)
		if tree == nil {
			t.Fatalf("test case %q: Layout returned nil (game may not define layout hook)", tc.name)
		}
		renderFn = func(rbuf *render.ImageBuffer, rx, ry, rw, rh int) {
			game.RenderAscii(rbuf, playerID, rx, ry, rw, rh)
		}

	default:
		t.Fatalf("test case %q: unknown mode %q (want text, canvas, or layout)", tc.name, mode)
		return ""
	}

	gw := widget.ReconcileGameWindow(nil, tree, renderFn, nil)
	buf := render.NewImageBuffer(w, h)
	gw.Window.RenderToBuf(buf, 0, 0, w, h, engineTestLayer())
	return normaliseGameOutput(buf.ToString(profile))
}

// normaliseGameOutput strips ANSI codes and trims trailing spaces per line
// and trailing blank lines.
func normaliseGameOutput(s string) string {
	stripped := ansi.Strip(s)
	lines := strings.Split(stripped, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// ─── File rewriter ───────────────────────────────────────────────────────────

func rewriteGameEvalFile(path string, cases []gameCase) error {
	var sb strings.Builder
	for i, c := range cases {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("=== " + c.name + "\n")
		if c.configRaw != "" {
			sb.WriteString(c.configRaw + "\n")
		}
		sb.WriteString("===\n")
		if c.expected != "" {
			sb.WriteString(c.expected + "\n")
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// ─── Test entry point ────────────────────────────────────────────────────────

func TestGameRenderHarness(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no eval files in testdata/")
	}

	for _, f := range files {
		f := f
		scenarioName := strings.TrimSuffix(filepath.Base(f), ".txt")
		t.Run(scenarioName, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			cases := parseGameEvalFile(string(data))
			if len(cases) == 0 {
				t.Skip("no test cases found")
			}

			baseDir := filepath.Dir(f)
			updated := false
			for i, tc := range cases {
				tc := tc
				i := i
				t.Run(tc.name, func(t *testing.T) {
					for _, profile := range []colorprofile.Profile{
						colorprofile.TrueColor,
						colorprofile.NoTTY,
					} {
						got := renderGameCase(t, tc, profile, baseDir)
						if *updateEngine {
							cases[i].expected = got
							cases[i].hasExpected = true
							updated = true
							return
						}
						if !tc.hasExpected {
							t.Fatalf("no expected output; run with -update-engine to generate")
						}
						if got != tc.expected {
							t.Errorf("profile %v mismatch\ngot:\n%s\n\nwant:\n%s",
								profile, got, tc.expected)
						}
					}
				})
			}

			if updated {
				if err := rewriteGameEvalFile(f, cases); err != nil {
					t.Fatalf("failed to rewrite eval file: %v", err)
				}
			}
		})
	}
}

// ─── Parser edge-case tests ──────────────────────────────────────────────────

func TestParseGameEvalFile_Basic(t *testing.T) {
	input := `=== hello
mode: text
width: 10
height: 3
--- js
var Game = { load: function(){}, renderAscii: function(buf){} };
---
===
hello
`
	cases := parseGameEvalFile(input)
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	c := cases[0]
	if c.name != "hello" {
		t.Errorf("name: got %q", c.name)
	}
	if c.props["mode"] != "text" {
		t.Errorf("mode: got %q", c.props["mode"])
	}
	if !strings.Contains(c.blocks["js"], "var Game") {
		t.Errorf("js block: got %q", c.blocks["js"])
	}
	if !c.hasExpected {
		t.Error("hasExpected should be true")
	}
	if c.expected != "hello" {
		t.Errorf("expected: got %q", c.expected)
	}
}

func TestParseGameEvalFile_MultipleBlocks(t *testing.T) {
	input := `=== a
mode: canvas
--- js
function one() {}
---
===

=== b
mode: layout
--- js
function two() {}
---
===
`
	cases := parseGameEvalFile(input)
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	if cases[0].name != "a" || cases[1].name != "b" {
		t.Errorf("names: %q, %q", cases[0].name, cases[1].name)
	}
	if !strings.Contains(cases[0].blocks["js"], "one") {
		t.Errorf("case a js: %q", cases[0].blocks["js"])
	}
	if !strings.Contains(cases[1].blocks["js"], "two") {
		t.Errorf("case b js: %q", cases[1].blocks["js"])
	}
}

// Ensure the fmt import is used.
var _ = fmt.Sprintf
