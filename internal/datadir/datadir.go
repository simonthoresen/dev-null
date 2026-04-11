// Package datadir handles data directory resolution and bootstrap.
//
// When the server or client runs from a built binary, bundled assets
// (games, fonts, themes, etc.) live in the install directory next to
// the executable. User data (saves, host keys, downloaded plugins)
// lives in a platform-specific data directory (%APPDATA%/DevNull on
// Windows). On first run or version upgrade, Bootstrap copies bundled
// assets from the install dir to the data dir.
package datadir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Manifest describes the bundled assets shipped with a build.
type Manifest struct {
	Version string         `json:"version"`
	Files   []ManifestFile `json:"files"`
}

// ManifestFile is a single entry in the bundle manifest.
type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// DefaultDataDir returns the platform-specific user data directory.
// On Windows this is %LOCALAPPDATA%/DevNull. When running via "go run"
// (exe in a temp directory), it falls back to "." for development.
func DefaultDataDir() string {
	if isGoRun() {
		return "."
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "DevNull")
	}
	// Fallback: directory of the executable.
	return exeDir()
}

// InstallDir returns the directory containing the running executable.
// When running via "go run" it falls back to ".".
func InstallDir() string {
	if isGoRun() {
		return "."
	}
	return exeDir()
}

// Bootstrap copies bundled assets from installDir to dataDir on first
// run or version upgrade. It is a no-op when buildCommit is "dev"
// (go run) or when the data dir already matches the current version.
func Bootstrap(installDir, dataDir, buildCommit string) error {
	if buildCommit == "dev" {
		return nil
	}

	// Check current version.
	versionFile := filepath.Join(dataDir, ".bundle-version")
	if cur, err := os.ReadFile(versionFile); err == nil {
		if strings.TrimSpace(string(cur)) == buildCommit {
			return nil // already up to date
		}
	}

	// Load new manifest from install dir.
	newManifest, err := loadManifest(filepath.Join(installDir, ".bundle-manifest.json"))
	if err != nil {
		// No manifest in install dir — nothing to bootstrap (e.g. bare exe).
		slog.Info("datadir: no bundle manifest in install dir, skipping bootstrap", "installDir", installDir)
		return nil
	}

	// Load old manifest from data dir (nil on first run).
	oldManifest, _ := loadManifest(filepath.Join(dataDir, ".bundle-manifest.json"))

	// Ensure data dir exists.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Build lookup of old manifest files for upgrade detection.
	oldFiles := make(map[string]string) // path → sha256
	if oldManifest != nil {
		for _, f := range oldManifest.Files {
			oldFiles[f.Path] = f.SHA256
		}
	}

	// Copy new/updated files.
	copied := 0
	for _, f := range newManifest.Files {
		if oldHash, exists := oldFiles[f.Path]; exists && oldHash == f.SHA256 {
			continue // unchanged bundled file
		}
		src := filepath.Join(installDir, f.Path)
		dst := filepath.Join(dataDir, f.Path)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", f.Path, err)
		}
		copied++
	}

	// Write manifest first, then version marker last (partial bootstrap retries).
	manifestDst := filepath.Join(dataDir, ".bundle-manifest.json")
	manifestSrc := filepath.Join(installDir, ".bundle-manifest.json")
	if err := copyFile(manifestSrc, manifestDst); err != nil {
		return fmt.Errorf("copy manifest: %w", err)
	}
	if err := os.WriteFile(versionFile, []byte(buildCommit), 0o644); err != nil {
		return fmt.Errorf("write version marker: %w", err)
	}

	if copied > 0 {
		slog.Info("datadir: bootstrap complete", "copied", copied, "version", buildCommit, "dataDir", dataDir)
	}
	return nil
}

// loadManifest reads and parses a bundle manifest JSON file.
func loadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// copyFile copies a single file, creating parent directories as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// FileHash returns the hex-encoded SHA-256 hash of a file.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isGoRun returns true if the executable is in a temp directory,
// indicating it was launched via "go run".
func isGoRun() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return strings.HasPrefix(filepath.Dir(exe), os.TempDir())
}

// exeDir returns the directory of the running executable, resolving symlinks.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
