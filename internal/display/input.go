package display

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	tea "charm.land/bubbletea/v2"
)

// specialKeyMapping maps an Ebitengine key to a Bubble Tea key Code constant.
type specialKeyMapping struct {
	eKey    ebiten.Key
	teaCode rune
}

// specialKeys maps Ebitengine special keys to their tea.Key* constants.
var specialKeys = []specialKeyMapping{
	{ebiten.KeyEnter, tea.KeyEnter},
	{ebiten.KeyBackspace, tea.KeyBackspace},
	{ebiten.KeyTab, tea.KeyTab},
	{ebiten.KeyEscape, tea.KeyEscape},
	{ebiten.KeyUp, tea.KeyUp},
	{ebiten.KeyDown, tea.KeyDown},
	{ebiten.KeyRight, tea.KeyRight},
	{ebiten.KeyLeft, tea.KeyLeft},
	{ebiten.KeyHome, tea.KeyHome},
	{ebiten.KeyEnd, tea.KeyEnd},
	{ebiten.KeyPageUp, tea.KeyPgUp},
	{ebiten.KeyPageDown, tea.KeyPgDown},
	{ebiten.KeyDelete, tea.KeyDelete},
	{ebiten.KeyF1, tea.KeyF1},
	{ebiten.KeyF2, tea.KeyF2},
	{ebiten.KeyF3, tea.KeyF3},
	{ebiten.KeyF4, tea.KeyF4},
	{ebiten.KeyF5, tea.KeyF5},
	{ebiten.KeyF6, tea.KeyF6},
	{ebiten.KeyF7, tea.KeyF7},
	{ebiten.KeyF8, tea.KeyF8},
	{ebiten.KeyF9, tea.KeyF9},
	{ebiten.KeyF10, tea.KeyF10},
	{ebiten.KeyF11, tea.KeyF11},
	{ebiten.KeyF12, tea.KeyF12},
}

// Key repeat constants (in ticks at 60 TPS).
const (
	repeatDelay  = 30 // frames before repeat starts (~500ms)
	repeatRate   = 3  // frames between repeats (~50ms)
)

// PollKeyMessages polls Ebitengine for newly pressed keys and returns them
// as Bubble Tea messages. Handles key repeat for special keys.
func PollKeyMessages() []tea.Msg {
	var msgs []tea.Msg

	// Check modifier state.
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Character input (typed text) — Ebitengine filters to printable runes.
	// Note: Ebitengine handles key repeat for character input automatically.
	// Skip when Alt or Ctrl is held — those combos are handled separately below.
	runes := ebiten.AppendInputChars(nil)
	if !alt && !ctrl {
		for _, r := range runes {
			msgs = append(msgs, tea.KeyPressMsg{
				Code: rune(r),
				Text: string(r),
			})
		}
	}

	// Special keys with key repeat support.
	for _, km := range specialKeys {
		dur := inpututil.KeyPressDuration(km.eKey)
		if dur == 1 || (dur >= repeatDelay && (dur-repeatDelay)%repeatRate == 0) {
			msgs = append(msgs, tea.KeyPressMsg{
				Code: km.teaCode,
				Mod:  buildMod(ctrl, alt, shift),
			})
		}
	}

	// Alt+letter for menu shortcuts (e.g. Alt+F for File menu).
	if alt && !ctrl {
		for key := ebiten.KeyA; key <= ebiten.KeyZ; key++ {
			if inpututil.IsKeyJustPressed(key) {
				letter := rune('a' + (key - ebiten.KeyA))
				msgs = append(msgs, tea.KeyPressMsg{
					Code: letter,
					Mod:  tea.ModAlt,
				})
			}
		}
	}

	// Ctrl+letter combos.
	if ctrl && !alt {
		for key := ebiten.KeyA; key <= ebiten.KeyZ; key++ {
			if inpututil.IsKeyJustPressed(key) {
				letter := rune('a' + (key - ebiten.KeyA))
				msgs = append(msgs, tea.KeyPressMsg{
					Code: letter,
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
