package server

import (
	"github.com/mbndr/figlet4go"
)

// renderer is a shared AsciiRender instance (thread-safe: read-only after init).
var figletRenderer = figlet4go.NewAsciiRender()

// Figlet renders text as ASCII art using the named font.
// Built-in fonts: "standard", "larry3d". Falls back to "standard" for unknown fonts.
// Returns an empty string if rendering fails.
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
