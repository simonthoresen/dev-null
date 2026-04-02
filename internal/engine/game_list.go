package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dop251/goja"

	"null-space/common"
)

// ProbeGameTeamRange reads a game JS file and extracts the Game.teamRange property
// without fully initializing the runtime. Returns zero TeamRange if not defined.
func ProbeGameTeamRange(path string) common.TeamRange {
	src, err := os.ReadFile(path)
	if err != nil {
		return common.TeamRange{}
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
	for _, name := range []string{"log", "chat", "chatPlayer", "teams", "gameOver", "figlet", "addMenu", "messageBox", "registerCommand"} {
		vm.Set(name, noop)
	}
	_, err = vm.RunScript(path, string(src))
	if err != nil {
		return common.TeamRange{}
	}
	gameVal := vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return common.TeamRange{}
	}
	gameObj := gameVal.ToObject(vm)
	if gameObj == nil {
		return common.TeamRange{}
	}
	v := gameObj.Get("teamRange")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return common.TeamRange{}
	}
	obj := v.ToObject(vm)
	if obj == nil {
		return common.TeamRange{}
	}
	var tr common.TeamRange
	if mv := obj.Get("min"); mv != nil && !goja.IsUndefined(mv) {
		tr.Min = int(mv.ToInteger())
	}
	if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) {
		tr.Max = int(mv.ToInteger())
	}
	return tr
}

// ResolveGamePath resolves a game name to a file path. It checks for:
// 1. gamesDir/<name>.js  (flat single-file game)
// 2. gamesDir/<name>/main.js  (folder-based multi-file game)
func ResolveGamePath(gamesDir, name string) string {
	flat := filepath.Join(gamesDir, name+".js")
	if _, err := os.Stat(flat); err == nil {
		return flat
	}
	folder := filepath.Join(gamesDir, name, "main.js")
	if _, err := os.Stat(folder); err == nil {
		return folder
	}
	return flat // return the flat path so the error message makes sense
}

// ListGames returns the names of all available games in dir: both flat .js files
// and subdirectories containing a main.js, sorted alphabetically.
func ListGames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Name() == ".cache" {
			continue
		}
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			names = append(names, strings.TrimSuffix(e.Name(), ".js"))
		} else if e.IsDir() {
			// Check for main.js inside the directory.
			mainJS := filepath.Join(dir, e.Name(), "main.js")
			if _, err := os.Stat(mainJS); err == nil {
				names = append(names, e.Name())
			}
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
