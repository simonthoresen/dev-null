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

func output(t *testing.T) (func(string), func() string) {
	t.Helper()
	var lines []string
	write := func(s string) { lines = append(lines, s) }
	read := func() string { return strings.Join(lines, "\n") }
	return write, read
}

func writeTheme(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "Themes")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"name":"`+name+`"}`), 0o644)
}

func writePlugin(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "Plugins")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, name+".js"), []byte(`var Plugin = { name: "`+name+`", onMessage: function(a,t,s){ return t; } };`), 0o644)
}

func writeShader(t *testing.T, dataDir, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "Shaders")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, name+".js"), []byte(`var Shader = { name: "`+name+`", process: function(buf,t){} };`), 0o644)
}

var testClock = &domain.MockClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

// TestMain isolates HOME / USERPROFILE so the real ~/DevNull/Create and
// ~/DevNull/Shared trees can't leak into source-aware asset listings.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "localcmd-test-home-")
	if err == nil {
		os.Setenv("HOME", tmp)
		os.Setenv("USERPROFILE", tmp)
		defer os.RemoveAll(tmp)
	}
	os.Exit(m.Run())
}

// isolateHome points HOME / USERPROFILE at a fresh temp dir so the
// user's real ~/DevNull/Create and ~/DevNull/Shared trees can't leak
// into source-aware asset listings during tests.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// ─── HandleThemeList / HandleThemeLoad ──────────────────────────────────────

func TestHandleThemeList_NoThemes(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleThemeList(dataDir, "", w)
	if !strings.Contains(read(), "No themes found") {
		t.Errorf("expected 'No themes found', got: %s", read())
	}
}

func TestHandleThemeList_ListsAvailable(t *testing.T) {
	dataDir := t.TempDir()
	writeTheme(t, dataDir, "dark")
	writeTheme(t, dataDir, "light")
	w, read := output(t)
	HandleThemeList(dataDir, "dark", w)
	got := read()
	if !strings.Contains(got, "dark") || !strings.Contains(got, "light") {
		t.Errorf("expected both themes listed, got: %s", got)
	}
	if !strings.Contains(got, "[active]") {
		t.Errorf("expected active marker, got: %s", got)
	}
}

func TestHandleThemeLoad_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeTheme(t, dataDir, "dark")
	w, read := output(t)
	theme, name := HandleThemeLoad("dark", dataDir, w)
	if theme == nil {
		t.Fatalf("expected theme to be returned; output: %s", read())
	}
	if name != "core:dark" {
		t.Errorf("expected name='core:dark', got %q", name)
	}
}

func TestHandleThemeLoad_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	theme, name := HandleThemeLoad("nonexistent", dataDir, w)
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

// ─── HandlePluginList / HandlePluginLoad / HandlePluginUnload ───────────────

func TestHandlePluginList_NoPlugins(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandlePluginList(dataDir, nil, w)
	if !strings.Contains(read(), "No plugins found") {
		t.Errorf("expected 'No plugins found', got: %s", read())
	}
}

func TestHandlePluginList_ShowsLoaded(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, _ := output(t)
	_, names, _ := HandlePluginLoad("echo", dataDir, testClock, nil, nil, w)
	w2, read2 := output(t)
	HandlePluginList(dataDir, names, w2)
	if !strings.Contains(read2(), "[loaded]") {
		t.Errorf("expected '[loaded]' marker, got: %s", read2())
	}
}

func TestHandlePluginLoad_Success(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	plugins, names, changed := HandlePluginLoad("echo", dataDir, testClock, nil, nil, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if len(plugins) != 1 || len(names) != 1 || names[0] != "core:echo" {
		t.Errorf("expected echo loaded; plugins=%d names=%v", len(plugins), names)
	}
}

func TestHandlePluginLoad_Duplicate(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, read := output(t)
	plugins, names, _ := HandlePluginLoad("echo", dataDir, testClock, nil, nil, w)
	_, _, changed := HandlePluginLoad("echo", dataDir, testClock, plugins, names, w)
	if changed {
		t.Error("expected no change on duplicate load")
	}
	if !strings.Contains(read(), "already loaded") {
		t.Errorf("expected 'already loaded' message, got: %s", read())
	}
}

func TestHandlePluginLoad_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandlePluginLoad("nope", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change on missing plugin")
	}
	if !strings.Contains(read(), "Failed") {
		t.Errorf("expected failure message, got: %s", read())
	}
}

func TestHandlePluginUnload_Success(t *testing.T) {
	dataDir := t.TempDir()
	writePlugin(t, dataDir, "echo")
	w, _ := output(t)
	plugins, names, _ := HandlePluginLoad("echo", dataDir, testClock, nil, nil, w)
	plugins2, names2, changed := HandlePluginUnload("echo", plugins, names, w)
	if !changed {
		t.Error("expected changed=true on unload")
	}
	if len(plugins2) != 0 || len(names2) != 0 {
		t.Errorf("expected empty after unload; got %v", names2)
	}
}

func TestHandlePluginUnload_NotLoaded(t *testing.T) {
	w, read := output(t)
	_, _, changed := HandlePluginUnload("ghost", nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "not loaded") {
		t.Errorf("expected 'not loaded' message, got: %s", read())
	}
}

// ─── HandleShaderList / HandleShaderLoad / HandleShaderUnload / Up / Down ───

func TestHandleShaderList_NoShaders(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	HandleShaderList(dataDir, nil, w)
	if !strings.Contains(read(), "No shaders found") {
		t.Errorf("expected 'No shaders found', got: %s", read())
	}
}

func TestHandleShaderList_ShowsActive(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, _ := output(t)
	_, names, _ := HandleShaderLoad("noop", dataDir, testClock, nil, nil, w)
	w2, read2 := output(t)
	HandleShaderList(dataDir, names, w2)
	if !strings.Contains(read2(), "[active]") {
		t.Errorf("expected '[active]' marker, got: %s", read2())
	}
}

func TestHandleShaderLoad_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	shaders, names, changed := HandleShaderLoad("noop", dataDir, testClock, nil, nil, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if len(shaders) != 1 || names[0] != "core:noop" {
		t.Errorf("expected noop loaded; got %v", names)
	}
}

func TestHandleShaderLoad_Duplicate(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, read := output(t)
	shaders, names, _ := HandleShaderLoad("noop", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShaderLoad("noop", dataDir, testClock, shaders, names, w)
	if changed {
		t.Error("expected no change on duplicate load")
	}
	if !strings.Contains(read(), "already loaded") {
		t.Errorf("expected 'already loaded', got: %s", read())
	}
}

func TestHandleShaderLoad_Missing(t *testing.T) {
	dataDir := t.TempDir()
	w, read := output(t)
	_, _, changed := HandleShaderLoad("ghost", dataDir, testClock, nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "Failed") {
		t.Errorf("expected failure message, got: %s", read())
	}
}

func TestHandleShaderUnload_Success(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "noop")
	w, _ := output(t)
	shaders, names, _ := HandleShaderLoad("noop", dataDir, testClock, nil, nil, w)
	shaders2, names2, changed := HandleShaderUnload("noop", shaders, names, w)
	if !changed {
		t.Error("expected changed=true on unload")
	}
	if len(shaders2) != 0 || len(names2) != 0 {
		t.Errorf("expected empty after unload")
	}
}

func TestHandleShaderUnload_NotLoaded(t *testing.T) {
	w, read := output(t)
	_, _, changed := HandleShaderUnload("ghost", nil, nil, w)
	if changed {
		t.Error("expected no change")
	}
	if !strings.Contains(read(), "not loaded") {
		t.Errorf("expected 'not loaded', got: %s", read())
	}
}

func TestHandleShaderUp(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	writeShader(t, dataDir, "b")
	w, read := output(t)
	shaders, names, _ := HandleShaderLoad("a", dataDir, testClock, nil, nil, w)
	shaders, names, _ = HandleShaderLoad("b", dataDir, testClock, shaders, names, w)
	shaders, names, changed := HandleShaderUp("b", shaders, names, w)
	if !changed {
		t.Errorf("expected changed=true; output: %s", read())
	}
	if names[0] != "core:b" || names[1] != "core:a" {
		t.Errorf("expected [b, a] after up, got %v", names)
	}
	_ = shaders
}

func TestHandleShaderDown(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	writeShader(t, dataDir, "b")
	w, _ := output(t)
	shaders, names, _ := HandleShaderLoad("a", dataDir, testClock, nil, nil, w)
	shaders, names, _ = HandleShaderLoad("b", dataDir, testClock, shaders, names, w)
	shaders, names, changed := HandleShaderDown("a", shaders, names, w)
	if !changed {
		t.Error("expected changed=true")
	}
	if names[0] != "core:b" || names[1] != "core:a" {
		t.Errorf("expected [b, a] after down, got %v", names)
	}
	_ = shaders
}

func TestHandleShaderUp_AlreadyFirst(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	w, read := output(t)
	shaders, names, _ := HandleShaderLoad("a", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShaderUp("a", shaders, names, w)
	if changed {
		t.Error("expected no change when already at top")
	}
	if !strings.Contains(read(), "already at position") {
		t.Errorf("expected position message, got: %s", read())
	}
}

func TestHandleShaderDown_AlreadyLast(t *testing.T) {
	dataDir := t.TempDir()
	writeShader(t, dataDir, "a")
	w, read := output(t)
	shaders, names, _ := HandleShaderLoad("a", dataDir, testClock, nil, nil, w)
	_, _, changed := HandleShaderDown("a", shaders, names, w)
	if changed {
		t.Error("expected no change when already at bottom")
	}
	if !strings.Contains(read(), "already at position") {
		t.Errorf("expected position message, got: %s", read())
	}
}
