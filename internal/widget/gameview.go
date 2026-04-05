package widget

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// GameView wraps a game's Render() function as a Control. When focused,
// non-Tab keys are forwarded to the game via OnKey. Enter triggers a
// focus cycle (to move to the command input for chat).
type GameView struct {
	RenderFn             func(buf *render.ImageBuffer, x, y, w, h int)
	OnKey                func(key string) // bound to game.OnInput(playerID, key)
	focusable            bool
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

func (g *GameView) Update(msg tea.Msg) {
	g.WantTab = false
	g.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter":
			// Enter moves focus to the command input for chat.
			g.WantTab = true
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
