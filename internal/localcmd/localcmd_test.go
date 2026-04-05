package localcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev-null/internal/domain"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func collect(fn func(func(string))) []string {
	var out []string
	fn(func(s string) { out = append(out, s) })
	return out
}

func output(t *testing.T) (func(string), func() string) {
	t.Helper()
	var lines []string
	write := func(s string) { lines = append(lines, s) }
	read := func() string { return strings.Join(lines, "\n") }
	return write, read
}

// writeTheme creates a minimal valid theme JSON file in dataDir/themes/.
func writeTheme(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"` + name + `"}`
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writePlugin creates a minimal valid plugin JS file in dataDir/plugins/.
func writePlugin(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `var Plugin = { name: "` + name + `", onMessage: function(a,t,s){ return t; } };`
	if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeShader creates a minimal valid shader JS file in dataDir/shaders/.
func writeShader(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "shaders")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `var Shader = { name: "` + name + `", process: function(buf,t){} };`
	if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

var testClock = &domain.MockClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

// ─── HandleTheme ────────────────────────────────────────────────────────────

func TestHandleTheme_NoArgs_NoThemes(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleTheme("/theme", dataDir, "", w)
	if !strings.Contains(read(), "No themes found") {
		t.Errorf("expected 'No themes found', got: %s", read())
	}
}

func TestHandleTheme_NoArgs_ListsAvailable(t *testing.T) {
	dataDir := t.TempDir()
	writeTheme(t, dataDir, "dark")
	writeTheme(t, dataDir, "light")
	w, read := output(t)
	HandleTheme("/theme", dataDir, "dark", w)
	got := read()
	if !strings.Contains(got, "dark") || !strings.Contains(got, "light") {
		t.Errorf("expected both themes listed, got: %s", got)
	}
	if !strings.Contains(got, "[active]") {
		t.Errorf("expected active marker, got: %s", got)
	}
}

func TestHandleTheme_Load_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeTheme(t, dataDir, "dark")
	w, read := output(t)
	theme, name := HandleTheme("/theme dark", dataDir, "", w)
	if theme == nil {
		t.Fatalf("expected theme to be returned; output: %s", read())
	}
	if name != "dark" {
		t.Errorf("expected name='dark', got %q", name)
	}
}

func TestHandleTheme_Load_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	theme, name := HandleTheme("/theme nonexistent", dataDir, "", w)
	if theme != nil {
		t.Error("expected nil theme for missing file")
	}
	if name != "" {
		t.Errorf("expected empty name, got %q", name)
	}
	if !strings.Contains(read(), "Failed") {
		t.Errorf("expected failure message, got: %s", read())
	}
}

// ─── HandlePlugin ────────────────────────────────────────────────────────────

func TestHandlePlugin_NoArgs_NoPlugins(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandlePlugin("/plugin", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "No plugins found") {
		t.Errorf("expected 'No plugins found', got: %s", read())
	}
}

func TestHandlePlugin_NoArgs_ListsAvailable(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	HandlePlugin("/plugin", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "echo") {
		t.Errorf("expected 'echo' listed, got: %s", read())
	}
}

func TestHandlePlugin_Load_Success(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	plugins, names, changed := HandlePlugin("/plugin load echo", dataDir, testClock, nil, nil, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if len(plugins) != 1 || len(names) != 1 || names[0] != "echo" {
		t.Errorf("expected echo loaded; plugins=%d names=%v", len(plugins), names)
	}
}

func TestHandlePlugin_Load_Duplicate(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	plugins, names, _ := HandlePlugin("/plugin load echo", dataDir, testClock, nil, nil, w)
	_, _, changed := HandlePlugin("/plugin load echo", dataDir, testClock, plugins, names, w)
	if changed {
		t.Error("expected no change on duplicate load")
	}
	if !strings.Contains(read(), "already loaded") {
		t.Errorf("expected 'already loaded' message, got: %s", read())
	}
}

func TestHandlePlugin_Load_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandlePlugin("/plugin load nope", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change on missing plugin")
	}
	if !strings.Contains(read(), "Failed") {
		t.Errorf("expected failure message, got: %s", read())
	}
}

func TestHandlePlugin_Unload_Success(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, _ := output(t)
	plugins, names, _ := HandlePlugin("/plugin load echo", dataDir, testClock, nil, nil, w)
	plugins2, names2, changed := HandlePlugin("/plugin unload echo", dataDir, testClock, plugins, names, w)
	if !changed {
		t.Error("expected changed=true on unload")
	}
	if len(plugins2) != 0 || len(names2) != 0 {
		t.Errorf("expected empty after unload; got %v", names2)
	}
}

func TestHandlePlugin_Unload_NotLoaded(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandlePlugin("/plugin unload ghost", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "not loaded") {
		t.Errorf("expected 'not loaded' message, got: %s", read())
	}
}

func TestHandlePlugin_List(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	plugins, names, _ := HandlePlugin("/plugin load echo", dataDir, testClock, nil, nil, w)
	HandlePlugin("/plugin list", dataDir, testClock, plugins, names, w)
	if !strings.Contains(read(), "echo") {
		t.Errorf("expected echo in list, got: %s", read())
	}
}

func TestHandlePlugin_List_Empty(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandlePlugin("/plugin list", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "No plugins") {
		t.Errorf("expected 'No plugins' message, got: %s", read())
	}
}

func TestHandlePlugin_UnknownSubcommand(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandlePlugin("/plugin bogus", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand', got: %s", read())
	}
}

func TestHandlePlugin_Load_NoName(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandlePlugin("/plugin load", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "Usage") {
		t.Errorf("expected usage hint, got: %s", read())
	}
}

func TestHandlePlugin_Unload_NoName(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandlePlugin("/plugin unload", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "Usage") {
		t.Errorf("expected usage hint, got: %s", read())
	}
}

// ─── HandleShader ────────────────────────────────────────────────────────────

func TestHandleShader_NoArgs_NoShaders(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleShader("/shader", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "No shaders found") {
		t.Errorf("expected 'No shaders found', got: %s", read())
	}
}

func TestHandleShader_NoArgs_ListsAvailable(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	HandleShader("/shader", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "noop") {
		t.Errorf("expected 'noop' listed, got: %s", read())
	}
}

func TestHandleShader_Load_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	shaders, names, changed := HandleShader("/shader load noop", dataDir, testClock, nil, nil, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if len(shaders) != 1 || names[0] != "noop" {
		t.Errorf("expected noop loaded; got %v", names)
	}
}

func TestHandleShader_Load_Duplicate(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	shaders, names, _ := HandleShader("/shader load noop", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShader("/shader load noop", dataDir, testClock, shaders, names, w)
	if changed {
		t.Error("expected no change on duplicate load")
	}
	if !strings.Contains(read(), "already loaded") {
		t.Errorf("expected 'already loaded', got: %s", read())
	}
}

func TestHandleShader_Load_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandleShader("/shader load ghost", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "Failed") {
		t.Errorf("expected failure message, got: %s", read())
	}
}

func TestHandleShader_Unload_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, _ := output(t)
	shaders, names, _ := HandleShader("/shader load noop", dataDir, testClock, nil, nil, w)
	shaders2, names2, changed := HandleShader("/shader unload noop", dataDir, testClock, shaders, names, w)
	if !changed {
		t.Error("expected changed=true on unload")
	}
	if len(shaders2) != 0 || len(names2) != 0 {
		t.Errorf("expected empty after unload")
	}
}

func TestHandleShader_Unload_NotLoaded(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandleShader("/shader unload ghost", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "not loaded") {
		t.Errorf("expected 'not loaded', got: %s", read())
	}
}

func TestHandleShader_List(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	shaders, names, _ := HandleShader("/shader load noop", dataDir, testClock, nil, nil, w)
	HandleShader("/shader list", dataDir, testClock, shaders, names, w)
	if !strings.Contains(read(), "noop") {
		t.Errorf("expected noop in list, got: %s", read())
	}
}

func TestHandleShader_List_Empty(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleShader("/shader list", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "No shaders") {
		t.Errorf("expected 'No shaders', got: %s", read())
	}
}

func TestHandleShader_MoveUp(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	writeShader(t, dataDir, "b")
	w, read := output(t)
	shaders, names, _ := HandleShader("/shader load a", dataDir, testClock, nil, nil, w)
	shaders, names, _ = HandleShader("/shader load b", dataDir, testClock, shaders, names, w)
	// b is at index 1; move it up to index 0
	shaders, names, changed := HandleShader("/shader up b", dataDir, testClock, shaders, names, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if names[0] != "b" || names[1] != "a" {
		t.Errorf("expected [b, a] after up, got %v", names)
	}
	_ = shaders
}

func TestHandleShader_MoveDown(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	writeShader(t, dataDir, "b")
	w, _ := output(t)
	shaders, names, _ := HandleShader("/shader load a", dataDir, testClock, nil, nil, w)
	shaders, names, _ = HandleShader("/shader load b", dataDir, testClock, shaders, names, w)
	// a is at index 0; move it down to index 1
	shaders, names, changed := HandleShader("/shader down a", dataDir, testClock, shaders, names, w)
	if !changed {
		t.Error("expected changed=true")
	}
	if names[0] != "b" || names[1] != "a" {
		t.Errorf("expected [b, a] after down, got %v", names)
	}
	_ = shaders
}

func TestHandleShader_MoveUp_AlreadyFirst(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	w, read := output(t)
	shaders, names, _ := HandleShader("/shader load a", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShader("/shader up a", dataDir, testClock, shaders, names, w)
	if changed {
		t.Error("expected no change when already at top")
	}
	if !strings.Contains(read(), "already at position") {
		t.Errorf("expected position message, got: %s", read())
	}
}

func TestHandleShader_MoveDown_AlreadyLast(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	w, read := output(t)
	shaders, names, _ := HandleShader("/shader load a", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShader("/shader down a", dataDir, testClock, shaders, names, w)
	if changed {
		t.Error("expected no change when already at bottom")
	}
	if !strings.Contains(read(), "already at position") {
		t.Errorf("expected position message, got: %s", read())
	}
}

func TestHandleShader_UnknownSubcommand(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleShader("/shader bogus", dataDir, testClock, nil, nil, w)
	if !strings.Contains(read(), "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand', got: %s", read())
	}
}

func TestHandleShader_Load_NoName(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandleShader("/shader load", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "Usage") {
		t.Errorf("expected usage hint, got: %s", read())
	}
}

// ─── integration: loaded plugin shown as [loaded] in listing ─────────────────

func TestHandlePlugin_NoArgs_ShowsLoaded(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, _ := output(t)
	plugins, names, _ := HandlePlugin("/plugin load echo", dataDir, testClock, nil, nil, w)

	var out2 []string
	HandlePlugin("/plugin", dataDir, testClock, plugins, names, func(s string) { out2 = append(out2, s) })
	combined := strings.Join(out2, "\n")
	if !strings.Contains(combined, "[loaded]") {
		t.Errorf("expected '[loaded]' marker for loaded plugin, got: %s", combined)
	}
}

func TestHandleShader_NoArgs_ShowsActive(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, _ := output(t)
	shaders, names, _ := HandleShader("/shader load noop", dataDir, testClock, nil, nil, w)

	var out2 []string
	HandleShader("/shader", dataDir, testClock, shaders, names, func(s string) { out2 = append(out2, s) })
	combined := strings.Join(out2, "\n")
	if !strings.Contains(combined, "[active]") {
		t.Errorf("expected '[active]' marker for active shader, got: %s", combined)
	}
}

