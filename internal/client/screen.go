package client

import (
	"time"

	"null-space/internal/render"
	"null-space/internal/theme"
	"null-space/internal/widget"
)

// ClientScreen builds and renders the full NC-style UI locally on the client.
// It uses the same widget primitives as the server chrome but is fed by
// server-provided data (game state, chat lines, status text) instead of
// reading CentralState directly.
type ClientScreen struct {
	screen    *widget.Screen
	menuBar   *widget.MenuBar
	statusBar *widget.StatusBar
	window    *widget.Window
	gameView  *widget.GameView
	chatView  *widget.TextView

	theme *theme.Theme
}

// NewClientScreen creates the playing-mode UI layout (mirrors chrome/model.go).
func NewClientScreen(t *theme.Theme) *ClientScreen {
	gameView := &widget.GameView{}
	gameView.SetFocusable(true)

	chatView := &widget.TextView{BottomAlign: true, Scrollable: true}

	menuBar := &widget.MenuBar{}
	statusBar := &widget.StatusBar{}

	win := &widget.Window{
		FocusIdx: 0,
		Children: []widget.GridChild{
			{Control: gameView, TabIndex: 0, Constraint: widget.GridConstraint{
				Col: 0, Row: 0, WeightX: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 1, MinH: 1, Fill: widget.FillHorizontal,
			}},
			{Control: chatView, TabIndex: 1, Constraint: widget.GridConstraint{
				Col: 0, Row: 2, WeightX: 1, WeightY: 1, Fill: widget.FillBoth,
			}},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{
				Col: 0, Row: 3, MinH: 1, Fill: widget.FillHorizontal,
			}},
		},
	}

	screen := &widget.Screen{
		MenuBar:   menuBar,
		Window:    win,
		StatusBar: statusBar,
	}

	return &ClientScreen{
		screen:    screen,
		menuBar:   menuBar,
		statusBar: statusBar,
		window:    win,
		gameView:  gameView,
		chatView:  chatView,
		theme:     t,
	}
}

// RenderPlaying renders the full playing-mode screen to an ImageBuffer.
// renderFn is called to fill the game viewport.
func (cs *ClientScreen) RenderPlaying(
	width, height int,
	chatLines []string,
	statusLeft string,
	renderFn func(buf *render.ImageBuffer, x, y, w, h int),
) *render.ImageBuffer {
	// Compute game viewport height (same 16:9 logic as server chrome).
	interiorH := height - 4
	gameH := width * 9 / 16
	chatH := interiorH - 3 - gameH
	minChatH := 5
	if interiorH/3 > minChatH {
		minChatH = interiorH / 3
	}
	if chatH < minChatH {
		chatH = minChatH
		gameH = interiorH - 3 - chatH
	}
	if gameH < 1 {
		gameH = 1
	}

	cs.window.Children[0].Constraint.MinH = gameH

	cs.gameView.RenderFn = renderFn
	cs.chatView.Lines = chatLines
	cs.statusBar.LeftText = " " + statusLeft
	cs.statusBar.RightText = time.Now().Format("2006-01-02 15:04:05") + " "

	buf := render.NewImageBuffer(width, height)
	cs.screen.RenderToBuf(buf, 0, 0, width, height, cs.theme)
	return buf
}
