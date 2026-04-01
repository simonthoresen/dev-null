package server

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mbndr/figlet4go"
)

// figletRenderer is a shared AsciiRender instance (thread-safe: read-only after init).
var figletRenderer = figlet4go.NewAsciiRender()

// loadFigletFonts loads all .flf files from <dataDir>/fonts/ into the shared renderer.
func loadFigletFonts(dataDir string) {
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
