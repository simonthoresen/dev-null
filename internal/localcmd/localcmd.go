// Package localcmd provides shared handlers for /theme, /plugin, and /shader
// commands. Both the player chrome and the server console use these; the only
// difference is the output function and the persist callback.
package localcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/theme"
)

// HandleTheme processes a /theme command. Returns the loaded theme and its name
// (both non-nil/non-empty on success), or (nil, "") if nothing changed.
func HandleTheme(input, dataDir, currentThemeName string, output func(string)) (*theme.Theme, string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := theme.ListThemes(dataDir)
		if len(available) == 0 {
			output("No themes found in themes/")
			return nil, ""
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if strings.EqualFold(name, currentThemeName) {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		output("Available themes:\n" + strings.Join(lines, "\n"))
		return nil, ""
	}
	name := parts[1]
	path := filepath.Join(dataDir, "themes", name+".json")
	t, err := theme.Load(path)
	if err != nil {
		output(fmt.Sprintf("Failed to load theme: %v", err))
		return nil, ""
	}
	output(fmt.Sprintf("Theme changed to: %s", t.Name))
	return t, name
}

// HandlePlugin processes a /plugin command. Returns the updated plugins slice,
// names slice, and whether a change was made (true = caller should persist).
func HandlePlugin(
	input, dataDir string,
	clock domain.Clock,
	plugins []engine.Plugin,
	names []string,
	output func(string),
) ([]engine.Plugin, []string, bool) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := engine.ListScripts(filepath.Join(dataDir, "plugins"))
		loadedSet := make(map[string]bool)
		for _, n := range names {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(names) == 0 {
			output("No plugins found in plugins/")
			return plugins, names, false
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [loaded]"
			}
			lines = append(lines, line)
		}
		output("Available plugins:\n" + strings.Join(lines, "\n"))
		return plugins, names, false
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			output("Usage: /plugin load <name|url>")
			return plugins, names, false
		}
		nameOrURL := parts[2]
		name, path, err := engine.ResolvePluginPath(nameOrURL, dataDir)
		if err != nil {
			output(fmt.Sprintf("Failed: %v", err))
			return plugins, names, false
		}
		for _, n := range names {
			if strings.EqualFold(n, name) {
				output(fmt.Sprintf("Plugin '%s' is already loaded.", name))
				return plugins, names, false
			}
		}
		pl, err := engine.LoadPlugin(path, clock)
		if err != nil {
			output(fmt.Sprintf("Failed to load plugin: %v", err))
			return plugins, names, false
		}
		output(fmt.Sprintf("Plugin loaded: %s", name))
		return append(plugins, pl), append(names, name), true
	case "unload":
		if len(parts) < 3 {
			output("Usage: /plugin unload <name>")
			return plugins, names, false
		}
		target := parts[2]
		for i, n := range names {
			if strings.EqualFold(n, target) {
				plugins[i].Unload()
				output(fmt.Sprintf("Plugin unloaded: %s", target))
				return append(plugins[:i:i], plugins[i+1:]...), append(names[:i:i], names[i+1:]...), true
			}
		}
		output(fmt.Sprintf("Plugin '%s' is not loaded.", target))
		return plugins, names, false
	case "list":
		if len(names) == 0 {
			output("No plugins currently loaded.")
			return plugins, names, false
		}
		output("Loaded plugins: " + strings.Join(names, ", "))
		return plugins, names, false
	default:
		output(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", parts[1]))
		return plugins, names, false
	}
}

// HandleShader processes a /shader command. Returns the updated shaders slice,
// names slice, and whether a change was made (true = caller should persist).
func HandleShader(
	input, dataDir string,
	clock domain.Clock,
	shaders []domain.Shader,
	names []string,
	output func(string),
) ([]domain.Shader, []string, bool) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := engine.ListScripts(filepath.Join(dataDir, "shaders"))
		loadedSet := make(map[string]bool)
		for _, n := range names {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(names) == 0 {
			output("No shaders found in shaders/")
			return shaders, names, false
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		output("Available shaders:\n" + strings.Join(lines, "\n"))
		return shaders, names, false
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			output("Usage: /shader load <name|url>")
			return shaders, names, false
		}
		nameOrURL := parts[2]
		name, path, err := engine.ResolveShaderPath(nameOrURL, dataDir)
		if err != nil {
			output(fmt.Sprintf("Failed: %v", err))
			return shaders, names, false
		}
		for _, n := range names {
			if strings.EqualFold(n, name) {
				output(fmt.Sprintf("Shader '%s' is already loaded.", name))
				return shaders, names, false
			}
		}
		sh, err := engine.LoadShader(path, clock)
		if err != nil {
			output(fmt.Sprintf("Failed to load shader: %v", err))
			return shaders, names, false
		}
		output(fmt.Sprintf("Shader loaded: %s", name))
		return append(shaders, sh), append(names, name), true
	case "unload":
		if len(parts) < 3 {
			output("Usage: /shader unload <name>")
			return shaders, names, false
		}
		target := parts[2]
		for i, n := range names {
			if strings.EqualFold(n, target) {
				shaders[i].Unload()
				output(fmt.Sprintf("Shader unloaded: %s", target))
				return append(shaders[:i:i], shaders[i+1:]...), append(names[:i:i], names[i+1:]...), true
			}
		}
		output(fmt.Sprintf("Shader '%s' is not loaded.", target))
		return shaders, names, false
	case "list":
		if len(names) == 0 {
			output("No shaders currently loaded.")
			return shaders, names, false
		}
		var lines []string
		for i, name := range names {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		output("Active shaders (in order):\n" + strings.Join(lines, "\n"))
		return shaders, names, false
	case "up":
		if len(parts) < 3 {
			output("Usage: /shader up <name>")
			return shaders, names, false
		}
		return moveShader(parts[2], -1, shaders, names, output)
	case "down":
		if len(parts) < 3 {
			output("Usage: /shader down <name>")
			return shaders, names, false
		}
		return moveShader(parts[2], +1, shaders, names, output)
	default:
		output(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list, up, down", parts[1]))
		return shaders, names, false
	}
}

func moveShader(name string, delta int, shaders []domain.Shader, names []string, output func(string)) ([]domain.Shader, []string, bool) {
	idx := -1
	for i, n := range names {
		if strings.EqualFold(n, name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		output(fmt.Sprintf("Shader '%s' is not loaded.", name))
		return shaders, names, false
	}
	newIdx := idx + delta
	if newIdx < 0 || newIdx >= len(names) {
		output(fmt.Sprintf("Shader '%s' is already at position %d.", name, idx+1))
		return shaders, names, false
	}
	shaders[idx], shaders[newIdx] = shaders[newIdx], shaders[idx]
	names[idx], names[newIdx] = names[newIdx], names[idx]
	output(fmt.Sprintf("Shader '%s' moved to position %d.", name, newIdx+1))
	return shaders, names, true
}
