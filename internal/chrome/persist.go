package chrome

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dev-null/internal/datadir"
	"dev-null/internal/domain"
)

// persistClientConfig rewrites <ConfigDir>/client.txt so that all managed
// command lines are replaced by a leading block that restores the current
// selections. Other lines are preserved.
func (m *Model) persistClientConfig() {
	path := datadir.InitFilePath("client.txt")
	// Serialize the graphics mode as a command. Blocks is default — omit it.
	var renderPref string
	switch m.graphicsPref {
	case domain.ModeAscii:
		renderPref = "/render-ascii"
	case domain.ModePixels:
		renderPref = "/render-pixels"
	}
	// Serialize the render location. Default depends on client type:
	// GUI = local, SSH = remote. Persist only non-default.
	if m.IsEnhancedClient && !m.renderLocalPref {
		renderPref += "\n/render-remote"
	} else if !m.IsEnhancedClient && m.renderLocalPref {
		renderPref += "\n/render-local"
	}
	persistConfigFile(path, m.themeName, m.synthName, renderPref, m.chatSize, m.pluginNames, m.shaderNames)
}

// managedPrefixes are command prefixes managed by persistConfigFile.
// Lines starting with any of these are stripped and re-generated.
var managedPrefixes = []string{
	"/theme-", "/plugin-", "/shader-", "/synth-", "/render-", "/chat-size",
}

// persistConfigFile rewrites the config file at path: strips managed lines,
// then prepends a fresh block reflecting the current state.
func persistConfigFile(path, themeName, synthName, renderPref string, chatSize int, pluginNames, shaderNames []string) {
	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			tok := strings.TrimSpace(line)
			managed := false
			for _, prefix := range managedPrefixes {
				if strings.HasPrefix(tok, prefix) {
					managed = true
					break
				}
			}
			if !managed {
				kept = append(kept, line)
			}
		}
	}

	for len(kept) > 0 && strings.TrimSpace(kept[0]) == "" {
		kept = kept[1:]
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}

	var managed []string
	if themeName != "" {
		managed = append(managed, "/theme-load "+themeName)
	}
	if synthName != "" {
		managed = append(managed, "/synth-load "+synthName)
	}
	if renderPref != "" {
		managed = append(managed, renderPref)
	}
	if chatSize != 5 {
		managed = append(managed, fmt.Sprintf("/chat-size %d", chatSize))
	}
	for _, name := range pluginNames {
		managed = append(managed, "/plugin-load "+name)
	}
	for _, name := range shaderNames {
		managed = append(managed, "/shader-load "+name)
	}

	// Combine: managed first, then a blank separator, then the rest.
	var all []string
	all = append(all, managed...)
	if len(managed) > 0 && len(kept) > 0 {
		all = append(all, "")
	}
	all = append(all, kept...)

	content := strings.Join(all, "\n")
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	os.WriteFile(path, []byte(content), 0o644) //nolint:errcheck
}
