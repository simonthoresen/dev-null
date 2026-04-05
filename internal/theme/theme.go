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
// Fields are populated at construction time (Default) or via Layer.UnmarshalJSON.
type Palette struct {
	Bg          color.Color
	Fg          color.Color
	Accent      color.Color
	HighlightBg color.Color
	HighlightFg color.Color
	ActiveBg    color.Color
	ActiveFg    color.Color
	InputBg     color.Color
	InputFg     color.Color
	DisabledFg  color.Color
}

// Lipgloss style helpers.
func (p *Palette) BaseStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.Bg).Foreground(p.Fg)
}
func (p *Palette) AccentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.Bg).Foreground(p.Accent).Underline(true)
}
func (p *Palette) HighlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.HighlightBg).Foreground(p.HighlightFg).Bold(true)
}
func (p *Palette) ActiveStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.ActiveBg).Foreground(p.ActiveFg).Bold(true)
}
func (p *Palette) DisabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.Bg).Foreground(p.DisabledFg)
}
func (p *Palette) InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.InputBg).Foreground(p.InputFg)
}

// ─── BorderSet ───────────────────────────────────────────────────────────────

// BorderSet defines border/divider characters for one visual layer.
// Fields are populated with defaults at construction time or via Layer.UnmarshalJSON.
type BorderSet struct {
	OuterTL string
	OuterTR string
	OuterBL string
	OuterBR string
	OuterH  string
	OuterV  string
	InnerH  string
	InnerV  string
	CrossL      string
	CrossR      string
	CrossT      string
	CrossB      string
	CrossX      string
	InnerCrossT string // VDivider starts below an inner HDivider (┬, all thin)
	InnerCrossB string // VDivider ends above an inner HDivider (┴, all thin)
	InnerCrossL string // HDivider starts right of an inner VDivider (├, all thin)
	InnerCrossR string // HDivider ends left of an inner VDivider (┤, all thin)
	BarSep      string
}

// ─── Layer ───────────────────────────────────────────────────────────────────

// Layer combines a color palette and border characters for one depth level.
type Layer struct {
	Palette   // embedded — promotes BaseStyle(), HighlightStyle(), etc.
	BorderSet // embedded — border fields
	// Monochrome is set at render time when the terminal has no color support.
	// Widgets use it to show a text cursor glyph (►/›) instead of relying on
	// background-color highlighting alone.
	Monochrome bool
}

// SetMonochrome propagates the monochrome flag to all layers in the theme.
// Call this once per View() render based on the terminal's color profile.
func (t *Theme) SetMonochrome(mono bool) {
	t.Primary.Monochrome = mono
	t.Secondary.Monochrome = mono
	t.Tertiary.Monochrome = mono
	t.Warning.Monochrome = mono
}

// fillDefaults populates zero-value palette colors and empty border strings
// with their default values. Safe to call multiple times (idempotent).
func (l *Layer) fillDefaults() {
	if l.Bg == nil {
		l.Bg = lipgloss.Color("#000080")
	}
	if l.Fg == nil {
		l.Fg = lipgloss.Color("#00AAAA")
	}
	if l.Accent == nil {
		l.Accent = lipgloss.Color("#FFFF55")
	}
	if l.HighlightBg == nil {
		l.HighlightBg = lipgloss.Color("#00AAAA")
	}
	if l.HighlightFg == nil {
		l.HighlightFg = lipgloss.Color("#000000")
	}
	if l.ActiveBg == nil {
		l.ActiveBg = lipgloss.Color("#FFFF55")
	}
	if l.ActiveFg == nil {
		l.ActiveFg = lipgloss.Color("#000000")
	}
	if l.InputBg == nil {
		l.InputBg = lipgloss.Color("#000000")
	}
	if l.InputFg == nil {
		l.InputFg = lipgloss.Color("#55FFFF")
	}
	if l.DisabledFg == nil {
		l.DisabledFg = lipgloss.Color("#555555")
	}
	if l.OuterTL == "" {
		l.OuterTL = "╔"
	}
	if l.OuterTR == "" {
		l.OuterTR = "╗"
	}
	if l.OuterBL == "" {
		l.OuterBL = "╚"
	}
	if l.OuterBR == "" {
		l.OuterBR = "╝"
	}
	if l.OuterH == "" {
		l.OuterH = "═"
	}
	if l.OuterV == "" {
		l.OuterV = "║"
	}
	if l.InnerH == "" {
		l.InnerH = "─"
	}
	if l.InnerV == "" {
		l.InnerV = "│"
	}
	if l.CrossL == "" {
		l.CrossL = "╟"
	}
	if l.CrossR == "" {
		l.CrossR = "╢"
	}
	if l.CrossT == "" {
		l.CrossT = "╤"
	}
	if l.CrossB == "" {
		l.CrossB = "╧"
	}
	if l.CrossX == "" {
		l.CrossX = "┼"
	}
	if l.InnerCrossT == "" {
		l.InnerCrossT = "┬"
	}
	if l.InnerCrossB == "" {
		l.InnerCrossB = "┴"
	}
	if l.InnerCrossL == "" {
		l.InnerCrossL = "├"
	}
	if l.InnerCrossR == "" {
		l.InnerCrossR = "┤"
	}
	if l.BarSep == "" {
		l.BarSep = "│"
	}
}

