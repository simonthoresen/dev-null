package widget

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
	"null-space/internal/theme"
)

// TeamPanel renders the lobby team list: unassigned players, teams with color
// swatches, and a [+ Create Team] button. It handles keyboard navigation
// (up/down to move between teams, left/right to cycle color, enter to rename).
type TeamPanel struct {
	// Data — set each frame by the lobby renderer before Render.
	Teams      []common.Team
	Unassigned []string // player IDs
	MyTeamIdx  int      // -1 = unassigned
	PlayerID   string
	GetPlayer  func(id string) *common.Player

	// Edit state — managed by chrome key handlers.
	Editing   bool
	EditValue string // current edit buffer (set by chrome)

	// Whether the [+ Create Team] button should show.
	ShowCreate bool

	WantTab     bool
	WantBackTab bool
}

func (tp *TeamPanel) Focusable() bool      { return true }
func (tp *TeamPanel) MinSize() (int, int)  { return 16, 3 }
func (tp *TeamPanel) TabWant() (bool, bool) { return tp.WantTab, tp.WantBackTab }

// HandleClick implements the Clickable interface for mouse support.
// The actual click handling is done by the chrome layer which inspects
// FocusIdx after Window.HandleClick returns.
func (tp *TeamPanel) HandleClick(rx, ry int) {}

func (tp *TeamPanel) Update(msg tea.Msg) {
	tp.WantTab = false
	tp.WantBackTab = false
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "tab", "esc":
			tp.WantTab = true
		case "shift+tab":
			tp.WantBackTab = true
		}
	}
}

func (tp *TeamPanel) Render(buf *common.ImageBuffer, x, y, width, height int, focused bool, layer *theme.Layer) {
	fg := layer.FgC()
	bg := layer.BgC()

	// Fill background.
	buf.Fill(x, y, width, height, ' ', fg, bg, common.AttrNone)

	row := 0

	// Unassigned header.
	if row < height {
		attr := common.PixelAttr(common.AttrNone)
		if focused && tp.MyTeamIdx < 0 {
			attr = common.AttrBold
		}
		// Gray color swatch.
		swatchColor := lipgloss.Color("#888888")
		buf.SetChar(x+1, y+row, ' ', fg, swatchColor, common.AttrNone)
		buf.SetChar(x+2, y+row, ' ', fg, swatchColor, common.AttrNone)
		buf.WriteString(x+4, y+row, "Unassigned", fg, bg, attr)
		row++
	}

	// Unassigned players.
	for _, pid := range tp.Unassigned {
		if row >= height {
			break
		}
		name := pid
		if tp.GetPlayer != nil {
			if p := tp.GetPlayer(pid); p != nil {
				name = p.Name
			}
		}
		buf.WriteString(x+4, y+row, TruncateStr(name, width-4), fg, bg, common.AttrNone)
		row++
	}

	// Teams.
	for i, team := range tp.Teams {
		if row >= height {
			break
		}
		// Blank separator.
		row++
		if row >= height {
			break
		}

		// Team header with color swatch.
		attr := common.PixelAttr(common.AttrNone)
		if focused && i == tp.MyTeamIdx {
			attr = common.AttrBold
		}
		teamColor := lipgloss.Color(team.Color)
		buf.SetChar(x+1, y+row, ' ', fg, teamColor, common.AttrNone)
		buf.SetChar(x+2, y+row, ' ', fg, teamColor, common.AttrNone)

		teamName := team.Name
		if tp.Editing && i == tp.MyTeamIdx {
			teamName = tp.EditValue
		}
		buf.WriteString(x+4, y+row, TruncateStr(teamName, width-4), fg, bg, attr)
		row++

		// Team members.
		for _, pid := range team.Players {
			if row >= height {
				break
			}
			name := pid
			if tp.GetPlayer != nil {
				if p := tp.GetPlayer(pid); p != nil {
					name = p.Name
				}
			}
			buf.WriteString(x+4, y+row, TruncateStr(name, width-4), fg, bg, common.AttrNone)
			row++
		}
	}

	// [+ Create Team] button.
	if tp.ShowCreate && row+1 < height {
		row++ // blank separator
		disabledFg := layer.DisabledFgC()
		buf.WriteString(x+1, y+row, TruncateStr("[+ Create Team]", width-1), disabledFg, bg, common.AttrNone)
	}
}

// TruncateStr truncates a plain string to maxW runes.
func TruncateStr(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	n := 0
	for i, r := range s {
		n++
		if n > maxW {
			return s[:i]
		}
		_ = r
	}
	return s
}
