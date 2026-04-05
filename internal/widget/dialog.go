package widget

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/internal/domain"
	"null-space/internal/render"
	"null-space/internal/theme"
)

// dialogEntry holds a materialized dialog: the original request plus
// the NC Window and control references needed to read state on close.
type dialogEntry struct {
	request domain.DialogRequest
	window  *Window
	listBox *ListBox    // nil when dialog has no list
	input   *TextInput  // nil when dialog has no input
	closed  bool        // set true by button OnPress
}

// dialogPad is the number of cells of padding between dialog border and content.
const dialogPad = 1

// buildDialogWindow constructs an NC Window from a DialogRequest.
// Content is placed in a 3-column grid (pad | content | pad) with spacer rows
// at top and bottom, giving 1-cell padding between the border and content
// without modifying the Window class.
func (o *OverlayState) buildDialogWindow(d domain.DialogRequest) *dialogEntry {
	entry := &dialogEntry{request: d}

	btns := d.Buttons
	if len(btns) == 0 {
		btns = []string{"OK"}
	}
	hasList := len(d.ListItems) > 0
	hasInput := d.InputPrompt != ""

	var children []GridChild
	tabIdx := 0
	row := 1 // row 0 is top padding

	// Top-left spacer: establishes padding column 0 and padding row 0.
	children = append(children, GridChild{
		Control:    &Label{Text: ""},
		Constraint: GridConstraint{Col: 0, Row: 0, MinW: dialogPad, MinH: dialogPad},
	})

	// --- Content area: body text OR list ---
	if hasList {
		lb := &ListBox{Items: d.ListItems, Tags: d.ListTags}
		if d.ListCursor > 0 && d.ListCursor < len(d.ListItems) {
			lb.Cursor = d.ListCursor
		}
		entry.listBox = lb
		children = append(children, GridChild{
			Control:  lb,
			TabIndex: tabIdx,
			Constraint: GridConstraint{
				Col: 1, Row: row, WeightX: 1, WeightY: 1, Fill: FillBoth,
			},
		})
		tabIdx++
	} else if d.Body != "" {
		bodyLines := strings.Split(d.Body, "\n")
		maxW := 0
		for _, line := range bodyLines {
			if w := len([]rune(line)); w > maxW {
				maxW = w
			}
		}
		tv := &TextView{Lines: bodyLines}
		children = append(children, GridChild{
			Control: tv,
			Constraint: GridConstraint{
				Col: 1, Row: row, WeightX: 1,
				MinW: maxW, MinH: len(bodyLines), Fill: FillBoth,
			},
		})
	}
	row++

	// Divider before input or buttons.
	children = append(children, GridChild{
		Control: &HDivider{},
		Constraint: GridConstraint{
			Col: 1, Row: row, MinH: 1, Fill: FillHorizontal,
		},
	})
	row++

	// --- Optional input field ---
	if hasInput {
		tiModel := new(textinput.Model)
		*tiModel = textinput.New()
		tiModel.Prompt = " " + d.InputPrompt + " "
		tiModel.Placeholder = ""
		tiModel.CharLimit = 256
		ti := &TextInput{Model: tiModel}
		entry.input = ti
		children = append(children, GridChild{
			Control:  ti,
			TabIndex: tabIdx,
			Constraint: GridConstraint{
				Col: 1, Row: row, WeightX: 1, Fill: FillHorizontal,
			},
		})
		tabIdx++
		row++

		children = append(children, GridChild{
			Control: &HDivider{},
			Constraint: GridConstraint{
				Col: 1, Row: row, MinH: 1, Fill: FillHorizontal,
			},
		})
		row++
	}

	// Build a set of button labels that require list navigation to be enabled.
	requireNav := make(map[string]bool, len(d.RequireListNavigation))
	for _, lbl := range d.RequireListNavigation {
		requireNav[lbl] = true
	}

	// --- Button row: horizontal Container ---
	var btnChildren []ContainerChild
	for _, label := range btns {
		label := label // capture for closure
		btn := &Button{
			Label: label,
			OnPress: func() {
				entry.closed = true
				o.fireDialogCloseEntry(entry, label)
			},
		}
		if requireNav[label] {
			btn.Disabled = func() bool {
				return entry.listBox == nil || !entry.listBox.Navigated
			}
		}
		btnChildren = append(btnChildren, ContainerChild{
			Control: btn,
			Fixed:   len(label) + 6, // "[ label ]" + gap
		})
	}
	btnContainer := &Container{Horizontal: true, Children: btnChildren}
	children = append(children, GridChild{
		Control:  btnContainer,
		TabIndex: tabIdx,
		Constraint: GridConstraint{
			Col: 1, Row: row, WeightX: 1, Fill: FillHorizontal,
		},
	})
	row++

	// Bottom-right spacer: establishes padding column 2 and padding bottom row.
	children = append(children, GridChild{
		Control:    &Label{Text: ""},
		Constraint: GridConstraint{Col: 2, Row: row, MinW: dialogPad, MinH: dialogPad},
	})

	// --- Assemble Window ---
	win := &Window{
		Title:    d.Title,
		Children: children,
	}
	// Focus the first focusable child (list, input, or buttons).
	win.FocusIdx = -1
	for i, c := range children {
		if c.Control.Focusable() {
			win.FocusIdx = i
			break
		}
	}

	entry.window = win
	return entry
}

