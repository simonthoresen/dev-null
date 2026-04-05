package widget

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"null-space/internal/domain"
	"null-space/internal/render"
	"null-space/internal/theme"
)

// MenuBar is a Control that renders the NC-style menu action bar.
// It reads overlay state (MenuFocused, MenuCursor, OpenMenu) to determine
// which menu title to highlight. It does NOT handle keyboard input — that
// stays in OverlayState.HandleKey.
type MenuBar struct {
	Menus   []domain.MenuDef
	Overlay *OverlayState
}

func (m *MenuBar) Focusable() bool      { return false }
func (m *MenuBar) MinSize() (int, int)  { return 1, 1 }
func (m *MenuBar) Update(_ tea.Msg)     {}

// Render writes the menu bar directly into buf at (x, y) within (width × height).
func (m *MenuBar) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	if width <= 0 || height <= 0 {
		return
	}

	fg := layer.Fg
	bg := layer.Bg
	accentFg := layer.Accent
	hlFg := layer.HighlightFg
	hlBg := layer.HighlightBg

	// Fill the entire bar with the base background.
	buf.Fill(x, y, width, 1, ' ', fg, bg, render.AttrNone)

	col := x
	for i, menu := range m.Menus {
		if col >= x+width {
			break
		}

		// Separator between menus.
		if i > 0 {
			sep := layer.BarSep
			if sep != "" {
				buf.WriteString(col, y, sep, fg, bg, render.AttrNone)
				col++
			}
		}

		focused := (m.Overlay.MenuFocused || m.Overlay.OpenMenu >= 0) && i == m.Overlay.MenuCursor

		// Cursor marker / leading space: ► only when bar is focused with no
		// open dropdown AND the terminal is monochrome (color terminals use
		// background highlight alone to indicate focus).
		if col < x+width {
			barCursor := focused && m.Overlay.OpenMenu < 0 && layer.Monochrome
			if barCursor {
				buf.SetChar(col, y, '►', hlFg, hlBg, render.AttrNone)
			} else if focused {
				buf.SetChar(col, y, ' ', hlFg, hlBg, render.AttrNone)
			} else {
				buf.SetChar(col, y, ' ', fg, bg, render.AttrNone)
			}
			col++
		}

		// Menu label with shortcut underline.
		// Find the ampersand position in the raw label to know which char to underline.
		ampIdx := strings.IndexByte(menu.Label, '&')
		clean, _ := StripAmpersand(menu.Label)

		for ci, ch := range clean {
			if col >= x+width {
				break
			}
			// ci matches the position in clean; ampIdx is in original label.
			// In clean, the shortcut char is at position ampIdx (since '&' was removed before it).
			isShortcut := ampIdx >= 0 && ci == ampIdx
			if focused {
				if isShortcut {
					buf.SetChar(col, y, ch, accentFg, hlBg, render.AttrBold|render.AttrUnderline)
				} else {
					buf.SetChar(col, y, ch, hlFg, hlBg, render.AttrBold)
				}
			} else {
				if isShortcut {
					buf.SetChar(col, y, ch, accentFg, bg, render.AttrUnderline)
				} else {
					buf.SetChar(col, y, ch, fg, bg, render.AttrNone)
				}
			}
			col++
		}

		// Trailing space.
		if col < x+width {
			if focused {
				buf.SetChar(col, y, ' ', hlFg, hlBg, render.AttrNone)
			}
			col++
		}
	}
}
