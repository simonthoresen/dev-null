package console

import (
	"os"
	"path/filepath"
	"strings"
)

// persistServerConfig rewrites ~/.dev-null/server.txt so that all
// /theme-load, /plugin-load, and /shader-load lines are replaced by a leading
// block that restores the current selections. Other lines are preserved.
func (m *Model) persistServerConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".dev-null", "server.txt")
	persistConfigFile(path, m.themeName, m.pluginNames, m.shaderNames)
}

// managedPrefixes are command prefixes managed by persistConfigFile.
var managedPrefixes = []string{
	"/theme-", "/plugin-", "/shader-",
	// Legacy prefixes (pre-flatten) — strip on upgrade.
	"/theme ", "/plugin ", "/shader ",
}

// persistConfigFile rewrites the config file at path: strips managed lines,
// then prepends a fresh block reflecting the current state.
func persistConfigFile(path, themeName string, pluginNames, shaderNames []string) {
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
	for _, name := range pluginNames {
		managed = append(managed, "/plugin-load "+name)
	}
	for _, name := range shaderNames {
		managed = append(managed, "/shader-load "+name)
	}

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