// fireDialogCloseEntry fires the appropriate callback for a dialog and pops it.
func (o *OverlayState) fireDialogCloseEntry(entry *dialogEntry, button string) {
	d := entry.request
	listIdx := 0
	if entry.listBox != nil {
		listIdx = entry.listBox.Cursor
	}
	inputVal := ""
	if entry.input != nil {
		inputVal = entry.input.Value()
	}

	// Pop first, then fire callback (callback may push a new dialog).
	o.popDialogEntry()

	switch {
	case d.OnListAction != nil && len(d.ListItems) > 0:
		d.OnListAction(button, listIdx)
	case d.OnInputClose != nil && d.InputPrompt != "":
		d.OnInputClose(button, inputVal)
	case d.OnClose != nil:
		d.OnClose(button)
	}
}

// popDialogEntry removes the top dialog entry.
func (o *OverlayState) popDialogEntry() {
	if len(o.dialogs) > 0 {
		o.dialogs = o.dialogs[:len(o.dialogs)-1]
	}
}

// --- Dialog sizing ---

// DialogSize computes the width and height for the top dialog's Window.
func (o *OverlayState) DialogSize(screenW, screenH int) (int, int) {
	entry := o.topEntry()
	if entry == nil || entry.window == nil {
		return 0, 0
	}

	// Sum children's min sizes to determine the dialog's natural dimensions.
	minW := 22
	minH := 2 // top + bottom border
	for _, child := range entry.window.Children {
		cw, ch := child.Control.MinSize()
		if child.Constraint.MinW > cw {
			cw = child.Constraint.MinW
		}
		if child.Constraint.MinH > ch {
			ch = child.Constraint.MinH
		}
		if cw > minW {
			minW = cw
		}
		minH += ch
	}
	// Account for title width.
	if tw := len(entry.request.Title) + 2; tw > minW {
		minW = tw
	}

	w := minW + 2 + 2*dialogPad // + borders + padding columns
	h := minH
	if w > screenW-4 {
		w = screenW - 4
	}
	if h > screenH-4 {
		h = screenH - 4
	}
	return w, h
}

// --- Rendering ---

// RenderDialogBuf renders the top dialog into a sub-buffer at the given layer.
// Returns the sub-buffer and its screen position, or nil if no dialog.
func (o *OverlayState) RenderDialogBuf(screenW, screenH int, layer *theme.Layer) (*render.ImageBuffer, int, int) {
	entry := o.topEntry()
	if entry == nil || entry.window == nil {
		return nil, 0, 0
	}
	w, h := o.DialogSize(screenW, screenH)
	if w <= 0 || h <= 0 {
		return nil, 0, 0
	}
	col := max(0, (screenW-w)/2)
	row := max(2, (screenH-h)/2)

	buf := render.NewImageBuffer(w, h)
	entry.window.RenderToBuf(buf, 0, 0, w, h, layer)
	return buf, col, row
}

// --- Message routing ---

// HandleDialogMsg routes a tea.Msg to the top dialog's NC Window.
// Returns (consumed, cmd). Call this before HandleKey for dialogs.
func (o *OverlayState) HandleDialogMsg(msg tea.Msg) (bool, tea.Cmd) {
	entry := o.topEntry()
	if entry == nil || entry.window == nil {
		return false, nil
	}

	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "esc":
			o.fireDialogCloseEntry(entry, "")
			return true, nil
		case "enter":
			// Enter on focused listbox triggers OnListEnter without closing the dialog.
			if entry.listBox != nil && entry.request.OnListEnter != nil &&
				entry.window.FocusedControl() == entry.listBox {
				entry.request.OnListEnter(entry.listBox.Cursor)
				return true, nil
			}
		}
	}

	cmd := entry.window.HandleUpdate(msg)

	// If a button fired (set entry.closed), the dialog is already popped.
	return true, cmd
}

// HandleDialogClick routes a mouse click to the top dialog's NC Window.
func (o *OverlayState) HandleDialogClick(x, y, screenW, screenH int) bool {
	entry := o.topEntry()
	if entry == nil || entry.window == nil {
		return false
	}
	w, h := o.DialogSize(screenW, screenH)
	col := max(0, (screenW-w)/2)
	row := max(2, (screenH-h)/2)

	// Translate to window-local coordinates.
	lx := x - col
	ly := y - row

	if lx >= 0 && lx < w && ly >= 0 && ly < h {
		entry.window.HandleClick(lx, ly)
		return true
	}

	// Click outside dialog still consumed (modal).
	return true
}

// DialogLayer returns the theme layer index for the top dialog.
// Layer 0 = main window, layer 1 = first dialog, layer 2 = dialog-on-dialog, etc.
func (o *OverlayState) DialogLayer() int {
	return len(o.dialogs)
}

// topEntry returns the top dialog entry, or nil.
func (o *OverlayState) topEntry() *dialogEntry {
	if len(o.dialogs) == 0 {
		return nil
	}
	return o.dialogs[len(o.dialogs)-1]
}

// DialogIsWarning reports whether the top dialog has Warning set.
func (o *OverlayState) DialogIsWarning() bool {
	e := o.topEntry()
	return e != nil && e.request.Warning
}
