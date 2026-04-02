package engine

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mbndr/figlet4go"
)

// figletRenderer is a shared AsciiRender instance (thread-safe: read-only after init).
var figletRenderer = figlet4go.NewAsciiRender()

// LoadFigletFonts loads all .flf files from <dataDir>/fonts/ into the shared renderer.
func LoadFigletFonts(dataDir string) {
	dir := filepath.Join(dataDir, "fonts")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	if err := figletRenderer.LoadFont(dir); err != nil {
		slog.Warn("Failed to load figlet fonts", "dir", dir, "error", err)
		return
	}
	slog.Info("Loaded figlet fonts", "dir", dir)
}

// AboutLogo returns the null-space ASCII art logo for the About dialog.
// Uses figlet if available, otherwise falls back to a hardcoded logo.
func AboutLogo() string {
	logo := Figlet("null-space", "")
	if logo != "" {
		// Strip ANSI codes and trim trailing whitespace.
		var lines []string
		for _, l := range strings.Split(logo, "\n") {
			lines = append(lines, strings.TrimRight(l, " \t\r"))
		}
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	return `             __ __
   ___  __ __/ // /  ___ ___  ___ ________
  / _ \/ // / // /_ (_-</ _ \/ _ '/ __/ -_)
 /_//_/\_,_/____/ /___/ .__/\_,_/\__/\__/
                     /_/`
}

// Figlet renders text as ASCII art using the named font.
// Built-in fonts: "standard", "larry3d". Any font from dist/fonts/ is also available.
// Falls back to "standard" for unknown fonts. Returns an empty string if rendering fails.
func Figlet(text, font string) string {
	opts := figlet4go.NewRenderOptions()
	if font != "" {
		opts.FontName = font
	}
	result, err := figletRenderer.RenderOpts(text, opts)
	if err != nil {
		return ""
	}
	return result
}
