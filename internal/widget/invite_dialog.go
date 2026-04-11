package widget

// BuildInviteWindow creates the invite dialog Window with two focusable logo
// panels (Windows on the left, SSH on the right) and a Close button.
//
// onCopy is called with the link string when a logo panel is activated (Enter/click).
// onClose is called to dismiss the dialog (pass overlay.PopDialog).
func BuildInviteWindow(
	winLink, sshLink string,
	onCopy func(string),
	onClose func(),
) *Window {
	winBtn := &LogoButton{
		RenderArt: RenderWindowsLogo,
		ArtW:      LogoArtWidth,
		ArtH:      LogoArtHeight,
		Caption:   "Windows",
		OnPress: func() {
			if onCopy != nil {
				onCopy(winLink)
			}
			if onClose != nil {
				onClose()
			}
		},
	}
	sshBtn := &LogoButton{
		RenderArt: RenderSSHLogo,
		ArtW:      LogoArtWidth,
		ArtH:      LogoArtHeight,
		Caption:   "SSH",
		OnPress: func() {
			if onCopy != nil {
				onCopy(sshLink)
			}
			if onClose != nil {
				onClose()
			}
		},
	}
	closeBtn := &Button{
		Label: "Close",
		OnPress: func() {
			if onClose != nil {
				onClose()
			}
		},
	}

	// Grid layout — 3 columns (pad | content | pad), same pattern as
	// buildDialogWindow. Rows: top-pad | logos | gap | buttons | bot-pad.
	children := []GridChild{
		// Padding spacers — establish 1-cell pad on all four sides.
		{
			Control:    &Label{Text: ""},
			Constraint: GridConstraint{Col: 0, Row: 0, MinW: dialogPad, MinH: dialogPad},
		},
		{
			Control:    &Label{Text: ""},
			Constraint: GridConstraint{Col: 2, Row: 4, MinW: dialogPad, MinH: dialogPad},
		},

		// Logo row: Win logo | gap | SSH logo.
		{
			Control: &Container{
				Horizontal: true,
				Children: []ContainerChild{
					{Control: winBtn, Weight: 1},
					{Control: &Label{Text: ""}, Fixed: 2},
					{Control: sshBtn, Weight: 1},
				},
			},
			TabIndex: 0,
			Constraint: GridConstraint{
				Col: 1, Row: 1, WeightX: 1, WeightY: 1, Fill: FillBoth,
			},
		},

		// Gap between logos and buttons.
		{
			Control:    &Label{Text: ""},
			Constraint: GridConstraint{Col: 1, Row: 2, MinH: 1, Fill: FillHorizontal},
		},

		// Button row: Close button centred via Label flex spacers.
		{
			Control: &Container{
				Horizontal: true,
				Children: []ContainerChild{
					{Control: &Label{Text: ""}, Weight: 1},
					{Control: closeBtn, Fixed: len("Close") + 6},
					{Control: &Label{Text: ""}, Weight: 1},
				},
			},
			TabIndex: 1,
			Constraint: GridConstraint{
				Col: 1, Row: 3, WeightX: 1, Fill: FillHorizontal,
			},
		},
	}

	win := &Window{Title: "Invite", Children: children}
	win.FocusFirst()
	return win
}
