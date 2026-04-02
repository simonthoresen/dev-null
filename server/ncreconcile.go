package server

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"

	"null-space/common"
)

// GameNCWindow wraps an NCWindow built from a WidgetNode tree.
// It lives on chromeModel (per-player) and is reconciled each frame
// so that interactive controls preserve their state (focus, cursor, scroll)
// across frames even though the WidgetNode tree is rebuilt from JS each time.
type GameNCWindow struct {
	Window   *NCWindow
	controls map[string]ncCachedControl // keyed by tree path for reuse
	renderFn func(buf *common.ImageBuffer, x, y, w, h int) // game's Render() function
	onInput  func(action string)        // bound to game.OnInput(playerID, ...)
}

// ncCachedControl pairs a control with metadata for reuse decisions.
type ncCachedControl struct {
	nodeType string
	control  NCControl
	hash     uint64 // WidgetNode.Hash() at build time; 0 = always rebuild
}

// HasFocusable returns true if the window contains any focusable controls.
func (gw *GameNCWindow) HasFocusable() bool {
	if gw == nil || gw.Window == nil {
		return false
	}
	for _, child := range gw.Window.Children {
		if child.Control.Focusable() {
			return true
		}
	}
	return false
}

// ReconcileGameWindow builds or updates a GameNCWindow from a WidgetNode tree.
// If prev is non-nil, interactive controls are reused by tree position to
// preserve state (focus, cursor, scroll offset).
func ReconcileGameWindow(
	prev *GameNCWindow,
	tree *common.WidgetNode,
	renderFn func(buf *common.ImageBuffer, x, y, w, h int),
	onInput func(action string),
) *GameNCWindow {
	gw := &GameNCWindow{
		controls: make(map[string]ncCachedControl),
		renderFn: renderFn,
		onInput:  onInput,
	}

	prevControls := map[string]ncCachedControl{}
	if prev != nil {
		prevControls = prev.controls
	}

	// Build the root control from the tree.
	root, children := gw.buildControl(tree, "0", prevControls)
	_ = root // root is the top-level control (might be a container, panel, or leaf)

	// Assemble into an NCWindow with a single child that fills everything.
	gw.Window = &NCWindow{
		Children: []GridChild{
			{
				Control:    root,
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth},
			},
		},
	}

	// Collect all focusable controls from the tree and add them as window
	// children so they participate in focus cycling. The root control handles
	// layout; focusable leaf controls need to be visible to the window's
	// focus management.
	if len(children) > 0 {
		// Replace the simple single-child approach: the window gets all
		// leaf focusable controls as direct children for focus routing,
		// while the root control handles visual layout.
		// For now, keep it simple: the root is the only child,
		// and focus cycling works if the root is an NCContainer whose
		// children are focusable.
		// TODO: deep focus cycling through nested containers
	}

	// Preserve focus index from previous window.
	if prev != nil && prev.Window != nil {
		gw.Window.FocusIdx = prev.Window.FocusIdx
	}

	return gw
}

// buildControl creates an NCControl from a WidgetNode, reusing prev controls
// where the type matches. Returns the control and a flat list of all focusable
// descendants (for focus management).
//
// Per-node caching: if the node's Hash() is non-zero (no interactive/gameview
// descendants) and matches the cached hash at the same path, the entire subtree
// is reused without rebuilding. This means only dynamic subtrees (containing
// gameview or interactive nodes) are rebuilt each frame.
func (gw *GameNCWindow) buildControl(node *common.WidgetNode, path string, prev map[string]ncCachedControl) (NCControl, []NCControl) {
	if node == nil {
		label := &NCLabel{Text: ""}
		return label, nil
	}

	// Fast path: if the node's hash is non-zero and matches the previous
	// frame at the same path, reuse the entire cached subtree.
	hash := node.Hash()
	if hash != 0 {
		if cached, ok := prev[path]; ok && cached.hash == hash {
			// Subtree unchanged — reuse control and propagate all cached
			// descendants (they were stored during the previous build).
			gw.controls[path] = cached
			gw.reuseCachedSubtree(path, prev)
			return cached.control, nil
		}
	}

	var ctrl NCControl
	var focusable []NCControl

	switch node.Type {
	case "label":
		ctrl = gw.buildLabel(node)
	case "panel":
		ctrl, focusable = gw.buildPanel(node, path, prev)
	case "divider":
		ctrl = &NCHDivider{Connected: false}
	case "table":
		ctrl = &NCTable{Rows: node.Rows}
	case "button":
		ctrl = gw.buildButton(node, path, prev)
	case "textinput":
		ctrl = gw.buildTextInput(node, path, prev)
	case "checkbox":
		ctrl = gw.buildCheckbox(node, path, prev)
	case "textview":
		ctrl = gw.buildTextView(node, path, prev)
	case "gameview":
		ctrl = gw.buildGameView(node)
	case "hsplit":
		ctrl, focusable = gw.buildContainer(node, path, true, prev)
	case "vsplit":
		ctrl, focusable = gw.buildContainer(node, path, false, prev)
	default:
		// Unknown type: treat as gameview fallback.
		ctrl = gw.buildGameView(node)
	}

	gw.controls[path] = ncCachedControl{nodeType: node.Type, control: ctrl, hash: hash}

	if ctrl.Focusable() {
		focusable = append(focusable, ctrl)
	}

	return ctrl, focusable
}

