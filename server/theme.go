package server

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
// NC used 3 palettes (primary/secondary/tertiary) that alternated by depth,
// plus a fixed warning palette for error dialogs.
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

// ─── Theme ────────────────────────────────────────────────────────────────────

// Theme defines the complete visual theme with 4 palettes that alternate by
// depth, plus global border characters and shadow color.
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

	Primary   Palette `json:"primary"`
	Secondary Palette `json:"secondary"`
	Tertiary  Palette `json:"tertiary"`
	Warning   Palette `json:"warning"`

	// Drop shadow
	ShadowBg string `json:"shadowBg"`

	// Outer border (the box/window frame)
	OuterTL string `json:"outerTL"`
	OuterTR string `json:"outerTR"`
	OuterBL string `json:"outerBL"`
	OuterBR string `json:"outerBR"`
	OuterH  string `json:"outerH"`
	OuterV  string `json:"outerV"`

	// Inner dividers (separators inside windows/panels)
	InnerH string `json:"innerH"`
	InnerV string `json:"innerV"`

	// Intersections (where inner dividers meet the outer frame)
	CrossL string `json:"crossL"`
	CrossR string `json:"crossR"`
	CrossT string `json:"crossT"`
	CrossB string `json:"crossB"`
	CrossX string `json:"crossX"`

	// Action bar separator
	BarSep string `json:"barSep"`
}

// PaletteAt returns the palette for the given depth level.
// Depth 0 = Primary, odd = Secondary, even > 0 = Tertiary.
func (t *Theme) PaletteAt(depth int) *Palette {
	switch {
	case depth <= 0:
		return &t.Primary
	case depth%2 == 1:
		return &t.Secondary
	default:
		return &t.Tertiary
	}
}

// WarningPalette returns the warning palette.
func (t *Theme) WarningPalette() *Palette { return &t.Warning }

// Global color accessors.
func (t *Theme) ShadowBgC() color.Color { return tc(t.ShadowBg, "#333333") }

// ts returns s if non-empty, otherwise fallback.
func ts(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Outer border accessors (default: double-line, matching NC windows).
func (t *Theme) OTL() string { return ts(t.OuterTL, "╔") }
func (t *Theme) OTR() string { return ts(t.OuterTR, "╗") }
func (t *Theme) OBL() string { return ts(t.OuterBL, "╚") }
func (t *Theme) OBR() string { return ts(t.OuterBR, "╝") }
func (t *Theme) OH() string  { return ts(t.OuterH, "═") }
func (t *Theme) OV() string  { return ts(t.OuterV, "║") }

// Inner divider accessors (default: single-line).
func (t *Theme) IH() string { return ts(t.InnerH, "─") }
func (t *Theme) IV() string { return ts(t.InnerV, "│") }

// Intersection accessors (inner single meets outer double).
func (t *Theme) XL() string { return ts(t.CrossL, "╟") }
func (t *Theme) XR() string { return ts(t.CrossR, "╢") }
func (t *Theme) XT() string { return ts(t.CrossT, "╤") }
func (t *Theme) XB() string { return ts(t.CrossB, "╧") }
func (t *Theme) XX() string { return ts(t.CrossX, "┼") }

// Action bar separator.
func (t *Theme) Sep() string { return ts(t.BarSep, "│") }

// LoadTheme reads a theme JSON file and returns the parsed Theme.
func LoadTheme(path string) (*Theme, error) {
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
	return &t, nil
}

// DefaultTheme returns the built-in norton theme (all fields empty = use defaults).
func DefaultTheme() *Theme {
	return &Theme{Name: "norton"}
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
