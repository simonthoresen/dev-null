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

// Theme defines the NC-style chrome palette as 4 depth layers, each with a
// background and foreground color. Theme authors choose whether layers share
// colors or are distinct.
//
//   Layer 0 — Desktop:  action bar, the "background" behind everything
//   Layer 1 — Menu:     dropdown menus pulled from the action bar
//   Layer 2 — Dialog:   modal dialog boxes
//   Layer 3 — Popup:    nested popups inside dialogs (confirmations, selectors)
//
// Additional colors: disabled text, drop shadow, and a highlight (selection)
// pair used for focused items, title bars, and active buttons.
type Theme struct {
	Name string `json:"name"`

	// Layer 0 — Desktop / action bar
	DesktopBg string `json:"desktopBg"`
	DesktopFg string `json:"desktopFg"`

	// Layer 1 — Menu (dropdown panels)
	MenuBg string `json:"menuBg"`
	MenuFg string `json:"menuFg"`

	// Layer 2 — Dialog (modal boxes)
	DialogBg string `json:"dialogBg"`
	DialogFg string `json:"dialogFg"`

	// Layer 3 — Popup (nested over dialogs)
	PopupBg string `json:"popupBg"`
	PopupFg string `json:"popupFg"`

	// Highlight — selected/focused items, title bars, active buttons
	HighlightBg string `json:"highlightBg"`
	HighlightFg string `json:"highlightFg"`

	// Disabled items
	DisabledFg string `json:"disabledFg"`

	// Drop shadow
	ShadowBg string `json:"shadowBg"`

	// Outer border (the box frame)
	OuterTL string `json:"outerTL"` // top-left corner     (default "┌")
	OuterTR string `json:"outerTR"` // top-right corner    (default "┐")
	OuterBL string `json:"outerBL"` // bottom-left corner  (default "└")
	OuterBR string `json:"outerBR"` // bottom-right corner (default "┘")
	OuterH  string `json:"outerH"`  // horizontal bar      (default "─")
	OuterV  string `json:"outerV"`  // vertical bar        (default "│")

	// Inner dividers (separators inside the box)
	InnerH string `json:"innerH"` // horizontal divider  (default "─")
	InnerV string `json:"innerV"` // vertical divider    (default "│")

	// Intersections (where inner dividers meet the outer frame)
	CrossL string `json:"crossL"` // inner-H meets outer-V on left   (default "├")
	CrossR string `json:"crossR"` // inner-H meets outer-V on right  (default "┤")
	CrossT string `json:"crossT"` // inner-V meets outer-H on top    (default "┬")
	CrossB string `json:"crossB"` // inner-V meets outer-H on bottom (default "┴")

	// Action bar separator
	BarSep string `json:"barSep"` // separator between menu titles (default "│")
}

// tc converts a hex string to a color.Color, returning fallback if empty.
func tc(hex, fallback string) color.Color {
	if hex == "" {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(hex)
}

// Layer 0 — Desktop
func (t *Theme) DesktopBgC() color.Color { return tc(t.DesktopBg, "#000080") }
func (t *Theme) DesktopFgC() color.Color { return tc(t.DesktopFg, "#AAAAAA") }

// Layer 1 — Menu
func (t *Theme) MenuBgC() color.Color { return tc(t.MenuBg, "#AAAAAA") }
func (t *Theme) MenuFgC() color.Color { return tc(t.MenuFg, "#000000") }

// Layer 2 — Dialog
func (t *Theme) DialogBgC() color.Color { return tc(t.DialogBg, "#AAAAAA") }
func (t *Theme) DialogFgC() color.Color { return tc(t.DialogFg, "#000000") }

// Layer 3 — Popup
func (t *Theme) PopupBgC() color.Color { return tc(t.PopupBg, "#AAAAAA") }
func (t *Theme) PopupFgC() color.Color { return tc(t.PopupFg, "#000000") }

// Highlight
func (t *Theme) HighlightBgC() color.Color { return tc(t.HighlightBg, "#000080") }
func (t *Theme) HighlightFgC() color.Color { return tc(t.HighlightFg, "#FFFFFF") }

// Extras
func (t *Theme) DisabledFgC() color.Color { return tc(t.DisabledFg, "#888888") }
func (t *Theme) ShadowBgC() color.Color   { return tc(t.ShadowBg, "#333333") }

// ts returns s if non-empty, otherwise fallback.
func ts(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Outer border accessors.
func (t *Theme) OTL() string { return ts(t.OuterTL, "┌") }
func (t *Theme) OTR() string { return ts(t.OuterTR, "┐") }
func (t *Theme) OBL() string { return ts(t.OuterBL, "└") }
func (t *Theme) OBR() string { return ts(t.OuterBR, "┘") }
func (t *Theme) OH() string  { return ts(t.OuterH, "─") }
func (t *Theme) OV() string  { return ts(t.OuterV, "│") }

// Inner divider accessors.
func (t *Theme) IH() string { return ts(t.InnerH, "─") }
func (t *Theme) IV() string { return ts(t.InnerV, "│") }

// Intersection accessors (inner meets outer).
func (t *Theme) XL() string { return ts(t.CrossL, "├") }
func (t *Theme) XR() string { return ts(t.CrossR, "┤") }
func (t *Theme) XT() string { return ts(t.CrossT, "┬") }
func (t *Theme) XB() string { return ts(t.CrossB, "┴") }

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