// reuseCachedSubtree copies all descendants of the given path from prev into
// the current controls map, so they survive to the next frame's cache check.
func (gw *GameNCWindow) reuseCachedSubtree(path string, prev map[string]ncCachedControl) {
	prefix := path + "."
	for k, v := range prev {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			gw.controls[k] = v
		}
	}
}

func (gw *GameNCWindow) buildLabel(node *common.WidgetNode) *NCLabel {
	return &NCLabel{Text: node.Text, Align: node.Align}
}

func (gw *GameNCWindow) buildPanel(node *common.WidgetNode, path string, prev map[string]ncCachedControl) (*NCPanel, []NCControl) {
	panel := &NCPanel{Title: node.Title}
	var allFocusable []NCControl

	for i, child := range node.Children {
		childPath := fmt.Sprintf("%s.%d", path, i)
		ctrl, focusable := gw.buildControl(child, childPath, prev)
		allFocusable = append(allFocusable, focusable...)

		constraint := GridConstraint{
			Col: 0, Row: i,
			WeightX: 1, Fill: FillHorizontal,
		}
		if child.Weight > 0 {
			constraint.WeightY = child.Weight
			constraint.Fill = FillBoth
		}
		panel.Children = append(panel.Children, GridChild{
			Control:    ctrl,
			Constraint: constraint,
			TabIndex:   child.TabIndex,
		})
	}
	return panel, allFocusable
}

func (gw *GameNCWindow) buildButton(node *common.WidgetNode, path string, prev map[string]ncCachedControl) *NCButton {
	// Reuse existing button if same type at same path.
	if cached, ok := prev[path]; ok && cached.nodeType == "button" {
		btn := cached.control.(*NCButton)
		btn.Label = node.Text
		return btn
	}
	action := node.Action
	return &NCButton{
		Label: node.Text,
		OnPress: func() {
			if gw.onInput != nil && action != "" {
				gw.onInput(action)
			}
		},
	}
}

func (gw *GameNCWindow) buildTextInput(node *common.WidgetNode, path string, prev map[string]ncCachedControl) *NCTextInput {
	// Reuse existing text input to preserve cursor/content state.
	if cached, ok := prev[path]; ok && cached.nodeType == "textinput" {
		ti := cached.control.(*NCTextInput)
		return ti
	}
	m := new(textinput.Model)
	*m = textinput.New()
	m.Prompt = ""
	m.Placeholder = ""
	m.CharLimit = 256
	if node.Value != "" {
		m.SetValue(node.Value)
	}
	action := node.Action
	ti := &NCTextInput{Model: m}
	ti.OnSubmit = func(text string) {
		if gw.onInput != nil && action != "" {
			gw.onInput(action + ":" + text)
		}
	}
	return ti
}

func (gw *GameNCWindow) buildCheckbox(node *common.WidgetNode, path string, prev map[string]ncCachedControl) *NCCheckbox {
	if cached, ok := prev[path]; ok && cached.nodeType == "checkbox" {
		cb := cached.control.(*NCCheckbox)
		cb.Label = node.Text
		// Update checked state from JS (game may have changed it).
		cb.Checked = node.Checked
		return cb
	}
	action := node.Action
	return &NCCheckbox{
		Label:   node.Text,
		Checked: node.Checked,
		OnToggle: func(checked bool) {
			if gw.onInput != nil && action != "" {
				if checked {
					gw.onInput(action + ":true")
				} else {
					gw.onInput(action + ":false")
				}
			}
		},
	}
}

func (gw *GameNCWindow) buildTextView(node *common.WidgetNode, path string, prev map[string]ncCachedControl) *NCTextView {
	// Reuse existing textview to preserve scroll position.
	if cached, ok := prev[path]; ok && cached.nodeType == "textview" {
		tv := cached.control.(*NCTextView)
		tv.Lines = node.Lines
		return tv
	}
	return &NCTextView{
		Lines:       node.Lines,
		BottomAlign: true,
		Scrollable:  true,
	}
}

func (gw *GameNCWindow) buildGameView(node *common.WidgetNode) *NCGameView {
	return &NCGameView{
		RenderFn:  gw.renderFn,
		OnKey:     gw.onInput,
		focusable: node.IsFocusable,
	}
}

func (gw *GameNCWindow) buildContainer(node *common.WidgetNode, path string, horizontal bool, prev map[string]ncCachedControl) (*NCContainer, []NCControl) {
	container := &NCContainer{Horizontal: horizontal}
	var allFocusable []NCControl

	for i, child := range node.Children {
		childPath := fmt.Sprintf("%s.%d", path, i)
		ctrl, focusable := gw.buildControl(child, childPath, prev)
		allFocusable = append(allFocusable, focusable...)

		container.Children = append(container.Children, ContainerChild{
			Control: ctrl,
			Weight:  child.Weight,
			Fixed: func() int {
				if horizontal {
					return child.Width
				}
				return child.Height
			}(),
		})
	}
	return container, allFocusable
}

// ─── NCLabel alignment support ───────────────────────────────────────────────

// The existing NCLabel only supports plain text. For viewNC labels with
// alignment, we check the Align field and render accordingly.
// This is handled by adding an Align field to NCLabel.

func init() {
	// Verify NCLabel has Align field at compile time.
	_ = NCLabel{}.Align
}

