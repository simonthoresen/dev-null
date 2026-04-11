package datadir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeManifest writes a Manifest as JSON to the given path.
func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bundle-manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFile creates a file with the given content, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the contents of a file as a string.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBootstrapDevMode(t *testing.T) {
	installDir := t.TempDir()
	dataDir := t.TempDir()
	if err := Bootstrap(installDir, dataDir, "dev"); err != nil {
		t.Fatal(err)
	}
	// Nothing should have been written.
	if _, err := os.Stat(filepath.Join(dataDir, ".bundle-version")); !os.IsNotExist(err) {
		t.Error("expected no .bundle-version in dev mode")
	}
}

func TestBootstrapFirstRun(t *testing.T) {
	installDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "data") // does not exist yet

	// Set up install dir with two bundled files.
	writeFile(t, filepath.Join(installDir, "games", "cube.js"), "// cube game")
	writeFile(t, filepath.Join(installDir, "fonts", "big.flf"), "flf font data")

	hash1, _ := FileHash(filepath.Join(installDir, "games", "cube.js"))
	hash2, _ := FileHash(filepath.Join(installDir, "fonts", "big.flf"))
	writeManifest(t, installDir, Manifest{
		Version: "abc123",
		Files: []ManifestFile{
			{Path: "games/cube.js", SHA256: hash1},
			{Path: "fonts/big.flf", SHA256: hash2},
		},
	})

	if err := Bootstrap(installDir, dataDir, "abc123"); err != nil {
		t.Fatal(err)
	}

	// Verify files were copied.
	if got := readFile(t, filepath.Join(dataDir, "games", "cube.js")); got != "// cube game" {
		t.Errorf("cube.js = %q, want %q", got, "// cube game")
	}
	if got := readFile(t, filepath.Join(dataDir, "fonts", "big.flf")); got != "flf font data" {
		t.Errorf("big.flf = %q, want %q", got, "flf font data")
	}

	// Verify version marker.
	if got := readFile(t, filepath.Join(dataDir, ".bundle-version")); got != "abc123" {
		t.Errorf(".bundle-version = %q, want %q", got, "abc123")
	}

	// Verify manifest was copied.
	m, err := loadManifest(filepath.Join(dataDir, ".bundle-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest has %d files, want 2", len(m.Files))
	}
}

func TestBootstrapSameVersion(t *testing.T) {
	installDir := t.TempDir()
	dataDir := t.TempDir()

	// Write version marker matching current version.
	writeFile(t, filepath.Join(dataDir, ".bundle-version"), "abc123")

	// Even without a manifest, Bootstrap should no-op.
	if err := Bootstrap(installDir, dataDir, "abc123"); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapUpgrade(t *testing.T) {
	installDir := t.TempDir()
	dataDir := t.TempDir()

	// Old version: one file.
	writeFile(t, filepath.Join(dataDir, "games", "cube.js"), "// old cube")
	oldHash, _ := FileHash(filepath.Join(dataDir, "games", "cube.js"))
	writeManifest(t, dataDir, Manifest{
		Version: "old",
		Files: []ManifestFile{
			{Path: "games/cube.js", SHA256: oldHash},
		},
	})
	writeFile(t, filepath.Join(dataDir, ".bundle-version"), "old")

	// User added a custom file (not in any manifest).
	writeFile(t, filepath.Join(dataDir, "games", "custom.js"), "// user game")

	// New version: updated cube.js + new orbits.js.
	writeFile(t, filepath.Join(installDir, "games", "cube.js"), "// new cube")
	writeFile(t, filepath.Join(installDir, "games", "orbits.js"), "// orbits")
	newCubeHash, _ := FileHash(filepath.Join(installDir, "games", "cube.js"))
	newOrbitsHash, _ := FileHash(filepath.Join(installDir, "games", "orbits.js"))
	writeManifest(t, installDir, Manifest{
		Version: "new",
		Files: []ManifestFile{
			{Path: "games/cube.js", SHA256: newCubeHash},
			{Path: "games/orbits.js", SHA256: newOrbitsHash},
		},
	})

	if err := Bootstrap(installDir, dataDir, "new"); err != nil {
		t.Fatal(err)
	}

	// Updated file should be overwritten.
	if got := readFile(t, filepath.Join(dataDir, "games", "cube.js")); got != "// new cube" {
		t.Errorf("cube.js = %q, want %q", got, "// new cube")
	}
	// New file should be copied.
	if got := readFile(t, filepath.Join(dataDir, "games", "orbits.js")); got != "// orbits" {
		t.Errorf("orbits.js = %q, want %q", got, "// orbits")
	}
	// User-added file should be untouched.
	if got := readFile(t, filepath.Join(dataDir, "games", "custom.js")); got != "// user game" {
		t.Errorf("custom.js = %q, want %q", got, "// user game")
	}
	// Version marker updated.
	if got := readFile(t, filepath.Join(dataDir, ".bundle-version")); got != "new" {
		t.Errorf(".bundle-version = %q, want %q", got, "new")
	}
}

func TestBootstrapNoManifest(t *testing.T) {
	installDir := t.TempDir()
	dataDir := t.TempDir()

	// No manifest in install dir — should be a no-op, not an error.
	if err := Bootstrap(installDir, dataDir, "abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".bundle-version")); !os.IsNotExist(err) {
		t.Error("expected no .bundle-version when no manifest exists")
	}
}
