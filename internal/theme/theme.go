package theme

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Palette ──────────────────────────────────────────────────────────────────

// Palette defines a complete color set for one visual layer.
type Palette struct {
	Bg          string `json:"bg"`          // body background
	Fg          string `json:"fg"`          // body foreground (normal text)
	Accent      string `json:"accent"`      // shortcut letters, column headers
	HighlightBg string `json:"highlightBg"` // selected item, active title, buttons
	HighlightFg string `json:"highlightFg"` // selected item foreground
	ActiveBg    string `json:"activeBg"`    // focused button, active accent
	ActiveFg    string `json:"activeFg"`    // focused button foreground
	InputBg     string `json:"inputBg"`     // text input field background
	InputFg     string `json:"inputFg"`     // text input field foreground
	DisabledFg  string `json:"disabledFg"`  // disabled/grayed items
}

// tc converts a hex string to a color.Color, returning fallback if empty.
func tc(hex, fallback string) color.Color {
	if hex == "" {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(hex)
}

// Resolved color accessors.
func (p *Palette) BgC() color.Color          { return tc(p.Bg, "#000080") }
func (p *Palette) FgC() color.Color          { return tc(p.Fg, "#00AAAA") }
func (p *Palette) AccentC() color.Color      { return tc(p.Accent, "#FFFF55") }
func (p *Palette) HighlightBgC() color.Color { return tc(p.HighlightBg, "#00AAAA") }
func (p *Palette) HighlightFgC() color.Color { return tc(p.HighlightFg, "#000000") }
func (p *Palette) ActiveBgC() color.Color    { return tc(p.ActiveBg, "#FFFF55") }
func (p *Palette) ActiveFgC() color.Color    { return tc(p.ActiveFg, "#000000") }
func (p *Palette) InputBgC() color.Color     { return tc(p.InputBg, "#000000") }
func (p *Palette) InputFgC() color.Color     { return tc(p.InputFg, "#55FFFF") }
func (p *Palette) DisabledFgC() color.Color  { return tc(p.DisabledFg, "#555555") }

// Lipgloss style helpers.
func (p *Palette) BaseStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.BgC()).Foreground(p.FgC())
}
func (p *Palette) AccentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.BgC()).Foreground(p.AccentC()).Underline(true)
}
func (p *Palette) HighlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.HighlightBgC()).Foreground(p.HighlightFgC()).Bold(true)
}
func (p *Palette) ActiveStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.ActiveBgC()).Foreground(p.ActiveFgC()).Bold(true)
}
func (p *Palette) DisabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.BgC()).Foreground(p.DisabledFgC())
}
func (p *Palette) InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.InputBgC()).Foreground(p.InputFgC())
}

// ─── BorderSet ───────────────────────────────────────────────────────────────

// BorderSet defines border/divider characters for one visual layer.
type BorderSet struct {
	OuterTL string `json:"outerTL,omitempty"`
	OuterTR string `json:"outerTR,omitempty"`
	OuterBL string `json:"outerBL,omitempty"`
	OuterBR string `json:"outerBR,omitempty"`
	OuterH  string `json:"outerH,omitempty"`
	OuterV  string `json:"outerV,omitempty"`
	InnerH  string `json:"innerH,omitempty"`
	InnerV  string `json:"innerV,omitempty"`
	CrossL  string `json:"crossL,omitempty"`
	CrossR  string `json:"crossR,omitempty"`
	CrossT  string `json:"crossT,omitempty"`
	CrossB  string `json:"crossB,omitempty"`
	CrossX  string `json:"crossX,omitempty"`
	BarSep  string `json:"barSep,omitempty"`
}

// ─── Layer ───────────────────────────────────────────────────────────────────

// Layer combines a color palette and border characters for one depth level.
type Layer struct {
	Palette   // embedded — promotes BaseStyle(), HighlightStyle(), etc.
	BorderSet // embedded — border fields
}

