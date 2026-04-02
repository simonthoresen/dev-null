package widget

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"

	"null-space/common"
)

// GameWindow wraps a Window built from a WidgetNode tree.
// It lives on chromeModel (per-player) and is reconciled each frame
// so that interactive controls preserve their state (focus, cursor, scroll)
// across frames even though the WidgetNode tree is rebuilt from JS each time.
type GameWindow struct {
	Window   *Window
	Controls map[string]CachedControl // keyed by tree path for reuse
	renderFn func(buf *common.ImageBuffer, x, y, w, h int) // game's Render() function
	onInput  func(action string)        // bound to game.OnInput(playerID, ...)
	rootHash uint64                     // hash of the root WidgetNode at build time
}

// CachedControl pairs a control with metadata for reuse decisions.
type CachedControl struct {
	NodeType   string
	Control    Control
	Hash       uint64   // WidgetNode.Hash() at build time; 0 = always rebuild
	ChildPaths []string // paths of all descendants (for efficient subtree reuse)
}

// HasFocusable returns true if the window contains any focusable controls.
func (gw *GameWindow) HasFocusable() bool {
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

// ReconcileGameWindow builds or updates a GameWindow from a WidgetNode tree.
// If prev is non-nil, interactive controls are reused by tree position to
// preserve state (focus, cursor, scroll offset).
func ReconcileGameWindow(
	prev *GameWindow,
	tree *common.WidgetNode,
	renderFn func(buf *common.ImageBuffer, x, y, w, h int),
	onInput func(action string),
) *GameWindow {
	// Fast path: if the entire tree is unchanged (root hash matches and is
	// non-zero), reuse the previous GameWindow — no allocations needed.
	rootHash := tree.Hash()
	if prev != nil && rootHash != 0 && rootHash == prev.rootHash {
		prev.renderFn = renderFn
		prev.onInput = onInput
		return prev
	}

	gw := &GameWindow{
		Controls: make(map[string]CachedControl),
		renderFn: renderFn,
		onInput:  onInput,
		rootHash: rootHash,
	}

	prevControls := map[string]CachedControl{}
	if prev != nil {
		prevControls = prev.Controls
	}

	// Build the root control from the tree.
	root := gw.buildControl(tree, "0", prevControls)

	// Assemble into a Window with a single child that fills everything.
	// Focus cycling works through nested Containers/Panels which implement
	// TabWanter and route focus internally.
	gw.Window = &Window{
		Children: []GridChild{
			{
				Control:    root,
				Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth},
			},
		},
	}

	// Preserve focus index from previous window.
	if prev != nil && prev.Window != nil {
		gw.Window.FocusIdx = prev.Window.FocusIdx
	}

	return gw
}

// buildControl creates a Control from a WidgetNode, reusing prev controls
// where the type matches.
//
// Per-node caching: if the node's Hash() is non-zero (no interactive/gameview
// descendants) and matches the cached hash at the same path, the entire subtree
// is reused without rebuilding. This means only dynamic subtrees (containing
// gameview or interactive nodes) are rebuilt each frame.
// buildControl returns the Control. It populates gw.Controls with entries for
// this node and all descendants. Each CachedControl stores ChildPaths for
// efficient subtree reuse on cache hit.
func (gw *GameWindow) buildControl(node *common.WidgetNode, path string, prev map[string]CachedControl) Control {
	if node == nil {
		return &Label{Text: ""}
	}

	// Fast path: if the node's hash is non-zero and matches the previous
	// frame at the same path, reuse the entire cached subtree.
	hash := node.Hash()
	if hash != 0 {
		if cached, ok := prev[path]; ok && cached.Hash == hash {
			gw.Controls[path] = cached
			// Copy descendant entries directly — O(descendants) not O(all controls).
			for _, cp := range cached.ChildPaths {
				if v, ok := prev[cp]; ok {
					gw.Controls[cp] = v
				}
			}
			return cached.Control
		}
	}

	var ctrl Control
	var childPaths []string

	buildChild := func(child *common.WidgetNode, i int) Control {
		childPath := fmt.Sprintf("%s.%d", path, i)
		childPaths = append(childPaths, childPath)
		c := gw.buildControl(child, childPath, prev)
		// Propagate grandchild paths.
		if cached, ok := gw.Controls[childPath]; ok {
			childPaths = append(childPaths, cached.ChildPaths...)
		}
		return c
	}

	switch node.Type {
	case "label":
		ctrl = gw.buildLabel(node)
	case "panel":
		ctrl = gw.buildPanelWith(node, buildChild)
	case "divider":
		ctrl = &HDivider{Connected: false}
	case "table":
		ctrl = &Table{Rows: node.Rows}
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
		ctrl = gw.buildContainerWith(node, true, buildChild)
	case "vsplit":
		ctrl = gw.buildContainerWith(node, false, buildChild)
	default:
		ctrl = gw.buildGameView(node)
	}

	// Wrap cacheable controls in RenderCached for pixel-level caching.
	// Controls with hash=0 (gameview, interactive) render every frame.
	if hash != 0 {
		// Carry forward the cached pixels from prev if the wrapper exists.
		var prevRC *RenderCached
		if cached, ok := prev[path]; ok {
			prevRC, _ = cached.Control.(*RenderCached)
		}
		rc := &RenderCached{Inner: ctrl, Hash: hash}
		if prevRC != nil {
			rc.cached = prevRC.cached
			rc.cachedW = prevRC.cachedW
			rc.cachedH = prevRC.cachedH
			rc.prevHash = prevRC.prevHash
		}
		ctrl = rc
	}

	gw.Controls[path] = CachedControl{NodeType: node.Type, Control: ctrl, Hash: hash, ChildPaths: childPaths}
	return ctrl
}

