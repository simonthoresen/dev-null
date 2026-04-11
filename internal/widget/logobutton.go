package widget

import (
	tea "charm.land/bubbletea/v2"

	"dev-null/internal/render"
	"dev-null/internal/theme"
)

// LogoButton is a focusable multi-line button that displays art and a caption
// label. When focused its border is drawn in the layer's highlight colours;
// Enter/Space trigger OnPress; Tab/Shift+Tab cycle focus.
//
// Art is supplied via one of two mutually exclusive fields:
//   - Lines: plain ASCII-art text lines (trailing whitespace pre-trimmed).
//   - RenderArt: a callback that writes coloured block-art cells directly into
//     the buffer. When set, ArtW/ArtH must give the art's cell dimensions.
type LogoButton struct {
	// Text-art path (use when no per-cell colour is needed).
	Lines []string // figlet art lines (trailing whitespace already trimmed)

	// Coloured block-art path (takes priority over Lines when non-nil).
	RenderArt func(buf *render.ImageBuffer, bx, by int)
	ArtW      int // art width in cells
	ArtH      int // art height in cells

	Caption string // label shown below the art
	OnPress func()

	WantTab     bool
	WantBackTab bool
}

func (b *LogoButton) Focusable() bool { return true }

// MinSize returns the minimum (width, height) the button needs.
// Width:  widest content line + 2 borders + 2 padding cells.
// Height: art lines + caption row + 2 borders.
func (b *LogoButton) MinSize() (int, int) {
	if b.RenderArt != nil {
		return max(b.ArtW, len([]rune(b.Caption))) + 4, b.ArtH + 3
	}
	maxW := len([]rune(b.Caption))
	for _, l := range b.Lines {
		if w := len([]rune(l)); w > maxW {
			maxW = w
		}
	}
	return maxW + 4, len(b.Lines) + 3
}

func (b *LogoButton) TabWant() (bool, bool) { return b.WantTab, b.WantBackTab }

func (b *LogoButton) Update(msg tea.Msg) {
	b.WantTab = false
	b.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "enter", " ":
			if b.OnPress != nil {
				b.OnPress()
			}
		case "tab":
			b.WantTab = true
		case "shift+tab":
			b.WantBackTab = true
		}
	}
}

func (b *LogoButton) HandleClick(_, _ int) {
	if b.OnPress != nil {
		b.OnPress()
	}
}

func (b *LogoButton) Render(buf *render.ImageBuffer, x, y, w, h int, focused bool, layer *theme.Layer) {
	fg := layer.Fg
	bg := layer.Bg
	attr := render.AttrNone
	if focused {
		fg = layer.HighlightFg
		bg = layer.HighlightBg
		attr = render.AttrBold
	}

	// Fill entire area with the appropriate background.
	buf.Fill(x, y, w, h, ' ', fg, bg, render.AttrNone)

	if w < 3 || h < 3 {
		return
	}

	// Draw thin single-line border.
	buf.SetChar(x, y, '┌', fg, bg, attr)
	buf.SetChar(x+w-1, y, '┐', fg, bg, attr)
	buf.SetChar(x, y+h-1, '└', fg, bg, attr)
	buf.SetChar(x+w-1, y+h-1, '┘', fg, bg, attr)
	for col := x + 1; col < x+w-1; col++ {
		buf.SetChar(col, y, '─', fg, bg, attr)
		buf.SetChar(col, y+h-1, '─', fg, bg, attr)
	}
	for row := y + 1; row < y+h-1; row++ {
		buf.SetChar(x, row, '│', fg, bg, attr)
		buf.SetChar(x+w-1, row, '│', fg, bg, attr)
	}

	if w < 5 || h < 4 {
		return
	}

	// Interior: 1-cell border + 1-cell padding on each horizontal side.
	innerW := w - 4 // usable content columns

	// Vertical layout:
	//   rows y+1 .. y+h-2  = interior (h-2 rows)
	//   last interior row  = caption
	//   rows before that   = art, vertically centred in the remaining space
	interiorH := h - 2
	captionRow := y + h - 2
	artAreaH := max(interiorH-1, 0) // rows available for art

	if b.RenderArt != nil {
		// Coloured block-art path: call RenderArt centred in the interior.
		artW := b.ArtW
		artH := b.ArtH
		artStartX := x + 2 + max(0, (innerW-artW)/2)
		artStartY := y + 1 + max(0, (artAreaH-artH)/2)
		b.RenderArt(buf, artStartX, artStartY)
	} else {
		artRows := len(b.Lines)
		artStart := max((artAreaH-artRows)/2, 0)

		for i, line := range b.Lines {
			row := y + 1 + artStart + i
			if row >= captionRow {
				break
			}
			runes := []rune(line)
			lw := len(runes)
			if lw > innerW {
				runes = runes[:innerW]
				lw = innerW
			}
			startCol := x + 2 + (innerW-lw)/2
			buf.WriteString(startCol, row, string(runes), fg, bg, render.AttrNone)
		}
	}

	// Caption centred on the last interior row.
	if b.Caption != "" {
		runes := []rune(b.Caption)
		lw := len(runes)
		if lw > innerW {
			runes = runes[:innerW]
			lw = innerW
		}
		startCol := x + 2 + (innerW-lw)/2
		buf.WriteString(startCol, captionRow, string(runes), fg, bg, attr)
	}
}
