package server

import (
	"bytes"
	"io"
)

// kittyStripWriter wraps an io.Writer and strips Kitty keyboard protocol
// escape sequences from the output stream. Bubble Tea v2's renderer always
// enables KittyDisambiguateEscapeCodes, which causes the client terminal to
// send multi-byte CSI u-encoded keystrokes. Over SSH, this breaks input
// parsing — the server's Bubble Tea input reader misinterprets or drops
// alternating keys. By stripping the enable/disable sequences from the output,
// the client terminal stays in legacy mode and sends simple 1-byte ASCII keys.
type kittyStripWriter struct {
	inner io.Writer
}

// Kitty keyboard protocol sequences to strip from output:
//
//	\x1b[>Nu   — Push keyboard mode (enable), where N is flags
//	\x1b[<u    — Pop keyboard mode (disable)
//	\x1b[?u    — Query keyboard mode
//
// We also strip modifyOtherKeys:
//
//	\x1b[>4;2m — Set modifyOtherKeys mode 2
//	\x1b[>4;0m — Reset modifyOtherKeys mode (or \x1b[>4m)
var kittyPatterns = [][]byte{
	// Common Kitty keyboard enable/disable patterns.
	// The renderer typically writes \x1b[>1u (flags=1, mode=1 push).
	[]byte("\x1b[>1u"),
	[]byte("\x1b[>3u"),
	[]byte("\x1b[>0u"),
	[]byte("\x1b[<u"),
	[]byte("\x1b[?u"),
	// modifyOtherKeys
	[]byte("\x1b[>4;2m"),
	[]byte("\x1b[>4;0m"),
	[]byte("\x1b[>4m"),
}

func newKittyStripWriter(w io.Writer) *kittyStripWriter {
	return &kittyStripWriter{inner: w}
}

func (w *kittyStripWriter) Write(p []byte) (int, error) {
	original := len(p)
	cleaned := p
	for _, pat := range kittyPatterns {
		cleaned = bytes.ReplaceAll(cleaned, pat, nil)
	}
	if len(cleaned) == 0 {
		return original, nil
	}
	_, err := w.inner.Write(cleaned)
	if err != nil {
		return 0, err
	}
	// Return original length so callers don't think bytes were lost.
	return original, nil
}

// Read delegates to the underlying writer if it also implements io.Reader
// (which ssh.Session does).
func (w *kittyStripWriter) Read(p []byte) (int, error) {
	if r, ok := w.inner.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}