func (gw *GameWindow) buildLabel(node *common.WidgetNode) *Label {
	return &Label{Text: node.Text, Align: node.Align}
}

func (gw *GameWindow) buildPanelWith(node *common.WidgetNode, buildChild func(*common.WidgetNode, int) Control) *Panel {
	panel := &Panel{Title: node.Title}

	for i, child := range node.Children {
		ctrl := buildChild(child, i)

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
	return panel
}

func (gw *GameWindow) buildButton(node *common.WidgetNode, path string, prev map[string]CachedControl) *Button {
	// Reuse existing button if same type at same path.
	if cached, ok := prev[path]; ok && cached.NodeType == "button" {
		btn := cached.Control.(*Button)
		btn.Label = node.Text
		return btn
	}
	action := node.Action
	return &Button{
		Label: node.Text,
		OnPress: func() {
			if gw.onInput != nil && action != "" {
				gw.onInput(action)
			}
		},
	}
}

func (gw *GameWindow) buildTextInput(node *common.WidgetNode, path string, prev map[string]CachedControl) *TextInput {
	// Reuse existing text input to preserve cursor/content state.
	if cached, ok := prev[path]; ok && cached.NodeType == "textinput" {
		ti := cached.Control.(*TextInput)
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
	ti := &TextInput{Model: m}
	ti.OnSubmit = func(text string) {
		if gw.onInput != nil && action != "" {
			gw.onInput(action + ":" + text)
		}
	}
	return ti
}

func (gw *GameWindow) buildCheckbox(node *common.WidgetNode, path string, prev map[string]CachedControl) *Checkbox {
	if cached, ok := prev[path]; ok && cached.NodeType == "checkbox" {
		cb := cached.Control.(*Checkbox)
		cb.Label = node.Text
		// Update checked state from JS (game may have changed it).
		cb.Checked = node.Checked
		return cb
	}
	action := node.Action
	return &Checkbox{
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

func (gw *GameWindow) buildTextView(node *common.WidgetNode, path string, prev map[string]CachedControl) *TextView {
	// Reuse existing textview to preserve scroll position.
	if cached, ok := prev[path]; ok && cached.NodeType == "textview" {
		tv := cached.Control.(*TextView)
		tv.Lines = node.Lines
		return tv
	}
	return &TextView{
		Lines:       node.Lines,
		BottomAlign: true,
		Scrollable:  true,
	}
}

func (gw *GameWindow) buildGameView(node *common.WidgetNode) *GameView {
	return &GameView{
		RenderFn:  gw.renderFn,
		OnKey:     gw.onInput,
		focusable: node.IsFocusable,
	}
}

func (gw *GameWindow) buildContainerWith(node *common.WidgetNode, horizontal bool, buildChild func(*common.WidgetNode, int) Control) *Container {
	container := &Container{Horizontal: horizontal}

	for i, child := range node.Children {
		ctrl := buildChild(child, i)

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
	return container
}

// ─── Label alignment support ───────────────────────────────────────────────

// The existing Label only supports plain text. For viewNC labels with
// alignment, we check the Align field and render accordingly.
// This is handled by adding an Align field to Label.

func init() {
	// Verify Label has Align field at compile time.
	_ = Label{}.Align
}
