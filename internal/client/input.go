package client

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Key repeat constants (in ticks at 60 TPS).
const (
	keyRepeatDelay = 30 // frames before repeat starts (~500ms)
	keyRepeatRate  = 3  // frames between repeats (~50ms)
)

// handleInput maps Ebitengine key events to SSH-compatible escape sequences.
func (r *ClientRenderer) handleInput() {
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt)
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Character input (typed text) — skip when Alt or Ctrl is held.
	if !alt && !ctrl {
		runes := ebiten.AppendInputChars(nil)
		for _, ch := range runes {
			r.conn.Write([]byte(string(ch)))
		}
	}

	// Special keys with key repeat and modifier support.
	for _, key := range specialKeys {
		dur := inpututil.KeyPressDuration(key.ekey)
		if dur == 1 || (dur >= keyRepeatDelay && (dur-keyRepeatDelay)%keyRepeatRate == 0) {
			seq := key.seq
			// Shift+Tab is the standard backtab sequence.
			if key.ekey == ebiten.KeyTab && shift {
				seq = "\x1b[Z"
			} else if (shift || ctrl || alt) && key.modSeq != "" {
				// Send modified escape sequences for Shift/Ctrl/Alt + special keys.
				// Format: CSI 1;modifier X (e.g. \x1b[1;2A for Shift+Up).
				mod := 1
				if shift {
					mod += 1
				}
				if alt {
					mod += 2
				}
				if ctrl {
					mod += 4
				}
				seq = fmt.Sprintf(key.modSeq, mod)
			}
			r.conn.Write([]byte(seq))
		}
	}

	// Alt+letter for menu shortcuts (e.g. Alt+F → ESC f).
	if alt && !ctrl {
		for key := ebiten.KeyA; key <= ebiten.KeyZ; key++ {
			if inpututil.IsKeyJustPressed(key) {
				letter := byte('a' + (key - ebiten.KeyA))
				r.conn.Write([]byte{0x1b, letter})
			}
		}
	}

	// Ctrl+letter combos (Ctrl+A = 0x01, Ctrl+B = 0x02, ..., Ctrl+Z = 0x1A).
	if ctrl && !alt {
		for key := ebiten.KeyA; key <= ebiten.KeyZ; key++ {
			if inpututil.IsKeyJustPressed(key) {
				r.conn.Write([]byte{byte(1 + (key - ebiten.KeyA))})
			}
		}
	}

	// Mouse events — send as SGR (mode 1006) escape sequences.
	r.handleMouseInput()
}

// handleMouseInput sends mouse click and scroll events as SGR escape sequences.
func (r *ClientRenderer) handleMouseInput() {
	cx, cy := ebiten.CursorPosition()
	cellX := cx / cellW()
	cellY := cy / cellH()

	// Left click.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dM", 0, cellX+1, cellY+1)))
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dm", 0, cellX+1, cellY+1)))
	}

	// Right click.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dM", 2, cellX+1, cellY+1)))
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonRight) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dm", 2, cellX+1, cellY+1)))
	}

	// Middle click.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonMiddle) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dM", 1, cellX+1, cellY+1)))
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonMiddle) {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dm", 1, cellX+1, cellY+1)))
	}

	// Scroll wheel.
	_, scrollY := ebiten.Wheel()
	if scrollY > 0 {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dM", 64, cellX+1, cellY+1)))
	} else if scrollY < 0 {
		r.conn.Write([]byte(fmt.Sprintf("\x1b[<%d;%d;%dM", 65, cellX+1, cellY+1)))
	}
}

type keyMapping struct {
	ekey   ebiten.Key
	seq    string // base escape sequence
	modSeq string // format string for modified sequence: fmt.Sprintf(modSeq, modifier)
}

var specialKeys = []keyMapping{
	{ebiten.KeyEnter, "\r", ""},
	{ebiten.KeyBackspace, "\x7f", ""},
	{ebiten.KeyTab, "\t", "\x1b[1;%dI"},       // Shift+Tab = \x1b[Z, but modifier format for others
	{ebiten.KeyEscape, "\x1b", ""},
	{ebiten.KeyUp, "\x1b[A", "\x1b[1;%dA"},
	{ebiten.KeyDown, "\x1b[B", "\x1b[1;%dB"},
	{ebiten.KeyRight, "\x1b[C", "\x1b[1;%dC"},
	{ebiten.KeyLeft, "\x1b[D", "\x1b[1;%dD"},
	{ebiten.KeyHome, "\x1b[H", "\x1b[1;%dH"},
	{ebiten.KeyEnd, "\x1b[F", "\x1b[1;%dF"},
	{ebiten.KeyPageUp, "\x1b[5~", "\x1b[5;%d~"},
	{ebiten.KeyPageDown, "\x1b[6~", "\x1b[6;%d~"},
	{ebiten.KeyDelete, "\x1b[3~", "\x1b[3;%d~"},
	{ebiten.KeyF1, "\x1bOP", "\x1b[1;%dP"},
	{ebiten.KeyF2, "\x1bOQ", "\x1b[1;%dQ"},
	{ebiten.KeyF3, "\x1bOR", "\x1b[1;%dR"},
	{ebiten.KeyF4, "\x1bOS", "\x1b[1;%dS"},
	{ebiten.KeyF5, "\x1b[15~", "\x1b[15;%d~"},
	{ebiten.KeyF6, "\x1b[17~", "\x1b[17;%d~"},
	{ebiten.KeyF7, "\x1b[18~", "\x1b[18;%d~"},
	{ebiten.KeyF8, "\x1b[19~", "\x1b[19;%d~"},
	{ebiten.KeyF9, "\x1b[20~", "\x1b[20;%d~"},
	{ebiten.KeyF10, "\x1b[21~", "\x1b[21;%d~"},
	{ebiten.KeyF11, "\x1b[23~", "\x1b[23;%d~"},
	{ebiten.KeyF12, "\x1b[24~", "\x1b[24;%d~"},
}