// Outer border accessors (default: double-line, matching NC windows).
func (l *Layer) OTL() string { return ts(l.OuterTL, "╔") }
func (l *Layer) OTR() string { return ts(l.OuterTR, "╗") }
func (l *Layer) OBL() string { return ts(l.OuterBL, "╚") }
func (l *Layer) OBR() string { return ts(l.OuterBR, "╝") }
func (l *Layer) OH() string  { return ts(l.OuterH, "═") }
func (l *Layer) OV() string  { return ts(l.OuterV, "║") }

// Inner divider accessors (default: single-line).
func (l *Layer) IH() string { return ts(l.InnerH, "─") }
func (l *Layer) IV() string { return ts(l.InnerV, "│") }

// Intersection accessors (inner single meets outer double).
func (l *Layer) XL() string { return ts(l.CrossL, "╟") }
func (l *Layer) XR() string { return ts(l.CrossR, "╢") }
func (l *Layer) XT() string { return ts(l.CrossT, "╤") }
func (l *Layer) XB() string { return ts(l.CrossB, "╧") }
func (l *Layer) XX() string { return ts(l.CrossX, "┼") }

// Action bar separator.
func (l *Layer) Sep() string { return ts(l.BarSep, "│") }

// ─── Theme ────────────────────────────────────────────────────────────────────

// Theme defines the complete visual theme with 4 layers that alternate by
// depth. Each layer carries both colors and border characters.
//
// Depth assignment:
//
//	0 (desktop/panels) → Primary
//	1 (menus, first dialog) → Secondary
//	2 (dialog over dialog) → Tertiary
//	3 → Secondary again, 4 → Tertiary, ...
//	Warning dialogs → Warning (always, regardless of depth)
type Theme struct {
	Name string `json:"name"`

	Primary   Layer `json:"primary"`
	Secondary Layer `json:"secondary"`
	Tertiary  Layer `json:"tertiary"`
	Warning   Layer `json:"warning"`

	// Drop shadow (bg = shadow area, fg = half-block foreground for depth effect)
	ShadowBg string `json:"shadowBg"`
	ShadowFg string `json:"shadowFg"`

	// Deprecated: global border fields for JSON backwards compatibility.
	// resolveDefaults() copies these into any layer that has empty borders.
	// New themes should define borders per-layer instead.
	LegacyOuterTL string `json:"outerTL,omitempty"`
	LegacyOuterTR string `json:"outerTR,omitempty"`
	LegacyOuterBL string `json:"outerBL,omitempty"`
	LegacyOuterBR string `json:"outerBR,omitempty"`
	LegacyOuterH  string `json:"outerH,omitempty"`
	LegacyOuterV  string `json:"outerV,omitempty"`
	LegacyInnerH  string `json:"innerH,omitempty"`
	LegacyInnerV  string `json:"innerV,omitempty"`
	LegacyCrossL  string `json:"crossL,omitempty"`
	LegacyCrossR  string `json:"crossR,omitempty"`
	LegacyCrossT  string `json:"crossT,omitempty"`
	LegacyCrossB  string `json:"crossB,omitempty"`
	LegacyCrossX  string `json:"crossX,omitempty"`
	LegacyBarSep  string `json:"barSep,omitempty"`
}

// LayerAt returns the theme layer for the given depth level.
// Depth 0 = Primary, odd = Secondary, even > 0 = Tertiary.
func (t *Theme) LayerAt(depth int) *Layer {
	switch {
	case depth <= 0:
		return &t.Primary
	case depth%2 == 1:
		return &t.Secondary
	default:
		return &t.Tertiary
	}
}

// WarningLayer returns the warning theme layer.
func (t *Theme) WarningLayer() *Layer { return &t.Warning }

// Global color accessors.
func (t *Theme) ShadowBgC() color.Color { return tc(t.ShadowBg, "#000000") }
func (t *Theme) ShadowFgC() color.Color { return tc(t.ShadowFg, "#555555") }

