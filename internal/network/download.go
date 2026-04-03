package network

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var githubBlobRe = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/blob/(.+)$`)

// IsURL returns true if s is an HTTPS URL.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "https://")
}

// ToRawURL converts a GitHub blob URL to its raw.githubusercontent.com equivalent.
// raw.githubusercontent.com and other HTTPS URLs are returned unchanged.
// Non-HTTPS URLs return an error.
func ToRawURL(u string) (string, error) {
	if !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("only HTTPS URLs are supported")
	}
	if m := githubBlobRe.FindStringSubmatch(u); m != nil {
		return "https://raw.githubusercontent.com/" + m[1] + "/" + m[2], nil
	}
	return u, nil
}

// DownloadToCache downloads the JS file at rawURL into cacheDir and returns
// the local file path. Re-downloading the same URL overwrites the cached copy.
// The cache filename is derived from the last path segment of the URL.
func DownloadToCache(rawURL, cacheDir string) (string, error) {
	downloadURL, err := ToRawURL(rawURL)
	if err != nil {
		return "", err
	}

	// Derive filename from the last segment of the URL path.
	urlPath := strings.Split(downloadURL, "?")[0]
	urlPath = strings.TrimRight(urlPath, "/")
	parts := strings.Split(urlPath, "/")
	filename := SanitizeFilename(parts[len(parts)-1])
	if filename == "" {
		return "", fmt.Errorf("could not derive a filename from URL")
	}
	if !strings.HasSuffix(filename, ".js") {
		filename += ".js"
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %s", resp.Status)
	}

	const maxSize = 1 << 20 // 1 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxSize {
		return "", fmt.Errorf("file too large (max 1 MB)")
	}

	localPath := filepath.Join(cacheDir, filename)
	if err := os.WriteFile(localPath, body, 0o644); err != nil {
		return "", fmt.Errorf("write cache: %w", err)
	}

	return localPath, nil
}

// IsZipURL returns true if the URL points to a .zip file.
func IsZipURL(u string) bool {
	// Strip query string and fragment.
	path := strings.Split(u, "?")[0]
	path = strings.Split(path, "#")[0]
	return strings.HasSuffix(strings.ToLower(path), ".zip")
}

// DownloadAndExtractZip downloads a zip file, extracts it to gamesDir/<name>/,
// and returns the path to main.js inside. The zip must contain a main.js at
// the root (or inside a single top-level directory).
func DownloadAndExtractZip(rawURL, gamesDir string) (string, error) {
	downloadURL, err := ToRawURL(rawURL)
	if err != nil {
		return "", err
	}

	// Derive game name from URL filename (minus .zip).
	urlPath := strings.Split(downloadURL, "?")[0]
	urlPath = strings.TrimRight(urlPath, "/")
	parts := strings.Split(urlPath, "/")
	filename := SanitizeFilename(parts[len(parts)-1])
	name := strings.TrimSuffix(filename, ".zip")
	if name == "" {
		return "", fmt.Errorf("could not derive game name from URL")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %s", resp.Status)
	}

	const maxSize = 10 << 20 // 10 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxSize {
		return "", fmt.Errorf("zip too large (max 10 MB)")
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}

	destDir := filepath.Join(gamesDir, name)
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("clean dest: %w", err)
	}

	const maxUncompressed = 100 << 20 // 100 MB uncompressed limit
	const maxFiles = 10_000           // max number of files in archive
	var totalBytes int64
	var fileCount int

	for _, f := range reader.File {
		// Security: skip entries with path traversal.
		if strings.Contains(f.Name, "..") {
			continue
		}

		fileCount++
		if fileCount > maxFiles {
			return "", fmt.Errorf("zip contains too many files (max %d)", maxFiles)
		}

		target := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("create dir: %w", err)
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxUncompressed-totalBytes+1))
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f.Name, err)
		}
		totalBytes += int64(len(data))
		if totalBytes > maxUncompressed {
			return "", fmt.Errorf("zip uncompressed content too large (max %d MB)", maxUncompressed>>20)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", f.Name, err)
		}
	}

	// Look for main.js at the root of the extracted directory.
	mainJS := filepath.Join(destDir, "main.js")
	if _, err := os.Stat(mainJS); err == nil {
		return mainJS, nil
	}

	// Check if the zip had a single top-level directory wrapping everything.
	entries, _ := os.ReadDir(destDir)
	if len(entries) == 1 && entries[0].IsDir() {
		nested := filepath.Join(destDir, entries[0].Name(), "main.js")
		if _, err := os.Stat(nested); err == nil {
			return nested, nil
		}
	}

	return "", fmt.Errorf("zip must contain main.js at root")
}

// SanitizeFilename keeps only characters safe for a cross-platform filename.
func SanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
