package localcmd

import (
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/widget"
)

// ─── Standalone "Add..." dialogs for sub-menu use ────────────────────────────

// PushGameAddDialog opens an input dialog for adding a game by name or URL.
func PushGameAddDialog(overlay *widget.OverlayState, dataDir string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Game",
		Body:        "Enter a game name or URL:",
		InputPrompt: "Game",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushThemeAddDialog opens an input dialog for loading a theme by name.
func PushThemeAddDialog(overlay *widget.OverlayState, dataDir string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Theme",
		Body:        "Enter a theme name:",
		InputPrompt: "Theme",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushScriptAddDialog opens an input dialog for adding a plugin or shader.
func PushScriptAddDialog(overlay *widget.OverlayState, noun string, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add " + noun,
		Body:        "Enter a " + strings.ToLower(noun) + " name or URL:",
		InputPrompt: noun,
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushSynthAddDialog opens an input dialog for adding a SoundFont.
func PushSynthAddDialog(overlay *widget.OverlayState, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add SoundFont",
		Body:        "Enter a SoundFont name:",
		InputPrompt: "SoundFont",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}

// PushFontAddDialog opens an input dialog for adding a Figlet font.
func PushFontAddDialog(overlay *widget.OverlayState, onLoad func(name string)) {
	overlay.PushDialog(domain.DialogRequest{
		Title:       "Add Font",
		Body:        "Enter a font name:",
		InputPrompt: "Font",
		Buttons:     []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			value = strings.TrimSpace(value)
			if btn == "Load" && value != "" && onLoad != nil {
				onLoad(value)
			}
		},
	})
}
