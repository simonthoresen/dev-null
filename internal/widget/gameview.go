package widget

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// GameView wraps a game's Render() function as a Control. When focused,
// keys other than Tab/Shift+Tab are forwarded to the game via OnKey.
//
// Enter and Esc are framework-reserved and are handled by the input
// router before Update() is called, so GameView will never see them.
// The router's default (when the focused widget doesn't implement
// EnterConsumer/EscConsumer) is to focus the chat / activate the menu.
//
// When Inner is non-nil and has focusable children, GameView behaves as
// a focus container: keys delegate to the inner window's focused child,
// and Tab cycles internal focus. When the inner cycle wraps, GameView
// pops out by signalling WantTab/WantBackTab so the chrome's parent
// Window advances to the next sibling (the command input).
type GameView struct {
	RenderFn  func(buf *render.ImageBuffer, x, y, w, h int)
	OnKey     func(key string) // bound to game.OnInput(playerID, key)
	Inner     *Window          // reconciled game widget tree, if any
	focusable bool

	WantTab, WantBackTab bool
}

func (g *GameView) SetFocusable(v bool) { g.focusable = v }
func (g *GameView) Focusable() bool     { return g.focusable }
func (g *GameView) MinSize() (int, int) { return 1, 1 }
func (g *GameView) TabWant() (bool, bool) {
	fwd, back := g.WantTab, g.WantBackTab
	g.WantTab = false
	g.WantBackTab = false
	return fwd, back
}

// FocusedChild returns the innermost focused Control inside Inner,
// or nil if GameView has no inner focus hierarchy.
func (g *GameView) FocusedChild() Control {
	if g.Inner == nil {
		return nil
	}
	if g.Inner.FocusIdx < 0 || g.Inner.FocusIdx >= len(g.Inner.Children) {
		return nil
	}
	return g.Inner.Children[g.Inner.FocusIdx].Control
}

// OnFocusDir is called by the parent Window when focus enters via Tab
// (dir=+1) or Shift+Tab (dir=-1). We use it to initialize internal
// focus to the first (or last) focusable child of Inner.
func (g *GameView) OnFocusDir(dir int) {
	if g.Inner == nil {
		return
	}
	// Reset internal focus to the first focusable child when arriving
	// forward, or the last when arriving backward.
	g.Inner.FocusFirstOrLast(dir < 0)
}

func (g *GameView) Update(msg tea.Msg) {
	g.WantTab = false
	g.WantBackTab = false

	// When we have an inner widget tree with focusable content, delegate
	// keys to its focused child. Tab/Shift+Tab cycle internal focus; when
	// the cycle wraps, we pop out to the chrome's parent Window.
	if g.Inner != nil && g.Inner.HasFocusable() {
		if km, ok := msg.(tea.KeyPressMsg); ok {
			switch km.String() {
			case "tab":
				if g.Inner.AtLastFocusable() {
					g.WantTab = true
					return
				}
				g.Inner.CycleFocus()
				return
			case "shift+tab":
				if g.Inner.AtFirstFocusable() {
					g.WantBackTab = true
					return
				}
				g.Inner.CycleFocusBack()
				return
			}
		}
		g.Inner.HandleUpdate(msg)
		return
	}

	// No inner tree — GameView is a raw game viewport. Forward keys to the
	// game via OnKey (which calls game.OnInput).
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "tab":
			g.WantTab = true
		case "shift+tab":
			g.WantBackTab = true
		default:
			if g.OnKey != nil {
				key := km.String()
				if key == "space" {
					key = " "
				}
				g.OnKey(key)
			}
		}
	}
}

func (g *GameView) Render(buf *render.ImageBuffer, x, y, width, height int, _ bool, layer *theme.Layer) {
	if g.RenderFn == nil {
		fg := layer.Fg
		bg := layer.Bg
		buf.Fill(x, y, width, height, ' ', fg, bg, render.AttrNone)
		return
	}
	g.RenderFn(buf, x, y, width, height)
}
