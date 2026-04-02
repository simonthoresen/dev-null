package render

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"bytes"
	"os"
)

// OSC escape sequences for the null-space enhanced client protocol.
// Regular terminals silently ignore unknown OSC sequences.
//
// Format: \x1b]ns;<type>;<payload>\x07
//
// Types:
//   charmap  — base64-encoded JSON charmap definition
//   atlas    — base64-encoded gzipped PNG sprite sheet
//   viewport — game viewport bounds: x,y,w,h

// EncodeCharmapOSC returns an OSC sequence containing the charmap definition.
func EncodeCharmapOSC(def *CharMapDef) string {
	data, err := json.Marshal(def)
	if err != nil {
		return ""
	}
	return "\x1b]ns;charmap;" + base64.StdEncoding.EncodeToString(data) + "\x07"
}

// EncodeAtlasOSC returns an OSC sequence containing a gzipped PNG atlas.
func EncodeAtlasOSC(pngPath string) string {
	raw, err := os.ReadFile(pngPath)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;atlas;" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeViewportOSC returns an OSC sequence with the game viewport bounds.
func EncodeViewportOSC(x, y, w, h int) string {
	return fmt.Sprintf("\x1b]ns;viewport;%d,%d,%d,%d\x07", x, y, w, h)
}

// EncodeFrameOSC returns an OSC sequence containing a canvas frame (gzipped PNG).
func EncodeFrameOSC(pngData []byte) string {
	if len(pngData) == 0 {
		return ""
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(pngData); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;frame;" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeGameSourceOSC returns an OSC sequence containing a game source file (gzipped).
func EncodeGameSourceOSC(filename, content string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;gamesrc;" + filename + ";" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeStateOSC returns an OSC sequence containing game state (gzipped JSON).
func EncodeStateOSC(state any) string {
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;state;" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeModeOSC returns an OSC sequence to switch the client rendering mode.
func EncodeModeOSC(mode string) string {
	return "\x1b]ns;mode;" + mode + "\x07"
}

// CanvasFrameSize estimates the bandwidth in bytes for a single canvas frame
// at the given pixel dimensions. Uses empirical PNG compression ratio.
func CanvasFrameSize(pixelW, pixelH int) int {
	// Empirical: PNG of game content compresses to ~10-25% of raw RGBA.
	// Use 15% as a middle estimate. Raw = w * h * 4 bytes (RGBA).
	raw := pixelW * pixelH * 4
	return raw * 15 / 100
}

// CanvasBandwidthMbps estimates the bandwidth in Mbps for canvas rendering
// at the given cell viewport size, scale factor, and tick rate.
func CanvasBandwidthMbps(viewportCols, viewportRows, scale, ticksPerSecond int) float64 {
	pixelW := viewportCols * scale
	pixelH := viewportRows * scale
	frameBytes := CanvasFrameSize(pixelW, pixelH)
	bytesPerSec := frameBytes * ticksPerSecond
	return float64(bytesPerSec) * 8 / 1_000_000 // bits to megabits
}