// ShadowStyle returns the style for drop shadow rendering.
func (t *Theme) ShadowStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(t.ShadowBgC()).Foreground(t.ShadowFgC())
}

// ts returns s if non-empty, otherwise fallback.
func ts(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// resolveDefaults copies global (legacy) border fields into any layer
// that has empty border fields, providing JSON backwards compatibility.
func (t *Theme) resolveDefaults() {
	for _, layer := range []*Layer{&t.Primary, &t.Secondary, &t.Tertiary, &t.Warning} {
		copyIfEmpty(&layer.OuterTL, t.LegacyOuterTL)
		copyIfEmpty(&layer.OuterTR, t.LegacyOuterTR)
		copyIfEmpty(&layer.OuterBL, t.LegacyOuterBL)
		copyIfEmpty(&layer.OuterBR, t.LegacyOuterBR)
		copyIfEmpty(&layer.OuterH, t.LegacyOuterH)
		copyIfEmpty(&layer.OuterV, t.LegacyOuterV)
		copyIfEmpty(&layer.InnerH, t.LegacyInnerH)
		copyIfEmpty(&layer.InnerV, t.LegacyInnerV)
		copyIfEmpty(&layer.CrossL, t.LegacyCrossL)
		copyIfEmpty(&layer.CrossR, t.LegacyCrossR)
		copyIfEmpty(&layer.CrossT, t.LegacyCrossT)
		copyIfEmpty(&layer.CrossB, t.LegacyCrossB)
		copyIfEmpty(&layer.CrossX, t.LegacyCrossX)
		copyIfEmpty(&layer.BarSep, t.LegacyBarSep)
	}
}

func copyIfEmpty(dst *string, src string) {
	if *dst == "" && src != "" {
		*dst = src
	}
}

// Load reads a theme JSON file and returns the parsed Theme.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse theme %s: %w", path, err)
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	t.resolveDefaults()
	return &t, nil
}

// Default returns the built-in norton theme.
func Default() *Theme {
	return &Theme{
		Name: "norton",
		Primary: Layer{Palette: Palette{
			Bg: "#000080", Fg: "#00AAAA", Accent: "#FFFF55",
			HighlightBg: "#00AAAA", HighlightFg: "#000000",
			ActiveBg: "#FFFF55", ActiveFg: "#000000",
			InputBg: "#000000", InputFg: "#55FFFF",
			DisabledFg: "#555555",
		}},
		Secondary: Layer{Palette: Palette{
			Bg: "#008080", Fg: "#000000", Accent: "#FFFF55",
			HighlightBg: "#000000", HighlightFg: "#00AAAA",
			ActiveBg: "#FFFF55", ActiveFg: "#000000",
			InputBg: "#000000", InputFg: "#55FFFF",
			DisabledFg: "#555555",
		}},
		Tertiary: Layer{Palette: Palette{
			Bg: "#AAAAAA", Fg: "#000000", Accent: "#FFFF55",
			HighlightBg: "#000000", HighlightFg: "#FFFFFF",
			ActiveBg: "#FFFF55", ActiveFg: "#000000",
			InputBg: "#000000", InputFg: "#55FFFF",
			DisabledFg: "#555555",
		}},
		Warning: Layer{Palette: Palette{
			Bg: "#AA0000", Fg: "#FFFFFF", Accent: "#FFFF55",
			HighlightBg: "#FFFFFF", HighlightFg: "#AA0000",
			ActiveBg: "#FFFF55", ActiveFg: "#000000",
			InputBg: "#000000", InputFg: "#55FFFF",
			DisabledFg: "#555555",
		}},
		ShadowBg: "#333333",
	}
}

// ListThemes returns the names of available theme files in the themes directory.
func ListThemes(dataDir string) []string {
	dir := filepath.Join(dataDir, "themes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return names
}
