package chrome

import (
	"os"
	"path/filepath"
	"strings"
)

// persistClientConfig rewrites ~/.dev-null/client.txt so that all
// /theme-load, /plugin-load, /shader-load, and /synth-load lines are replaced
// by a leading block that restores the current selections. Other lines are preserved.
func (m *Model) persistClientConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".dev-null", "client.txt")
	persistConfigFile(path, m.themeName, m.synthName, m.pluginNames, m.shaderNames)
}

// managedPrefixes are command prefixes managed by persistConfigFile.
// Lines starting with any of these are stripped and re-generated.
var managedPrefixes = []string{
	"/theme-", "/plugin-", "/shader-", "/synth-",
	// Legacy prefixes (pre-flatten) — strip on upgrade.
	"/theme ", "/plugin ", "/shader ", "/synth ",
}

// persistConfigFile rewrites the config file at path: strips managed lines,
// then prepends a fresh block reflecting the current state.
func persistConfigFile(path, themeName, synthName string, pluginNames, shaderNames []string) {
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
