package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dev-null/internal/datadir"
)

// Source identifies which asset root a game/plugin/shader was found in.
//
// Resolution order is Create > Shared > Core: sources are checked in
// that order to detect collisions, but no source implicitly shadows
// another any more — duplicate names must be qualified by the user
// as "create/<name>", "shared/<name>", or "core/<name>".
type Source int

const (
	// SourceCreate is %USERPROFILE%/DevNull/Create — the author's git repo.
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

// Prefix returns the lowercase prefix used for qualified names, e.g.
// "create:" for SourceCreate. Empty when the source is unknown.
func (s Source) Prefix() string {
	if l := s.Label(); l != "" {
		return strings.ToLower(l) + ":"
	}
	return ""
}

// SourceOrder lists sources in canonical order (Create, Shared, Core).
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

// ParseQualifiedName checks whether a name is prefixed with "create/",
// "shared/", or "core/" (case-insensitive). When matched it returns the
// source and the unqualified base name. ok is false for plain names.
func ParseQualifiedName(name string) (Source, string, bool) {
	lower := strings.ToLower(name)
	for _, src := range SourceOrder {
		p := src.Prefix()
		if p != "" && strings.HasPrefix(lower, p) {
			return src, name[len(p):], true
		}
	}
	return 0, name, false
}

// QualifiedName returns "<src>/<name>" using the source's lowercase prefix.
func QualifiedName(src Source, name string) string {
	return src.Prefix() + name
}

// AmbiguousAssetError signals that an unqualified name exists in more
// than one source. The caller should ask the user to qualify the name
// using one of the listed candidates.
type AmbiguousAssetError struct {
	Kind       string // "game", "plugin", "shader", "theme"
	Name       string
	Candidates []Source
}

func (e *AmbiguousAssetError) Error() string {
	parts := make([]string, 0, len(e.Candidates))
	for _, s := range e.Candidates {
		parts = append(parts, QualifiedName(s, e.Name))
	}
	return fmt.Sprintf("ambiguous %s name %q (try %s)",
		e.Kind, e.Name, strings.Join(parts, " or "))
}

// IsAmbiguous reports whether err is an AmbiguousAssetError.
func IsAmbiguous(err error) bool {
	var amb *AmbiguousAssetError
	return errors.As(err, &amb)
}

// ListAllGames returns games from every configured source. Names are NOT
// deduplicated — a name appearing in multiple sources will appear once
// per source and must be qualified to load.
func ListAllGames(dataDir string) []Item {
	return listAll(dataDir, datadir.DirGames, ListGames)
}

// ListAllScripts returns plugins or shaders (kind == "Plugins"/"Shaders")
// from every configured source. Duplicates are kept; callers must
// disambiguate with qualified names.
func ListAllScripts(kind, dataDir string) []Item {
	return listAll(dataDir, kind, ListScripts)
}

// ListAllThemes returns theme names (.json) from every configured source.
// Duplicates are kept; qualify by source to disambiguate.
func ListAllThemes(dataDir string) []Item {
	lister := func(dir string) []string { return ListDir(dir, ".json") }
	return listAll(dataDir, datadir.DirThemes, lister)
}

// listAll lists items across all sources, attributing each to its source.
// Duplicates are preserved (not deduped).
func listAll(dataDir, kind string, lister func(string) []string) []Item {
	var items []Item
	for _, src := range SourceOrder {
		dir := SourceDir(src, kind, dataDir)
		if dir == "" {
			continue
		}
		for _, name := range lister(dir) {
			items = append(items, Item{Name: name, Source: src})
		}
	}
	return items
}

// ResolveGamePathInSource resolves a game name within a single source.
// Returns the computed path even if the file does not exist (the caller
// surfaces the not-found error when it tries to load).
func ResolveGamePathInSource(src Source, dataDir, name string) string {
	dir := SourceDir(src, datadir.DirGames, dataDir)
	if dir == "" {
		return filepath.Join(dataDir, datadir.DirGames, name+".js")
	}
	return ResolveGamePath(dir, name)
}

// ResolveScriptPathInSource resolves a plugin or shader within one source.
func ResolveScriptPathInSource(kind string, src Source, dataDir, name string) string {
	dir := SourceDir(src, kind, dataDir)
	if dir == "" {
		return filepath.Join(dataDir, kind, name+".js")
	}
	return filepath.Join(dir, name+".js")
}

// ResolveThemePathInSource resolves a theme within a single source.
func ResolveThemePathInSource(src Source, dataDir, name string) string {
	dir := SourceDir(src, datadir.DirThemes, dataDir)
	if dir == "" {
		return filepath.Join(dataDir, datadir.DirThemes, name+".json")
	}
	return filepath.Join(dir, name+".json")
}

// ResolveTheme resolves a theme name (qualified or bare) to its canonical
// qualified id "<src>:<name>" and absolute file path. Bare names that
// exist in multiple sources return AmbiguousAssetError; bare names that
// don't exist anywhere fall back to the Core source so the caller can
// surface a clean not-found error against a real path.
func ResolveTheme(dataDir, name string) (id, path string, err error) {
	if src, base, ok := ParseQualifiedName(name); ok {
		return QualifiedName(src, base), ResolveThemePathInSource(src, dataDir, base), nil
	}
	matches := scanSources(name, datadir.DirThemes, ".json", dataDir)
	if len(matches) > 1 {
		return "", "", &AmbiguousAssetError{Kind: "theme", Name: name, Candidates: matches}
	}
	if len(matches) == 1 {
		return QualifiedName(matches[0], name), ResolveThemePathInSource(matches[0], dataDir, name), nil
	}
	return QualifiedName(SourceCore, name),
		filepath.Join(dataDir, datadir.DirThemes, name+".json"), nil
}

// ResolveGame resolves a game name (qualified or bare) to its canonical
// qualified id and absolute file path. See ResolveTheme for semantics.
func ResolveGame(dataDir, name string) (id, path string, err error) {
	if src, base, ok := ParseQualifiedName(name); ok {
		return QualifiedName(src, base), ResolveGamePathInSource(src, dataDir, base), nil
	}
	var matches []Source
	for _, src := range SourceOrder {
		dir := SourceDir(src, datadir.DirGames, dataDir)
		if dir == "" {
			continue
		}
		p := ResolveGamePath(dir, name)
		if _, err := os.Stat(p); err == nil {
			matches = append(matches, src)
		}
	}
	if len(matches) > 1 {
		return "", "", &AmbiguousAssetError{Kind: "game", Name: name, Candidates: matches}
	}
	if len(matches) == 1 {
		return QualifiedName(matches[0], name),
			ResolveGamePathInSource(matches[0], dataDir, name), nil
	}
	return QualifiedName(SourceCore, name),
		filepath.Join(dataDir, datadir.DirGames, name+".js"), nil
}

// ResolveScript resolves a plugin or shader name (kind == "Plugins" or
// "Shaders") to its qualified id and absolute path. Same semantics as
// ResolveGame.
func ResolveScript(kind, dataDir, name string) (id, path string, err error) {
	if src, base, ok := ParseQualifiedName(name); ok {
		return QualifiedName(src, base), ResolveScriptPathInSource(kind, src, dataDir, base), nil
	}
	matches := scanSources(name, kind, ".js", dataDir)
	if len(matches) > 1 {
		return "", "", &AmbiguousAssetError{
			Kind: strings.ToLower(strings.TrimSuffix(kind, "s")),
			Name: name, Candidates: matches}
	}
	if len(matches) == 1 {
		return QualifiedName(matches[0], name),
			ResolveScriptPathInSource(kind, matches[0], dataDir, name), nil
	}
	return QualifiedName(SourceCore, name),
		filepath.Join(dataDir, kind, name+".js"), nil
}

// SourceForPath identifies which source root contains the given absolute
// file path. Used by lifecycle code that already has a path (from URL
// download or direct load) and needs to derive the qualified id.
// Falls back to SourceCore when no source root is a prefix of path.
func SourceForPath(path, kind, dataDir string) Source {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	for _, src := range SourceOrder {
		dir := SourceDir(src, kind, dataDir)
		if dir == "" {
			continue
		}
		dirAbs, err := filepath.Abs(dir)
		if err != nil {
			dirAbs = dir
		}
		dirAbs = filepath.Clean(dirAbs)
		rel, err := filepath.Rel(dirAbs, abs)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return src
		}
	}
	return SourceCore
}

// scanSources returns the sources where <dir>/<name><ext> exists.
func scanSources(name, kind, ext, dataDir string) []Source {
	var matches []Source
	for _, src := range SourceOrder {
		dir := SourceDir(src, kind, dataDir)
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name+ext)
		if _, err := os.Stat(path); err == nil {
			matches = append(matches, src)
		}
	}
	return matches
}
