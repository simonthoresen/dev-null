package console

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"dev-null/internal/datadir"
)

// persistServerConfig rewrites <ConfigDir>/server.txt so that all
// /theme-load, /plugin-load, and /shader-load lines are replaced by a leading
// block that restores the current selections. Other lines are preserved.
func (m *Model) persistServerConfig() {
	persistConfigFile(datadir.InitFilePath("server.txt"), m.themeName, m.pluginNames, m.shaderNames)
}

// readInitFile reads an init file by name (e.g. "server.txt"). Tries the
// new <ConfigDir>/<name> first; if absent, falls back to the legacy
// ~/.dev-null/<name> and copies it forward (one-time migration) so subsequent
// writes don't lose its non-managed lines. Returns (contents, true) on success.
func readInitFile(name string) ([]byte, bool) {
	newPath := datadir.InitFilePath(name)
	if data, err := os.ReadFile(newPath); err == nil {
		return data, true
	}
	legacy := datadir.LegacyInitFilePath(name)
	if legacy == "" {
		return nil, false
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		return nil, false
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err == nil {
		if err := os.WriteFile(newPath, data, 0o644); err == nil {
			slog.Info("console: migrated legacy init file", "from", legacy, "to", newPath)
		}
	}
	return data, true
}

// managedPrefixes are command prefixes managed by persistConfigFile.
var managedPrefixes = []string{
	"/theme-", "/plugin-", "/shader-",
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
