package engine

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mbndr/figlet4go"
)

// ─── Build info ───────────────────────────────────────────────────────────────

var (
	infoBuildDate   string // yyyy-MM-dd (first 10 chars of ISO date from -ldflags)
	infoBuildRemote string // git remote URL from -ldflags
)

// SetBuildInfo stores the build date and remote URL for use in About dialogs.
// Called once at startup from main with values injected via -ldflags.
func SetBuildInfo(date, remote string) {
	if len(date) > 10 {
		date = date[:10]
	}
	infoBuildDate = date
	infoBuildRemote = remote
}

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

// aboutLogoLines are the three rows of the "dev-null" block-character logo.
// Each row is padded with spaces to column 35 so the bracket column aligns at 35.
var aboutLogoLines = [3]string{
	"█▀▀█ █▀▀ █  █  █▀▄█ █  █ █   █     ",  // 30 chars + 5 spaces = 35
	"█  █ █▀▀ ▀▄▀   █ ▀█ █  █ █   █     ",  // 30 chars + 5 spaces = 35
	"▀▀▀  ▀▀▀  ▀    ▀  ▀  ▀▀  ▀▀▀ ▀▀▀   ", // 32 chars + 3 spaces = 35
}

// AboutLogo returns the About dialog body.
// The right column carries the remote URL split across 3 rows × 21 chars.
func AboutLogo() string {
	const (
		bracketInner = 21 // chars inside each "[ ... ]"
		totalInner   = bracketInner * 3 // 63
		sepWidth     = 60
	)

	remote := infoBuildRemote
	// Pad or truncate to exactly totalInner runes.
	remoteRunes := []rune(remote)
	if len(remoteRunes) >= totalInner {
		remoteRunes = remoteRunes[:totalInner]
	} else {
		for len(remoteRunes) < totalInner {
			remoteRunes = append(remoteRunes, ' ')
		}
	}

	sep := strings.Repeat("░", sepWidth)
	var lines []string
	lines = append(lines, sep, "")
	for i, logo := range aboutLogoLines {
		start := i * bracketInner
		slice := string(remoteRunes[start : start+bracketInner])
		lines = append(lines, logo+"[ "+slice+" ]")
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// FigletLines renders text as ASCII art and returns individual lines with
// trailing whitespace stripped from each line and trailing blank lines removed.
// Suitable for passing directly to widget.LogoButton.Lines.
func FigletLines(text, font string) []string {
	raw := Figlet(text, font)
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
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
