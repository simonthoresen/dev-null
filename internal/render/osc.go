package render

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// OSC escape sequences for the dev-null enhanced client protocol.
// Regular terminals silently ignore unknown OSC sequences.
//
// Format: \x1b]ns;<type>;<payload>\x07
//
// Types:
//   viewport — game viewport bounds: x,y,w,h
//
// Error handling: These functions return "" on error intentionally.
// They are called exclusively from View()/Render() paths where errors
// cannot be propagated (tea.View has no error return) and logging is
// forbidden (slog in render paths causes a feedback loop — see CLAUDE.md).
// A missing OSC sequence degrades gracefully: the enhanced client falls
// back to text-mode rendering.

// EncodeViewportOSC returns an OSC sequence with the game viewport bounds.
func EncodeViewportOSC(x, y, w, h int) string {
	return fmt.Sprintf("\x1b]ns;viewport;%d,%d,%d,%d\x07", x, y, w, h)
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
// data must be the raw JSON bytes of the state object.
//
// This is the full-baseline path; the client replaces Game.state wholesale.
// For incremental updates prefer EncodeStatePatchOSC.
func EncodeStateOSC(data []byte) string {
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

// EncodeStatePatchOSC returns an OSC sequence containing a depth-1 JSON merge
// patch against Game.state. Keys present in data replace the corresponding
// key on the client; keys with a JSON null are deleted; keys not in data are
// left untouched.
//
// The client must have received an ns;state baseline before this patch is
// applied — the framework's state-broadcast path always sends the baseline
// first after (re)connection, mode switch, or game load.
func EncodeStatePatchOSC(data []byte) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;state-patch;" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeModeOSC returns an OSC sequence to switch the client rendering mode.
func EncodeModeOSC(mode string) string {
	return "\x1b]ns;mode;" + mode + "\x07"
}

// EncodePlayerIDOSC returns an OSC sequence telling the client which ID the
// server assigned to this session. Games keyed by player ID (e.g. most
// multiplayer games) need this to look up their own entry in Game.state.
func EncodePlayerIDOSC(pid string) string {
	return "\x1b]ns;playerid;" + pid + "\x07"
}

// EncodeAssetManifestOSC returns an OSC sequence announcing the total number of
// game asset files about to be sent. The client uses this for loading progress.
func EncodeAssetManifestOSC(count int) string {
	return fmt.Sprintf("\x1b]ns;asset-manifest;%d\x07", count)
}

// EncodeAssetOSC returns an OSC sequence containing a single game asset file
// (gzipped and base64-encoded). name must be a bare filename with no path separators.
func EncodeAssetOSC(name string, data []byte) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return ""
	}
	if err := gz.Close(); err != nil {
		return ""
	}
	return "\x1b]ns;asset;" + name + ";" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "\x07"
}

// EncodeSoundOSC returns an OSC sequence instructing graphical clients to play a sound.
func EncodeSoundOSC(filename string, loop bool) string {
	loopVal := "0"
	if loop {
		loopVal = "1"
	}
	return "\x1b]ns;sound;" + filename + ";loop=" + loopVal + "\x07"
}

// EncodeStopSoundOSC returns an OSC sequence instructing graphical clients to stop a sound.
// An empty filename means stop all currently playing sounds.
func EncodeStopSoundOSC(filename string) string {
	return "\x1b]ns;stop-sound;" + filename + "\x07"
}

// EncodeMidiOSC returns an OSC sequence containing MIDI events (base64-encoded JSON).
// The events parameter must be JSON-marshalable (typically []domain.MidiEvent).
func EncodeMidiOSC(events any) string {
	data, err := json.Marshal(events)
	if err != nil {
		return ""
	}
	return "\x1b]ns;midi;" + base64.StdEncoding.EncodeToString(data) + "\x07"
}

// EncodeSynthOSC returns an OSC sequence telling the client which SoundFont to use.
func EncodeSynthOSC(name string) string {
	return "\x1b]ns;synth;" + name + "\x07"
}

// EncodeOSC52 returns a standard OSC 52 escape sequence that sets the system
// clipboard. Supported by Windows Terminal, iTerm2, kitty, and most modern
// terminal emulators. Terminals that don't support it silently ignore it.
func EncodeOSC52(text string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\x07"
}
