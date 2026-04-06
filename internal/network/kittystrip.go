package network

import (
	"bytes"
	"io"
)

// KittyStripWriter wraps an io.Writer and strips Kitty keyboard protocol
// escape sequences from the output stream, and maps bare \n to \r\n (ONLCR).
//
// Kitty stripping: Bubble Tea v2's renderer always enables
// KittyDisambiguateEscapeCodes, which causes the client terminal to send
// multi-byte CSI u-encoded keystrokes. Over SSH, this breaks input parsing —
// the server's Bubble Tea input reader misinterprets or drops alternating keys.
// Stripping the enable/disable sequences keeps the client in legacy mode.
//
// ONLCR mapping: Bubble Tea v2 detects that the SSH session input is not a
// real TTY (ssh.Session has no Fd()), so it enables mapNl mode and emits bare
// \n to move down a line, expecting the PTY to supply the \r (ONLCR). Wish's
// emulated PTY does not implement ONLCR — it passes \n through unchanged. The
// cursor moves down but never returns to column 0, so each row's characters
// land shifted right, accumulating as rendering artifacts. We implement ONLCR
// here: any \n not already preceded by \r gets a \r prepended.
type KittyStripWriter struct {
	inner    io.Writer
	lastByte byte // last byte written, for cross-call \r\n detection
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

// NewKittyStripWriter creates a new writer that strips Kitty protocol sequences
// and maps bare \n to \r\n.
func NewKittyStripWriter(w io.Writer) *KittyStripWriter {
	return &KittyStripWriter{inner: w}
}

func (w *KittyStripWriter) Write(p []byte) (int, error) {
	original := len(p)
	cleaned := p
	for _, pat := range kittyPatterns {
		cleaned = bytes.ReplaceAll(cleaned, pat, nil)
	}

	// ONLCR: map bare \n → \r\n, skipping \n already preceded by \r.
	cleaned = w.applyONLCR(cleaned)

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

// applyONLCR replaces bare \n (not preceded by \r) with \r\n.
// Uses w.lastByte to correctly handle \r\n pairs split across Write calls.
func (w *KittyStripWriter) applyONLCR(p []byte) []byte {
	// Snapshot pre-call lastByte; used for both count and build passes.
	priorLast := w.lastByte

	// Count bare \n occurrences.
	prev := priorLast
	count := 0
	for _, b := range p {
		if b == '\n' && prev != '\r' {
			count++
		}
		prev = b
	}

	// Persist lastByte for the next Write call.
	if len(p) > 0 {
		w.lastByte = p[len(p)-1]
	}

	if count == 0 {
		return p
	}

	// Build expanded slice, inserting \r before bare \n.
	out := make([]byte, 0, len(p)+count)
	prev = priorLast
	for _, b := range p {
		if b == '\n' && prev != '\r' {
			out = append(out, '\r')
		}
		out = append(out, b)
		prev = b
	}
	return out
}

// Read delegates to the underlying writer if it also implements io.Reader
// (which ssh.Session does).
func (w *KittyStripWriter) Read(p []byte) (int, error) {
	if r, ok := w.inner.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}
