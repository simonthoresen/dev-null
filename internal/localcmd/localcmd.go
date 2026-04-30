// Package localcmd provides shared handlers for theme, plugin, shader, and
// synth commands. Both the player chrome and the server console use these;
// the only difference is the output function and the persist callback.
package localcmd

import (
	"fmt"
	"strings"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/theme"
)

// --- Theme commands ---

// HandleThemeList lists available themes across Create/Shared/Core sources.
func HandleThemeList(dataDir, currentThemeName string, output func(string)) {
	available := engine.ListAllThemes(dataDir)
	if len(available) == 0 {
		output("No themes found in Themes/")
		return
	}
	var lines []string
	for _, item := range available {
		line := "  " + item.Name + "  (" + item.Source.Label() + ")"
		if strings.EqualFold(item.Name, currentThemeName) {
			line += "  [active]"
		}
		lines = append(lines, line)
	}
	output("Available themes:\n" + strings.Join(lines, "\n"))
}

// HandleThemeLoad loads a theme by name. An empty name or "default" resets to
// the built-in default theme (themeName == ""). Returns the new theme and the
// canonical qualified id ("" for the built-in), or (nil, "") if loading failed.
func HandleThemeLoad(name, dataDir string, output func(string)) (*theme.Theme, string) {
	if name == "" || strings.EqualFold(name, "default") {
		t := theme.Default()
		output(fmt.Sprintf("Theme changed to: %s", t.Name))
		return t, ""
	}
	id, path, err := engine.ResolveTheme(dataDir, name)
	if err != nil {
		output(fmt.Sprintf("Failed to load theme: %v", err))
		return nil, ""
	}
	t, err := theme.Load(path)
	if err != nil {
		output(fmt.Sprintf("Failed to load theme: %v", err))
		return nil, ""
	}
	output(fmt.Sprintf("Theme changed to: %s", t.Name))
	return t, id
}

// --- Plugin commands ---

// HandlePluginList lists available and loaded plugins across all sources.
func HandlePluginList(dataDir string, names []string, output func(string)) {
	available := engine.ListAllScripts("Plugins", dataDir)
	loadedSet := make(map[string]bool)
	for _, n := range names {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(names) == 0 {
		output("No plugins found.")
		return
	}
	var lines []string
	for _, item := range available {
		id := engine.QualifiedName(item.Source, item.Name)
		line := "  " + item.Name + "  (" + item.Source.Label() + ")"
		if loadedSet[id] {
			line += "  [loaded]"
		}
		lines = append(lines, line)
	}
	output("Available plugins:\n" + strings.Join(lines, "\n"))
}

// findLoaded returns the index of the loaded asset matching target.
// Tries an exact qualified-id match first; falls back to base-name
// matching so callers may pass bare names (e.g. "echo" matches
// "core:echo" when there is no other "echo" loaded).
func findLoaded(names []string, target string) int {
	for i, n := range names {
		if strings.EqualFold(n, target) {
			return i
		}
	}
	_, tb, _ := engine.ParseQualifiedName(target)
	for i, n := range names {
		_, nb, _ := engine.ParseQualifiedName(n)
		if strings.EqualFold(nb, tb) {
			return i
		}
	}
	return -1
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
	if findLoaded(names, name) >= 0 {
		output(fmt.Sprintf("Plugin '%s' is already loaded.", name))
		return plugins, names, false
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
	i := findLoaded(names, target)
	if i < 0 {
		output(fmt.Sprintf("Plugin '%s' is not loaded.", target))
		return plugins, names, false
	}
	plugins[i].Unload()
	output(fmt.Sprintf("Plugin unloaded: %s", names[i]))
	return append(plugins[:i:i], plugins[i+1:]...), append(names[:i:i], names[i+1:]...), true
}

// --- Shader commands ---

// HandleShaderList lists available and active shaders across all sources.
func HandleShaderList(dataDir string, names []string, output func(string)) {
	available := engine.ListAllScripts("Shaders", dataDir)
	loadedSet := make(map[string]bool)
	for _, n := range names {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(names) == 0 {
		output("No shaders found.")
		return
	}
	var lines []string
	for _, item := range available {
		id := engine.QualifiedName(item.Source, item.Name)
		line := "  " + item.Name + "  (" + item.Source.Label() + ")"
		if loadedSet[id] {
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
	if findLoaded(names, name) >= 0 {
		output(fmt.Sprintf("Shader '%s' is already loaded.", name))
		return shaders, names, false
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
	i := findLoaded(names, target)
	if i < 0 {
		output(fmt.Sprintf("Shader '%s' is not loaded.", target))
		return shaders, names, false
	}
	shaders[i].Unload()
	output(fmt.Sprintf("Shader unloaded: %s", names[i]))
	return append(shaders[:i:i], shaders[i+1:]...), append(names[:i:i], names[i+1:]...), true
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
	idx := findLoaded(names, name)
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

// ParseCommand splits "/cmd-name arg1 arg2" into ("/cmd-name", "arg1 arg2").
func ParseCommand(text string) (string, string) {
	cmd, arg, _ := strings.Cut(strings.TrimSpace(text), " ")
	return cmd, strings.TrimSpace(arg)
}
