package client

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// handleInput maps Ebitengine key events to SSH-compatible escape sequences.
func (g *Game) handleInput() {
	// Handle character input (typed text).
	runes := ebiten.AppendInputChars(nil)
	for _, r := range runes {
		g.conn.Write([]byte(string(r)))
	}

	// Handle special keys.
	for _, key := range specialKeys {
		if inpututil.IsKeyJustPressed(key.ekey) {
			g.conn.Write([]byte(key.seq))
		}
	}
}

type keyMapping struct {
	ekey ebiten.Key
	seq  string
}

var specialKeys = []keyMapping{
	{ebiten.KeyEnter, "\r"},
	{ebiten.KeyBackspace, "\x7f"},
	{ebiten.KeyTab, "\t"},
	{ebiten.KeyEscape, "\x1b"},
	{ebiten.KeyUp, "\x1b[A"},
	{ebiten.KeyDown, "\x1b[B"},
	{ebiten.KeyRight, "\x1b[C"},
	{ebiten.KeyLeft, "\x1b[D"},
	{ebiten.KeyHome, "\x1b[H"},
	{ebiten.KeyEnd, "\x1b[F"},
	{ebiten.KeyPageUp, "\x1b[5~"},
	{ebiten.KeyPageDown, "\x1b[6~"},
	{ebiten.KeyDelete, "\x1b[3~"},
	{ebiten.KeyF1, "\x1bOP"},
	{ebiten.KeyF2, "\x1bOQ"},
	{ebiten.KeyF3, "\x1bOR"},
	{ebiten.KeyF4, "\x1bOS"},
	{ebiten.KeyF5, "\x1b[15~"},
	{ebiten.KeyF6, "\x1b[17~"},
	{ebiten.KeyF7, "\x1b[18~"},
	{ebiten.KeyF8, "\x1b[19~"},
	{ebiten.KeyF9, "\x1b[20~"},
	{ebiten.KeyF10, "\x1b[21~"},
	{ebiten.KeyF11, "\x1b[23~"},
	{ebiten.KeyF12, "\x1b[24~"},
}
