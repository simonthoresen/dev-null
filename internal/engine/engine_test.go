package engine

import (
	"os"
	"path/filepath"
	"testing"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

// --- gamelist tests ---

func TestProbeGameTeamRange(t *testing.T) {
	dir := t.TempDir()

	// Game with teamRange.
	ranged := filepath.Join(dir, "ranged.js")
	os.WriteFile(ranged, []byte(`var Game = { teamRange: { min: 2, max: 4 }, load: function(){} };`), 0o644)
	tr := ProbeGameTeamRange(ranged)
	if tr.Min != 2 || tr.Max != 4 {
		t.Fatalf("expected {2,4}, got {%d,%d}", tr.Min, tr.Max)
	}

	// Game without teamRange.
	noRange := filepath.Join(dir, "norange.js")
	os.WriteFile(noRange, []byte(`var Game = { load: function(){} };`), 0o644)
	tr = ProbeGameTeamRange(noRange)
	if tr.Min != 0 || tr.Max != 0 {
		t.Fatalf("expected {0,0}, got {%d,%d}", tr.Min, tr.Max)
	}

	// Nonexistent file.
	tr = ProbeGameTeamRange(filepath.Join(dir, "nonexistent.js"))
	if tr.Min != 0 || tr.Max != 0 {
		t.Fatalf("expected {0,0} for missing file, got {%d,%d}", tr.Min, tr.Max)
	}
}

func TestProbeGameTeamRangeWithInclude(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.js"), []byte(`var TEAMS = { min: 3, max: 6 };`), 0o644)
	mainJS := filepath.Join(dir, "main.js")
	os.WriteFile(mainJS, []byte(`
		include("config.js");
		var Game = { teamRange: TEAMS, load: function(){} };
	`), 0o644)

	tr := ProbeGameTeamRange(mainJS)
	if tr.Min != 3 || tr.Max != 6 {
		t.Fatalf("expected {3,6}, got {%d,%d}", tr.Min, tr.Max)
	}
}

func TestFormatGameList(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "simple.js"), []byte(`var Game = { load: function(){} };`), 0o644)
	os.WriteFile(filepath.Join(dir, "teams.js"), []byte(`var Game = { teamRange: {min: 2, max: 2}, load: function(){} };`), 0o644)

	result := FormatGameList(dir, []string{"simple", "teams"}, "simple", 2)

	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Should contain game names.
	if !contains(result, "simple") || !contains(result, "teams") {
		t.Fatalf("expected both game names in output: %s", result)
	}
	// Active game should be marked.
	if !contains(result, "[active]") {
		t.Fatalf("expected [active] marker: %s", result)
	}
	// Team range should appear.
	if !contains(result, "[2 teams]") {
		t.Fatalf("expected [2 teams] marker: %s", result)
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.js"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "beta.js"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte(""), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	names := ListDir(dir, ".js")
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(names), names)
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("unexpected: %v", names)
	}
}

func TestListDirEmpty(t *testing.T) {
	dir := t.TempDir()
	names := ListDir(dir, ".js")
	if len(names) != 0 {
		t.Fatalf("expected 0, got %d", len(names))
	}
}

func TestListDirNonexistent(t *testing.T) {
	names := ListDir("/nonexistent/path", ".js")
	if names != nil {
		t.Fatalf("expected nil, got %v", names)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- plugin tests ---

func TestLoadPlugin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "echo.js")
	os.WriteFile(path, []byte(`
		var Plugin = {
			onMessage: function(author, text, isSystem) {
				if (text === "!echo") return "echoed";
				return null;
			}
		};
	`), 0o644)

	p, err := LoadPlugin(path, domain.RealClock{})
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	defer p.Unload()

	if p.Name() != "echo" {
		t.Fatalf("expected 'echo', got %q", p.Name())
	}

	// Should return "echoed" for "!echo".
	reply := p.OnMessage("alice", "!echo", false)
	if reply != "echoed" {
		t.Fatalf("expected 'echoed', got %q", reply)
	}

	// Should return empty for other messages.
	reply = p.OnMessage("alice", "hello", false)
	if reply != "" {
		t.Fatalf("expected empty, got %q", reply)
	}
}

func TestLoadPluginMissingObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.js")
	os.WriteFile(path, []byte(`var x = 1;`), 0o644)

	_, err := LoadPlugin(path, domain.RealClock{})
	if err == nil {
		t.Fatal("expected error for missing Plugin object")
	}
}

func TestLoadPluginMissingOnMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.js")
	os.WriteFile(path, []byte(`var Plugin = {};`), 0o644)

	_, err := LoadPlugin(path, domain.RealClock{})
	if err == nil {
		t.Fatal("expected error for missing onMessage")
	}
}

// --- shader tests ---

func TestLoadShader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invert.js")
	os.WriteFile(path, []byte(`
		var Shader = {
			process: function(buf, elapsed) {
				var p = buf.getPixel(0, 0);
				if (p) {
					buf.setChar(0, 0, "X", p.bg, p.fg, 0);
				}
			}
		};
	`), 0o644)

	s, err := LoadShader(path, domain.RealClock{})
	if err != nil {
		t.Fatalf("LoadShader: %v", err)
	}
	defer s.Unload()

	if s.Name() != "invert" {
		t.Fatalf("expected 'invert', got %q", s.Name())
	}

	// Apply shader to a buffer.
	buf := render.NewImageBuffer(10, 5)
	buf.SetChar(0, 0, 'A', nil, nil, 0)
	s.Process(buf, 0.0)

	if buf.Pixels[0].Char != 'X' {
		t.Fatalf("expected 'X' after shader, got %c", buf.Pixels[0].Char)
	}
}

func TestLoadShaderMissingProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.js")
	os.WriteFile(path, []byte(`var Shader = {};`), 0o644)

	_, err := LoadShader(path, domain.RealClock{})
	if err == nil {
		t.Fatal("expected error for missing process")
	}
}

func TestLoadShaderWithInit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "withinit.js")
	os.WriteFile(path, []byte(`
		var initialized = false;
		var Shader = {
			load: function() { initialized = true; },
			process: function(buf, elapsed) {}
		};
	`), 0o644)

	s, err := LoadShader(path, domain.RealClock{})
	if err != nil {
		t.Fatalf("LoadShader: %v", err)
	}
	defer s.Unload()
}

func TestApplyShaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upper.js")
	os.WriteFile(path, []byte(`
		var Shader = {
			process: function(buf, elapsed) {
				buf.writeString(0, 0, "DONE", "#fff", null, 0);
			}
		};
	`), 0o644)

	s, err := LoadShader(path, domain.RealClock{})
	if err != nil {
		t.Fatalf("LoadShader: %v", err)
	}
	defer s.Unload()

	buf := render.NewImageBuffer(10, 5)
	ApplyShaders([]domain.Shader{s}, buf, 1.0)

	if buf.Pixels[0].Char != 'D' {
		t.Fatalf("expected 'D', got %c", buf.Pixels[0].Char)
	}
}

func TestColorToHex(t *testing.T) {
	if s := ColorToHex(nil); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}
