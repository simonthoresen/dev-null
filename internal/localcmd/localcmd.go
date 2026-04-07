// Package localcmd provides shared handlers for theme, plugin, shader, and
// synth commands. Both the player chrome and the server console use these;
// the only difference is the output function and the persist callback.
package localcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/theme"
)

// --- Theme commands ---

// HandleThemeList lists available themes.
func HandleThemeList(dataDir, currentThemeName string, output func(string)) {
	available := theme.ListThemes(dataDir)
	if len(available) == 0 {
		output("No themes found in themes/")
		return
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
}

// HandleThemeLoad loads a theme by name. Returns the loaded theme and its name
// (both non-nil/non-empty on success), or (nil, "") if nothing changed.
func HandleThemeLoad(name, dataDir string, output func(string)) (*theme.Theme, string) {
	path := filepath.Join(dataDir, "themes", name+".json")
	t, err := theme.Load(path)
	if err != nil {
		output(fmt.Sprintf("Failed to load theme: %v", err))
		return nil, ""
	}
	output(fmt.Sprintf("Theme changed to: %s", t.Name))
	return t, name
}

// --- Plugin commands ---

// HandlePluginList lists available and loaded plugins.
func HandlePluginList(dataDir string, names []string, output func(string)) {
	available := engine.ListScripts(filepath.Join(dataDir, "plugins"))
	loadedSet := make(map[string]bool)
	for _, n := range names {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(names) == 0 {
		output("No plugins found in plugins/")
		return
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
}

// HandlePluginLoad loads a plugin by name or URL. Returns the updated slices
// and true if a change was made.
func HandlePluginLoad(
	nameOrURL, dataDir string,
	clock domain.Clock,
	plugins []engine.Plugin,
	names []string,
	output func(string),
) ([]engine.Plugin, []string, bool) {
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
}

// HandlePluginUnload unloads a plugin by name. Returns the updated slices
// and true if a change was made.
func HandlePluginUnload(
	target string,
	plugins []engine.Plugin,
	names []string,
	output func(string),
) ([]engine.Plugin, []string, bool) {
	for i, n := range names {
		if strings.EqualFold(n, target) {
			plugins[i].Unload()
			output(fmt.Sprintf("Plugin unloaded: %s", target))
			return append(plugins[:i:i], plugins[i+1:]...), append(names[:i:i], names[i+1:]...), true
		}
	}
	output(fmt.Sprintf("Plugin '%s' is not loaded.", target))
	return plugins, names, false
}

// --- Shader commands ---

// HandleShaderList lists available and active shaders.
func HandleShaderList(dataDir string, names []string, output func(string)) {
	available := engine.ListScripts(filepath.Join(dataDir, "shaders"))
	loadedSet := make(map[string]bool)
	for _, n := range names {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(names) == 0 {
		output("No shaders found in shaders/")
		return
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
}

// HandleShaderLoad loads a shader by name or URL. Returns the updated slices
// and true if a change was made.
func HandleShaderLoad(
	nameOrURL, dataDir string,
	clock domain.Clock,
	shaders []domain.Shader,
	names []string,
	output func(string),
) ([]domain.Shader, []string, bool) {
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
}

// HandleShaderUnload unloads a shader by name. Returns the updated slices
// and true if a change was made.
func HandleShaderUnload(
	target string,
	shaders []domain.Shader,
	names []string,
	output func(string),
) ([]domain.Shader, []string, bool) {
	for i, n := range names {
		if strings.EqualFold(n, target) {
			shaders[i].Unload()
			output(fmt.Sprintf("Shader unloaded: %s", target))
			return append(shaders[:i:i], shaders[i+1:]...), append(names[:i:i], names[i+1:]...), true
		}
	}
	output(fmt.Sprintf("Shader '%s' is not loaded.", target))
	return shaders, names, false
}

// HandleShaderUp moves a shader earlier in the processing chain.
func HandleShaderUp(
	name string,
	shaders []domain.Shader,
	names []string,
	output func(string),
) ([]domain.Shader, []string, bool) {
	return moveShader(name, -1, shaders, names, output)
}

// HandleShaderDown moves a shader later in the processing chain.
func HandleShaderDown(
	name string,
	shaders []domain.Shader,
	names []string,
	output func(string),
) ([]domain.Shader, []string, bool) {
	return moveShader(name, +1, shaders, names, output)
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
