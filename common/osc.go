package common

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
