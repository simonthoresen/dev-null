package theme

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDefaultThemeNotNil(t *testing.T) {
	th := Default()
	if th == nil {
		t.Fatal("DefaultTheme returned nil")
	}
	if th.Name != "norton" {
		t.Errorf("expected name 'norton', got %q", th.Name)
	}
}

func TestLayerAtDepth(t *testing.T) {
	th := Default()
	tests := []struct {
		depth int
		want  *Layer
	}{
		{0, &th.Primary},
		{-1, &th.Primary},
		{1, &th.Secondary},
		{2, &th.Tertiary},
		{3, &th.Secondary},
		{4, &th.Tertiary},
		{5, &th.Secondary},
	}
	for _, tt := range tests {
		got := th.LayerAt(tt.depth)
		if got != tt.want {
			t.Errorf("LayerAt(%d): wrong layer", tt.depth)
		}
	}
}

func TestWarningLayer(t *testing.T) {
	th := Default()
	w := th.WarningLayer()
	if w != &th.Warning {
		t.Error("WarningLayer should return Warning layer")
	}
}

func TestBorderDefaults(t *testing.T) {
	th := Default()
	layer := th.LayerAt(0)
	if layer.OuterTL != "╔" {
		t.Errorf("expected double-line TL, got %q", layer.OuterTL)
	}
	if layer.OuterTR != "╗" {
		t.Errorf("expected double-line TR, got %q", layer.OuterTR)
	}
	if layer.OuterBL != "╚" {
		t.Errorf("expected double-line BL, got %q", layer.OuterBL)
	}
	if layer.OuterBR != "╝" {
		t.Errorf("expected double-line BR, got %q", layer.OuterBR)
	}
	if layer.OuterH != "═" {
		t.Errorf("expected double-line H, got %q", layer.OuterH)
	}
	if layer.OuterV != "║" {
		t.Errorf("expected double-line V, got %q", layer.OuterV)
	}
	if layer.InnerH != "─" {
		t.Errorf("expected single-line inner H, got %q", layer.InnerH)
	}
	if layer.InnerV != "│" {
		t.Errorf("expected single-line inner V, got %q", layer.InnerV)
	}
	if layer.CrossL != "╟" {
		t.Errorf("expected intersection CrossL, got %q", layer.CrossL)
	}
	if layer.CrossR != "╢" {
		t.Errorf("expected intersection CrossR, got %q", layer.CrossR)
	}
	if layer.CrossT != "╤" {
		t.Errorf("expected intersection CrossT, got %q", layer.CrossT)
	}
	if layer.CrossB != "╧" {
		t.Errorf("expected intersection CrossB, got %q", layer.CrossB)
	}
	if layer.CrossX != "┼" {
		t.Errorf("expected intersection CrossX, got %q", layer.CrossX)
	}
	if layer.BarSep != "│" {
		t.Errorf("expected bar separator, got %q", layer.BarSep)
	}
}

func TestBorderCustomValues(t *testing.T) {
	th := Default()
	th.Primary.OuterTL = "+"
	th.Primary.InnerH = "~"
	layer := th.LayerAt(0)
	if layer.OuterTL != "+" {
		t.Errorf("expected custom TL '+', got %q", layer.OuterTL)
	}
	if layer.InnerH != "~" {
		t.Errorf("expected custom IH '~', got %q", layer.InnerH)
	}
}

func TestPaletteColors(t *testing.T) {
	p := &Palette{
		Bg: lipgloss.Color("#112233"), Fg: lipgloss.Color("#445566"),
		Accent: lipgloss.Color("#778899"), HighlightBg: lipgloss.Color("#aabbcc"),
		HighlightFg: lipgloss.Color("#ddeeff"), ActiveBg: lipgloss.Color("#001122"),
		ActiveFg: lipgloss.Color("#334455"), InputBg: lipgloss.Color("#667788"),
		InputFg: lipgloss.Color("#99aabb"), DisabledFg: lipgloss.Color("#ccddee"),
	}
	// Verify fields are set and non-nil.
	if p.Bg == nil {
		t.Error("Bg should not be nil")
	}
	if p.Fg == nil {
		t.Error("Fg should not be nil")
	}
}