// UnmarshalJSON decodes a Layer from JSON, applying defaults for missing fields.
func (l *Layer) UnmarshalJSON(data []byte) error {
	var raw struct {
		Bg          string `json:"bg"`
		Fg          string `json:"fg"`
		Accent      string `json:"accent"`
		HighlightBg string `json:"highlightBg"`
		HighlightFg string `json:"highlightFg"`
		ActiveBg    string `json:"activeBg"`
		ActiveFg    string `json:"activeFg"`
		InputBg     string `json:"inputBg"`
		InputFg     string `json:"inputFg"`
		DisabledFg  string `json:"disabledFg"`
		OuterTL     string `json:"outerTL"`
		OuterTR     string `json:"outerTR"`
		OuterBL     string `json:"outerBL"`
		OuterBR     string `json:"outerBR"`
		OuterH      string `json:"outerH"`
		OuterV      string `json:"outerV"`
		InnerH      string `json:"innerH"`
		InnerV      string `json:"innerV"`
		CrossL      string `json:"crossL"`
		CrossR      string `json:"crossR"`
		CrossT      string `json:"crossT"`
		CrossB      string `json:"crossB"`
		CrossX      string `json:"crossX"`
		InnerCrossT string `json:"innerCrossT"`
		InnerCrossB string `json:"innerCrossB"`
		InnerCrossL string `json:"innerCrossL"`
		InnerCrossR string `json:"innerCrossR"`
		BarSep      string `json:"barSep"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// Palette: convert non-empty hex strings to lipgloss.Color.
	if raw.Bg != "" {
		l.Bg = lipgloss.Color(raw.Bg)
	}
	if raw.Fg != "" {
		l.Fg = lipgloss.Color(raw.Fg)
	}
	if raw.Accent != "" {
		l.Accent = lipgloss.Color(raw.Accent)
	}
	if raw.HighlightBg != "" {
		l.HighlightBg = lipgloss.Color(raw.HighlightBg)
	}
	if raw.HighlightFg != "" {
		l.HighlightFg = lipgloss.Color(raw.HighlightFg)
	}
	if raw.ActiveBg != "" {
		l.ActiveBg = lipgloss.Color(raw.ActiveBg)
	}
	if raw.ActiveFg != "" {
		l.ActiveFg = lipgloss.Color(raw.ActiveFg)
	}
	if raw.InputBg != "" {
		l.InputBg = lipgloss.Color(raw.InputBg)
	}
	if raw.InputFg != "" {
		l.InputFg = lipgloss.Color(raw.InputFg)
	}
	if raw.DisabledFg != "" {
		l.DisabledFg = lipgloss.Color(raw.DisabledFg)
	}
	// Borders: copy raw strings.
	l.OuterTL = raw.OuterTL
	l.OuterTR = raw.OuterTR
	l.OuterBL = raw.OuterBL
	l.OuterBR = raw.OuterBR
	l.OuterH = raw.OuterH
	l.OuterV = raw.OuterV
	l.InnerH = raw.InnerH
	l.InnerV = raw.InnerV
	l.CrossL = raw.CrossL
	l.CrossR = raw.CrossR
	l.CrossT = raw.CrossT
	l.CrossB = raw.CrossB
	l.CrossX = raw.CrossX
	l.InnerCrossT = raw.InnerCrossT
	l.InnerCrossB = raw.InnerCrossB
	l.InnerCrossL = raw.InnerCrossL
	l.InnerCrossR = raw.InnerCrossR
	l.BarSep = raw.BarSep
	l.fillDefaults()
	return nil
}

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
	Name string `json:"-"`

	Primary   Layer `json:"-"`
	Secondary Layer `json:"-"`
	Tertiary  Layer `json:"-"`
	Warning   Layer `json:"-"`

	// Drop shadow (bg = shadow area, fg = half-block foreground for depth effect).
	ShadowBg color.Color `json:"-"`
	ShadowFg color.Color `json:"-"`
}

// UnmarshalJSON decodes a Theme from JSON, converting shadow color strings
// and delegating layer decoding to Layer.UnmarshalJSON.
func (t *Theme) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name      string `json:"name"`
		Primary   Layer  `json:"primary"`
		Secondary Layer  `json:"secondary"`
		Tertiary  Layer  `json:"tertiary"`
		Warning   Layer  `json:"warning"`
		ShadowBg  string `json:"shadowBg"`
		ShadowFg  string `json:"shadowFg"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.Name = raw.Name
	t.Primary = raw.Primary
	t.Secondary = raw.Secondary
	t.Tertiary = raw.Tertiary
	t.Warning = raw.Warning
	if raw.ShadowBg != "" {
		t.ShadowBg = lipgloss.Color(raw.ShadowBg)
	} else {
		t.ShadowBg = lipgloss.Color("#000000")
	}
	if raw.ShadowFg != "" {
		t.ShadowFg = lipgloss.Color(raw.ShadowFg)
	} else {
		t.ShadowFg = lipgloss.Color("#555555")
	}
	return nil
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

// ShadowStyle returns the style for drop shadow rendering.
func (t *Theme) ShadowStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(t.ShadowBg).Foreground(t.ShadowFg)
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
	return &t, nil
}

// Default returns the built-in norton theme.
func Default() *Theme {
	t := &Theme{
		Name: "norton",
		Primary: Layer{Palette: Palette{
			Bg: lipgloss.Color("#000080"), Fg: lipgloss.Color("#00AAAA"), Accent: lipgloss.Color("#FFFF55"),
			HighlightBg: lipgloss.Color("#00AAAA"), HighlightFg: lipgloss.Color("#000000"),
			ActiveBg: lipgloss.Color("#FFFF55"), ActiveFg: lipgloss.Color("#000000"),
			InputBg: lipgloss.Color("#000000"), InputFg: lipgloss.Color("#55FFFF"),
			DisabledFg: lipgloss.Color("#555555"),
		}},
		Secondary: Layer{Palette: Palette{
			Bg: lipgloss.Color("#008080"), Fg: lipgloss.Color("#000000"), Accent: lipgloss.Color("#FFFF55"),
			HighlightBg: lipgloss.Color("#000000"), HighlightFg: lipgloss.Color("#00AAAA"),
			ActiveBg: lipgloss.Color("#FFFF55"), ActiveFg: lipgloss.Color("#000000"),
			InputBg: lipgloss.Color("#000000"), InputFg: lipgloss.Color("#55FFFF"),
			DisabledFg: lipgloss.Color("#555555"),
		}},
		Tertiary: Layer{Palette: Palette{
			Bg: lipgloss.Color("#AAAAAA"), Fg: lipgloss.Color("#000000"), Accent: lipgloss.Color("#FFFF55"),
			HighlightBg: lipgloss.Color("#000000"), HighlightFg: lipgloss.Color("#FFFFFF"),
			ActiveBg: lipgloss.Color("#FFFF55"), ActiveFg: lipgloss.Color("#000000"),
			InputBg: lipgloss.Color("#000000"), InputFg: lipgloss.Color("#55FFFF"),
			DisabledFg: lipgloss.Color("#555555"),
		}},
		Warning: Layer{Palette: Palette{
			Bg: lipgloss.Color("#AA0000"), Fg: lipgloss.Color("#FFFFFF"), Accent: lipgloss.Color("#FFFF55"),
			HighlightBg: lipgloss.Color("#FFFFFF"), HighlightFg: lipgloss.Color("#AA0000"),
			ActiveBg: lipgloss.Color("#FFFF55"), ActiveFg: lipgloss.Color("#000000"),
			InputBg: lipgloss.Color("#000000"), InputFg: lipgloss.Color("#55FFFF"),
			DisabledFg: lipgloss.Color("#555555"),
		}},
		ShadowBg: lipgloss.Color("#333333"),
		ShadowFg: lipgloss.Color("#555555"),
	}
	t.Primary.fillDefaults()
	t.Secondary.fillDefaults()
	t.Tertiary.fillDefaults()
	t.Warning.fillDefaults()
	return t
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
