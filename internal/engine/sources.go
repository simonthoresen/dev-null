package engine

import (
	"os"
	"path/filepath"

	"dev-null/internal/datadir"
)

// Source identifies which asset root a game/plugin/shader was found in.
//
// Resolution order is Create > Shared > Core: an in-progress create item
// shadows a name from Shared, which shadows the same name in Core.
type Source int

const (
	// SourceCreate is %USERPROFILE%/DevNull/Create — the author's git repo.
	// Highest priority so in-progress work shadows installed names.
	SourceCreate Source = iota

	// SourceShared is %USERPROFILE%/DevNull/Shared — items downloaded
	// via "Games > Add" / /game-load <url>.
	SourceShared

	// SourceCore is the data dir (defaults to %USERPROFILE%/DevNull/Core).
	// Holds bundled assets shipped with the install.
	SourceCore
)

// Label returns the display label for this source ("Create", "Shared", "Core").
func (s Source) Label() string {
	switch s {
	case SourceCreate:
		return "Create"
	case SourceShared:
		return "Shared"
	case SourceCore:
		return "Core"
	}
	return ""
}

// SourceOrder lists sources in resolution priority (highest first).
var SourceOrder = []Source{SourceCreate, SourceShared, SourceCore}

// SourceDir returns the directory containing items of the given asset kind
// ("Games", "Plugins", "Shaders") for the given source. Returns "" when
// the source root is not configured (e.g. SourceCreate when the author
// hasn't run DevNullCreate.ps1 yet).
func SourceDir(src Source, kind, dataDir string) string {
	switch src {
	case SourceCreate:
		if create := datadir.CreateDir(); create != "" {
			return filepath.Join(create, kind)
		}
		return ""
	case SourceShared:
		return filepath.Join(datadir.SharedDir(), kind)
	case SourceCore:
		return filepath.Join(dataDir, kind)
	}
	return ""
}

// Item describes a discovered asset with its source attribution.
type Item struct {
	Name   string
	Source Source
}

// ListAllGames returns games from every configured source in priority
// order. Names are deduplicated: a name appearing in a higher-priority
// source shadows the same name in a lower-priority one.
func ListAllGames(dataDir string) []Item {
	return listAll(dataDir, datadir.DirGames, ListGames)
}

// ListAllScripts returns plugins or shaders (kind == "Plugins"/"Shaders")
// from every configured source in priority order, with deduplication.
func ListAllScripts(kind, dataDir string) []Item {
	return listAll(dataDir, kind, ListScripts)
}

// ListAllThemes returns theme names (.json) from every configured source in
// priority order, with deduplication. Uses the same Create > Shared > Core
// resolution as other assets so authors can place themes in Create/Shared.
func ListAllThemes(dataDir string) []Item {
	// Use a small wrapper to adapt ListDir(dir, ext) to the expected lister
	// signature used by listAll.
	lister := func(dir string) []string { return ListDir(dir, ".json") }
	return listAll(dataDir, datadir.DirThemes, lister)
}

// ResolveThemePathAll walks Create > Shared > Core and returns the first
// matching theme JSON path. Falls back to the Core-source path for error
// messages even if no file exists there.
func ResolveThemePathAll(dataDir, name string) string {
	for _, src := range SourceOrder {
		dir := SourceDir(src, datadir.DirThemes, dataDir)
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name+".json")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(dataDir, datadir.DirThemes, name+".json")
}

// listAll is the shared multi-source listing helper.
func listAll(dataDir, kind string, lister func(string) []string) []Item {
	seen := map[string]bool{}
	var items []Item
	for _, src := range SourceOrder {
		dir := SourceDir(src, kind, dataDir)
		if dir == "" {
			continue
		}
		for _, name := range lister(dir) {
			if seen[name] {
				continue
			}
			seen[name] = true
			items = append(items, Item{Name: name, Source: src})
		}
	}
	return items
}

// ResolveGamePathAll walks Create > Shared > Core and returns the first
// matching path. Falls back to the Core-source path for error messages
// even if no file exists there.
func ResolveGamePathAll(dataDir, name string) string {
	for _, src := range SourceOrder {
		dir := SourceDir(src, datadir.DirGames, dataDir)
		if dir == "" {
			continue
		}
		path := ResolveGamePath(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(dataDir, datadir.DirGames, name+".js")
}

// ResolveScriptPathAll walks Create > Shared > Core for a plugin or
// shader file (kind == "Plugins"/"Shaders"). Falls back to the
// Core-source path for error messages even if no file exists.
func ResolveScriptPathAll(kind, dataDir, name string) string {
	for _, src := range SourceOrder {
		dir := SourceDir(src, kind, dataDir)
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name+".js")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(dataDir, kind, name+".js")
}
