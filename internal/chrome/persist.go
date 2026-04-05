package chrome

import (
	"os"
	"path/filepath"
	"strings"
)

// persistClientConfig rewrites ~/.dev-null/client.txt so that all
// /theme, /plugin, and /shader lines are replaced by a leading block
// that restores the current selections. All other lines are preserved.
func (m *Model) persistClientConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".dev-null", "client.txt")
	persistConfigFile(path, m.themeName, m.pluginNames, m.shaderNames)
}

// persistConfigFile rewrites the config file at path: strips every line whose
// first non-space token is /theme, /plugin, or /shader, then prepends a fresh
// block of those commands reflecting the current state.
func persistConfigFile(path, themeName string, pluginNames, shaderNames []string) {
	// Read existing content (ignore error — file may not exist yet).
	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			tok := strings.TrimSpace(line)
			if strings.HasPrefix(tok, "/theme") ||
				strings.HasPrefix(tok, "/plugin") ||
				strings.HasPrefix(tok, "/shader") {
				continue
			}
			kept = append(kept, line)
		}
	}

	// Strip leading and trailing blank lines from the kept block.
	for len(kept) > 0 && strings.TrimSpace(kept[0]) == "" {
		kept = kept[1:]
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}

	// Build the managed block.
	var managed []string
	if themeName != "" {
		managed = append(managed, "/theme "+themeName)
	}
	for _, name := range pluginNames {
		managed = append(managed, "/plugin load "+name)
	}
	for _, name := range shaderNames {
		managed = append(managed, "/shader load "+name)
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
