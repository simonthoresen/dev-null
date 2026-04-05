package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dop251/goja"

	"dev-null/internal/domain"
)

// TrimScriptExt removes the .js extension from a filename.
func TrimScriptExt(name string) string {
	return strings.TrimSuffix(name, ".js")
}

// ProbeGameTeamRange reads a game script file and extracts the Game.teamRange property
// without fully initializing the runtime. Returns zero TeamRange if not defined.
func ProbeGameTeamRange(path string) domain.TeamRange {
	src, err := os.ReadFile(path)
	if err != nil {
		return domain.TeamRange{}
	}
	vm := goja.New()
	// Register stub globals so scripts using include/log/etc. don't crash.
	baseDir := filepath.Dir(path)
	included := map[string]bool{}
	vm.Set("include", func(name string) {
		if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
			return // reject path traversal
		}
		if !strings.HasSuffix(name, ".js") {
			name += ".js"
		}
		absPath := filepath.Join(baseDir, name)
		if included[absPath] {
			return
		}
		included[absPath] = true
		inc, err := os.ReadFile(absPath)
		if err != nil {
			return // silently skip in probe mode
		}
		_, _ = vm.RunScript(name, string(inc))
	})
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"log", "chat", "chatPlayer", "teams", "gameOver", "figlet", "addMenu", "messageBox", "registerCommand", "playSound", "stopSound"} {
		vm.Set(name, noop)
	}
	_, err = vm.RunScript(path, string(src))
	if err != nil {
		return domain.TeamRange{}
	}
	gameVal := vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return domain.TeamRange{}
	}
	gameObj := gameVal.ToObject(vm)
	if gameObj == nil {
		return domain.TeamRange{}
	}
	v := gameObj.Get("teamRange")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return domain.TeamRange{}
	}
	obj := v.ToObject(vm)
	if obj == nil {
		return domain.TeamRange{}
	}
	var tr domain.TeamRange
	if mv := obj.Get("min"); mv != nil && !goja.IsUndefined(mv) {
		tr.Min = int(mv.ToInteger())
	}
	if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) {
		tr.Max = int(mv.ToInteger())
	}
	return tr
}

// ResolveGamePath resolves a game name to a file path. Checks in order:
// 1. gamesDir/<name>.js
// 2. gamesDir/<name>/main.js
func ResolveGamePath(gamesDir, name string) string {
	for _, flat := range []string{
		filepath.Join(gamesDir, name+".js"),
		filepath.Join(gamesDir, name, "main.js"),
	} {
		if _, err := os.Stat(flat); err == nil {
			return flat
		}
	}
	return filepath.Join(gamesDir, name+".js") // fallback for error message
}

// ListGames returns the names of all available games in dir: flat .js/.lua files
// and subdirectories containing a main.js or main.lua, sorted alphabetically.
// Duplicate names (e.g. foo.js and foo.lua) are deduplicated.
func ListGames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, e := range entries {
		if e.Name() == ".cache" {
			continue
		}
		if !e.IsDir() {
			if strings.HasSuffix(e.Name(), ".js") {
				add(strings.TrimSuffix(e.Name(), ".js"))
			}
		} else {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "main.js")); err == nil {
				add(e.Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// ListScripts returns the names of all .js files in dir (without extension),
// sorted alphabetically.
func ListScripts(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			names = append(names, strings.TrimSuffix(e.Name(), ".js"))
		}
	}
	sort.Strings(names)
	return names
}

// ListDir returns the name (without extension) of every file in dir that ends
// with ext, sorted alphabetically.
func ListDir(dir, ext string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(names)
	return names
}

// FormatGameList builds the game list output with team range info and compatibility markers.
func FormatGameList(gamesDir string, available []string, activeGame string, teamCount int) string {
	var lines []string
	for _, name := range available {
		path := ResolveGamePath(gamesDir, name)
		tr := ProbeGameTeamRange(path)

		// Compatibility check.
		compatible := true
		if tr.Min > 0 && teamCount < tr.Min {
			compatible = false
		}
		if tr.Max > 0 && teamCount > tr.Max {
			compatible = false
		}

		// Build the line.
		marker := "  "
		if tr.Min > 0 || tr.Max > 0 {
			if compatible {
				marker = "+ "
			} else {
				marker = "- "
			}
		}

		line := marker + name

		// Team range label.
		if tr.Min > 0 && tr.Max > 0 {
			if tr.Min == tr.Max {
				line += fmt.Sprintf("  [%d teams]", tr.Min)
			} else {
				line += fmt.Sprintf("  [%d-%d teams]", tr.Min, tr.Max)
			}
		} else if tr.Min > 0 {
			line += fmt.Sprintf("  [%d+ teams]", tr.Min)
		} else if tr.Max > 0 {
			line += fmt.Sprintf("  [up to %d teams]", tr.Max)
		}

		if name == activeGame {
			line += "  [active]"
		}

		lines = append(lines, line)
	}

	header := fmt.Sprintf("Available games (%d teams configured):", teamCount)
	return header + "\n" + strings.Join(lines, "\n")
}