func TestPaletteColorFallbacks(t *testing.T) {
	// Empty layer — fillDefaults should populate all colors.
	l := &Layer{}
	l.fillDefaults()
	if l.Bg == nil {
		t.Error("Bg should be set by fillDefaults")
	}
	if l.Fg == nil {
		t.Error("Fg should be set by fillDefaults")
	}
	if l.InputBg == nil {
		t.Error("InputBg should be set by fillDefaults")
	}
	if l.DisabledFg == nil {
		t.Error("DisabledFg should be set by fillDefaults")
	}
}

func TestPaletteStyleBuilders(t *testing.T) {
	p := &Palette{
		Bg: lipgloss.Color("#000000"), Fg: lipgloss.Color("#ffffff"),
		Accent: lipgloss.Color("#ff0000"),
		HighlightBg: lipgloss.Color("#0000ff"), HighlightFg: lipgloss.Color("#ffffff"),
		ActiveBg: lipgloss.Color("#00ff00"), ActiveFg: lipgloss.Color("#000000"),
		InputBg: lipgloss.Color("#111111"), InputFg: lipgloss.Color("#eeeeee"),
		DisabledFg: lipgloss.Color("#888888"),
	}

	// Verify style builders don't panic and produce non-empty renders.
	styles := []func() string{
		func() string { return p.BaseStyle().Render("x") },
		func() string { return p.AccentStyle().Render("x") },
		func() string { return p.HighlightStyle().Render("x") },
		func() string { return p.ActiveStyle().Render("x") },
		func() string { return p.DisabledStyle().Render("x") },
		func() string { return p.InputStyle().Render("x") },
	}
	for i, fn := range styles {
		s := fn()
		if len(s) == 0 {
			t.Errorf("style %d rendered empty", i)
		}
	}
}

func TestShadowStyle(t *testing.T) {
	th := Default()
	s := th.ShadowStyle().Render("x")
	if len(s) == 0 {
		t.Error("shadow style rendered empty")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.json")
	os.WriteFile(path, []byte(`{
		"name": "custom",
		"primary": {"bg": "#111", "fg": "#eee"},
		"secondary": {"bg": "#222", "fg": "#ddd"},
		"tertiary": {"bg": "#333", "fg": "#ccc"},
		"warning": {"bg": "#f00", "fg": "#fff"}
	}`), 0o644)

	th, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if th.Name != "custom" {
		t.Fatalf("expected 'custom', got %q", th.Name)
	}
	if th.Primary.Bg != lipgloss.Color("#111") {
		t.Fatalf("expected primary bg lipgloss.Color('#111'), got %v", th.Primary.Bg)
	}
}

func TestLoadInfersName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mytheme.json")
	os.WriteFile(path, []byte(`{"primary": {"bg": "#000"}}`), 0o644)

	th, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if th.Name != "mytheme" {
		t.Fatalf("expected inferred name 'mytheme', got %q", th.Name)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte(`{not json}`), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/theme.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadWithPerLayerBorders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bordered.json")
	os.WriteFile(path, []byte(`{
		"primary": {"bg": "#000", "outerTL": "┌", "outerH": "─"},
		"secondary": {"bg": "#111", "outerTL": "╔", "outerH": "═"}
	}`), 0o644)

	th, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if th.Primary.OuterTL != "┌" {
		t.Fatalf("expected primary TL '┌', got %q", th.Primary.OuterTL)
	}
	if th.Primary.OuterH != "─" {
		t.Fatalf("expected primary H '─', got %q", th.Primary.OuterH)
	}
	if th.Secondary.OuterTL != "╔" {
		t.Fatalf("expected secondary TL '╔', got %q", th.Secondary.OuterTL)
	}
}

func TestListThemes(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	os.MkdirAll(themesDir, 0o755)
	os.WriteFile(filepath.Join(themesDir, "dark.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(themesDir, "light.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(themesDir, "readme.txt"), []byte(""), 0o644)
	os.MkdirAll(filepath.Join(themesDir, "subdir"), 0o755)

	names := ListThemes(dir)
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(names), names)
	}
}

func TestListThemesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	names := ListThemes(dir)
	if names != nil {
		t.Fatalf("expected nil, got %v", names)
	}
}
