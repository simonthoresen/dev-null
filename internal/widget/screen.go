package widget

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/internal/render"
	"null-space/internal/theme"
)

// Screen is a top-level borderless vertical layout that composes the
// framework chrome: menu bar (row 0), content window (fill), status bar (last row).
// It delegates focus management and cursor position to the content Window.
type Screen struct {
	MenuBar   *MenuBar
	Window    *Window
	StatusBar *StatusBar

	// Computed during render.
	screenX, screenY int
	width, height    int
	menuBarH         int
	windowY, windowH int
	statusBarY       int
}

// RenderToBuf draws all three sections vertically into buf at (x, y).
// Menu bar and status bar use secondary layer (depth 1); content window uses primary (depth 0).
func (s *Screen) RenderToBuf(buf *render.ImageBuffer, x, y, w, h int, t *theme.Theme) {
	s.screenX = x
	s.screenY = y
	s.width = w
	s.height = h

	// Fixed 1-row menu bar, fixed 1-row status bar, window gets the rest.
	s.menuBarH = 1
	statusBarH := 1
	s.windowH = h - s.menuBarH - statusBarH
	if s.windowH < 1 {
		s.windowH = 1
	}

	s.windowY = y + s.menuBarH
	s.statusBarY = s.windowY + s.windowH

	barLayer := t.LayerAt(1) // secondary: menu bar, status bar
	bodyLayer := t.LayerAt(0) // primary: content window

	s.MenuBar.Render(buf, x, y, w, s.menuBarH, false, barLayer)
	s.Window.RenderToBuf(buf, x, s.windowY, w, s.windowH, bodyLayer)
	s.StatusBar.Render(buf, x, s.statusBarY, w, statusBarH, false, barLayer)
}

// HandleUpdate delegates to the content Window.
func (s *Screen) HandleUpdate(msg tea.Msg) tea.Cmd {
	return s.Window.HandleUpdate(msg)
}

// CycleFocus delegates to the content Window.
func (s *Screen) CycleFocus() tea.Cmd {
	return s.Window.CycleFocus()
}

// CursorPosition delegates to the content Window.
func (s *Screen) CursorPosition() (cx, cy int, visible bool) {
	return s.Window.CursorPosition()
}

// CursorModel returns the textinput cursor model from the content Window's
// focused child, or nil if not applicable.
func (s *Screen) CursorModel() *textinput.Model {
	if s.Window.FocusIdx < 0 || s.Window.FocusIdx >= len(s.Window.Children) {
		return nil
	}
	switch ti := s.Window.Children[s.Window.FocusIdx].Control.(type) {
	case *TextInput:
		return ti.Model
	case *CommandInput:
		return ti.Model
	}
	return nil
}

// HandleClick routes a click. Returns true if consumed.
// Menu bar clicks should be handled by the caller via OverlayState.
func (s *Screen) HandleClick(mx, my int) bool {
	// Content window area.
	if my >= s.windowY && my < s.windowY+s.windowH {
		return s.Window.HandleClick(mx, my)
	}
	return false
}

// MenuBarRow returns the absolute screen row of the menu bar.
func (s *Screen) MenuBarRow() int {
	return s.screenY
}

// WindowY returns the absolute Y of the content window.
func (s *Screen) WindowY() int {
	return s.windowY
}

// WindowH returns the allocated height of the content window.
func (s *Screen) WindowH() int {
	return s.windowH
}
