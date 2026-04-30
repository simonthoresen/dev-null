package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"dev-null/internal/domain"
)

// validNameRe matches names that are safe for use in file paths:
// alphanumeric, hyphens, underscores, and dots, optionally prefixed
// by a source qualifier like "create:" or "core:" or "shared:".
// The qualifier is rejected for path use without sanitisation — callers
// that build filesystem paths must run names through SanitizeForPath.
var validNameRe = regexp.MustCompile(
	`^([a-zA-Z][a-zA-Z0-9]*:)?[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateName checks that a name is safe for use in file paths.
// Rejects empty strings, path traversal (.. or separators), and non-alphanumeric starts.
// Accepts an optional "<source>:" qualifier prefix.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name must not contain '..'")
	}
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("name %q contains invalid characters (allowed: a-z, 0-9, '-', '_', '.', optional 'source:' prefix)", name)
	}
	return nil
}

// SanitizeForPath converts a (possibly qualified) name into a form safe
// to embed in a filesystem path on all platforms by replacing the ':'
// qualifier separator with '__'. Idempotent for non-qualified names.
func SanitizeForPath(name string) string {
	return strings.ReplaceAll(name, ":", "__")
}

// LoadGameState reads the saved state for a game from dist/state/<gameName>.json.
// Returns nil (no error) if the file does not exist.
func LoadGameState(dataDir, gameName string) (any, error) {
	if err := ValidateName(gameName); err != nil {
		return nil, fmt.Errorf("invalid game name: %w", err)
	}
	path := filepath.Join(dataDir, "state", SanitizeForPath(gameName)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read game state: %w", err)
	}
	var s any
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse game state: %w", err)
	}
	return s, nil
}

// --- Suspend saves ---
// Suspend saves are stored in dist/state/saves/<gameName>/<saveName>.json.
// They are independent of the per-game global state (high scores, etc.).

// SuspendSave is the on-disk format for a suspended game session.
type SuspendSave struct {
	GameName     string        `json:"gameName"`
	SaveName     string        `json:"saveName"`
	SavedAt      time.Time     `json:"savedAt"`
	Teams        []domain.Team `json:"teams"`
	Disconnected map[string]string `json:"disconnected,omitempty"`
	GameState    any           `json:"gameState,omitempty"`
}

// SuspendInfo is a lightweight summary returned by ListSuspends.
type SuspendInfo struct {
	GameName  string
	SaveName  string
	SavedAt   time.Time
	TeamCount int
}

func suspendDir(dataDir, gameName string) string {
	return filepath.Join(dataDir, "state", "saves", SanitizeForPath(gameName))
}

func suspendPath(dataDir, gameName, saveName string) string {
	return filepath.Join(suspendDir(dataDir, gameName), saveName+".json")
}

// SaveSuspend writes a suspend save to disk.
func SaveSuspend(dataDir string, save *SuspendSave) error {
	if err := ValidateName(save.GameName); err != nil {
		return fmt.Errorf("invalid game name: %w", err)
	}
	if err := ValidateName(save.SaveName); err != nil {
		return fmt.Errorf("invalid save name: %w", err)
	}
	dir := suspendDir(dataDir, save.GameName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create suspend dir: %w", err)
	}
	data, err := json.MarshalIndent(save, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal suspend save: %w", err)
	}
	return os.WriteFile(suspendPath(dataDir, save.GameName, save.SaveName), data, 0o644)
}

// LoadSuspend reads a suspend save from disk.
func LoadSuspend(dataDir, gameName, saveName string) (*SuspendSave, error) {
	if err := ValidateName(gameName); err != nil {
		return nil, fmt.Errorf("invalid game name: %w", err)
	}
	if err := ValidateName(saveName); err != nil {
		return nil, fmt.Errorf("invalid save name: %w", err)
	}
	data, err := os.ReadFile(suspendPath(dataDir, gameName, saveName))
	if err != nil {
		return nil, err
	}
	var save SuspendSave
	if err := json.Unmarshal(data, &save); err != nil {
		return nil, fmt.Errorf("parse suspend save: %w", err)
	}
	return &save, nil
}

// DeleteSuspend removes a suspend save from disk.
func DeleteSuspend(dataDir, gameName, saveName string) error {
	if err := ValidateName(gameName); err != nil {
		return fmt.Errorf("invalid game name: %w", err)
	}
	if err := ValidateName(saveName); err != nil {
		return fmt.Errorf("invalid save name: %w", err)
	}
	return os.Remove(suspendPath(dataDir, gameName, saveName))
}

// ListSuspends returns all suspend saves, optionally filtered by game name.
// If gameName is empty, returns saves for all games. The qualified game id
// stored inside each save file is what populates SuspendInfo.GameName;
// on-disk directory names are sanitised so a single qualifier may use
// either form.
func ListSuspends(dataDir string, gameName string) []SuspendInfo {
	savesRoot := filepath.Join(dataDir, "state", "saves")
	var gameDirs []string
	if gameName != "" {
		gameDirs = []string{SanitizeForPath(gameName)}
	} else {
		entries, err := os.ReadDir(savesRoot)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if e.IsDir() {
				gameDirs = append(gameDirs, e.Name())
			}
		}
	}

	var results []SuspendInfo
	for _, gn := range gameDirs {
		dir := filepath.Join(savesRoot, gn)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			sn := strings.TrimSuffix(e.Name(), ".json")
			// Read directly: gn here is already the on-disk (sanitized) form.
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var save SuspendSave
			if err := json.Unmarshal(data, &save); err != nil {
				continue
			}
			_ = sn
			results = append(results, SuspendInfo{
				GameName:  save.GameName,
				SaveName:  save.SaveName,
				SavedAt:   save.SavedAt,
				TeamCount: len(save.Teams),
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].SavedAt.After(results[j].SavedAt)
	})
	return results
}

// ListSuspendNames returns save names for tab completion, formatted as "gameName/saveName".
func ListSuspendNames(dataDir string) []string {
	saves := ListSuspends(dataDir, "")
	names := make([]string, len(saves))
	for i, s := range saves {
		names[i] = s.GameName + "/" + s.SaveName
	}
	return names
}

// SaveGameState writes game state to dist/state/<gameName>.json.
// Creates the state/ directory if it does not exist. Does nothing if state is nil.
func SaveGameState(dataDir, gameName string, s any) error {
	if s == nil {
		return nil
	}
	if err := ValidateName(gameName); err != nil {
		return fmt.Errorf("invalid game name: %w", err)
	}
	dir := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal game state: %w", err)
	}
	path := filepath.Join(dir, SanitizeForPath(gameName)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write game state: %w", err)
	}
	return nil
}
