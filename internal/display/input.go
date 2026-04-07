package display

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	tea "charm.land/bubbletea/v2"
)

// KeyMapping maps an Ebitengine key to a Bubble Tea key string.
type KeyMapping struct {
	EKey ebiten.Key
	Tea  string
}

// SpecialKeys is the table of non-character keys that map to terminal escape
// sequences or Bubble Tea key names.
var SpecialKeys = []KeyMapping{
	{ebiten.KeyEnter, "enter"},
	{ebiten.KeyBackspace, "backspace"},
	{ebiten.KeyTab, "tab"},
	{ebiten.KeyEscape, "escape"},
	{ebiten.KeyUp, "up"},
	{ebiten.KeyDown, "down"},
	{ebiten.KeyRight, "right"},
	{ebiten.KeyLeft, "left"},
	{ebiten.KeyHome, "home"},
	{ebiten.KeyEnd, "end"},
	{ebiten.KeyPageUp, "pgup"},
	{ebiten.KeyPageDown, "pgdown"},
	{ebiten.KeyDelete, "delete"},
	{ebiten.KeyF1, "f1"},
	{ebiten.KeyF2, "f2"},
	{ebiten.KeyF3, "f3"},
	{ebiten.KeyF4, "f4"},
	{ebiten.KeyF5, "f5"},
	{ebiten.KeyF6, "f6"},
	{ebiten.KeyF7, "f7"},
	{ebiten.KeyF8, "f8"},
	{ebiten.KeyF9, "f9"},
	{ebiten.KeyF10, "f10"},
	{ebiten.KeyF11, "f11"},
	{ebiten.KeyF12, "f12"},
}

// PollKeyMessages polls Ebitengine for newly pressed keys and returns them
// as Bubble Tea messages. Character input (typed text) is returned as
// tea.KeyPressMsg with the rune set. Special keys are returned with their
// Bubble Tea key name.
func PollKeyMessages() []tea.Msg {
	var msgs []tea.Msg

	// Check modifier state.
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Character input (typed text) — Ebitengine filters to printable runes.
	runes := ebiten.AppendInputChars(nil)
	for _, r := range runes {
		// If Ctrl is held and we get a rune, Ebitengine may or may not filter it.
		// Most Ctrl+letter combos don't produce runes, but if they do, we handle them.
		msgs = append(msgs, tea.KeyPressMsg{
			Code: rune(r),
			Text: string(r),
			Mod:  buildMod(false, alt, shift), // ctrl already consumed by the rune
		})
	}

	// Special keys — only fire on just-pressed (not held).
	for _, km := range SpecialKeys {
		if inpututil.IsKeyJustPressed(km.EKey) {
			msgs = append(msgs, tea.KeyPressMsg{
				Code: runeForKey(km.Tea),
				Text: km.Tea,
				Mod:  buildMod(ctrl, alt, shift),
			})
		}
	}

	// Ctrl+letter combos that don't produce runes.
	if ctrl && len(runes) == 0 {
		for key := ebiten.KeyA; key <= ebiten.KeyZ; key++ {
			if inpututil.IsKeyJustPressed(key) {
				letter := rune('a' + (key - ebiten.KeyA))
				name := "ctrl+" + string(letter)
				msgs = append(msgs, tea.KeyPressMsg{
					Code: letter,
					Text: name,
					Mod:  tea.ModCtrl,
				})
			}
		}
	}

	return msgs
}

// PollMouseMessages polls Ebitengine for mouse events and returns them
// as Bubble Tea messages. Coordinates are in cell units (pixel / CellW,H).
func PollMouseMessages() []tea.Msg {
	var msgs []tea.Msg

	cx, cy := ebiten.CursorPosition()
	cellX := cx / CellW
	cellY := cy / CellH

	// Click events.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		msgs = append(msgs, tea.MouseClickMsg{
			X:      cellX,
			Y:      cellY,
			Button: tea.MouseLeft,
		})
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
		msgs = append(msgs, tea.MouseClickMsg{
			X:      cellX,
			Y:      cellY,
			Button: tea.MouseRight,
		})
	}

	// Scroll events.
	_, scrollY := ebiten.Wheel()
	if scrollY > 0 {
		msgs = append(msgs, tea.MouseWheelMsg{
			X:      cellX,
			Y:      cellY,
			Button: tea.MouseWheelUp,
		})
	} else if scrollY < 0 {
		msgs = append(msgs, tea.MouseWheelMsg{
			X:      cellX,
			Y:      cellY,
			Button: tea.MouseWheelDown,
		})
	}

	return msgs
}

func buildMod(ctrl, alt, shift bool) tea.KeyMod {
	var mod tea.KeyMod
	if ctrl {
		mod |= tea.ModCtrl
	}
	if alt {
		mod |= tea.ModAlt
	}
	if shift {
		mod |= tea.ModShift
	}
	return mod
}

func runeForKey(name string) rune {
	switch name {
	case "enter":
		return '\r'
	case "backspace":
		return 0x7f
	case "tab":
		return '\t'
	case "escape":
		return 0x1b
	case "delete":
		return 0x7f
	default:
		return 0
	}
}
