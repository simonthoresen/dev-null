package server

import (
	"strings"
	"testing"

	"null-space/common"
	"null-space/internal/theme"
)

func TestReconcileLabel(t *testing.T) {
	tree := &common.WidgetNode{Type: "label", Text: "Hello"}
	gw := ReconcileGameWindow(nil, tree, nil, nil)
	if gw == nil || gw.Window == nil {
		t.Fatal("expected non-nil GameNCWindow")
	}
	output := gw.Window.Render(0, 0, 20, 3, theme.Default().LayerAt(0))
	s := newScreen(output)
	// The label should appear somewhere in the rendered output.
	found := false
	for _, l := range s.lines {
		if strings.Contains(l, "Hello") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("label 'Hello' not found in output:\n%s", s.String())
	}
}

func TestReconcilePanel(t *testing.T) {
	tree := &common.WidgetNode{
		Type:  "panel",
		Title: "Info",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "Line 1"},
		},
	}
	gw := ReconcileGameWindow(nil, tree, nil, nil)
	output := gw.Window.Render(0, 0, 20, 5, theme.Default().LayerAt(0))
	s := newScreen(output)
	// Panel is inside the NCWindow wrapper, so look for title anywhere.
	if !strings.Contains(s.String(), "Info") {
		t.Errorf("panel title 'Info' not found:\n%s", s.String())
	}
	if !strings.Contains(s.String(), "Line 1") {
		t.Errorf("panel content 'Line 1' not found:\n%s", s.String())
	}
}

func TestReconcileHSplit(t *testing.T) {
	tree := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "LEFT", Weight: 1},
			{Type: "label", Text: "RIGHT", Weight: 1},
		},
	}
	gw := ReconcileGameWindow(nil, tree, nil, nil)
	output := gw.Window.Render(0, 0, 20, 3, theme.Default().LayerAt(0))
	s := newScreen(output)
	// Both labels should appear on the same row.
	found := false
	for _, l := range s.lines {
		if strings.Contains(l, "LEFT") && strings.Contains(l, "RIGHT") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hsplit should show LEFT and RIGHT side by side:\n%s", s.String())
	}
}

func TestReconcileVSplit(t *testing.T) {
	tree := &common.WidgetNode{
		Type: "vsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "TOP", Height: 1},
			{Type: "label", Text: "BOTTOM", Weight: 1},
		},
	}
	gw := ReconcileGameWindow(nil, tree, nil, nil)
	output := gw.Window.Render(0, 0, 20, 4, theme.Default().LayerAt(0))
	s := newScreen(output)
	// TOP should appear in the inner area (row 1, since row 0 is the window border).
	if !strings.Contains(s.String(), "TOP") {
		t.Errorf("TOP not found:\n%s", s.String())
	}
	if !strings.Contains(s.String(), "BOTTOM") {
		t.Errorf("BOTTOM not found:\n%s", s.String())
	}
}

func TestReconcileGameView(t *testing.T) {
	tree := &common.WidgetNode{Type: "gameview"}
	called := false
	gw := ReconcileGameWindow(nil, tree,
		func(buf *common.ImageBuffer, x, y, w, h int) {
			called = true
			buf.WriteString(x, y, "GAME OUTPUT", nil, nil, common.AttrNone)
		}, nil)
	output := gw.Window.Render(0, 0, 20, 3, theme.Default().LayerAt(0))
	if !called {
		t.Error("viewFn was not called")
	}
	s := newScreen(output)
	if !strings.Contains(s.String(), "GAME OUTPUT") {
		t.Errorf("gameview content missing:\n%s", s.String())
	}
}

func TestReconcileButton(t *testing.T) {
	tree := &common.WidgetNode{
		Type:   "button",
		Text:   "Fold",
		Action: "fold",
	}
	var received string
	gw := ReconcileGameWindow(nil, tree, nil, func(action string) {
		received = action
	})
	output := gw.Window.Render(0, 0, 20, 3, theme.Default().LayerAt(0))
	s := newScreen(output)
	if !strings.Contains(s.String(), "Fold") {
		t.Errorf("button label 'Fold' not found:\n%s", s.String())
	}

	// The button should be focusable.
	if !gw.HasFocusable() {
		t.Error("expected HasFocusable() = true")
	}

	// Simulate pressing the button.
	root := gw.Window.Children[0].Control
	if btn, ok := findButton(root); ok {
		btn.OnPress()
		if received != "fold" {
			t.Errorf("expected OnInput('fold'), got %q", received)
		}
	} else {
		t.Error("no button found in tree")
	}
}

func TestReconcilePreservesState(t *testing.T) {
	tree := &common.WidgetNode{
		Type: "vsplit",
		Children: []*common.WidgetNode{
			{Type: "textview", Lines: []string{"line1", "line2", "line3", "line4", "line5"}},
			{Type: "label", Text: "footer", Height: 1},
		},
	}

	// First reconcile — creates fresh controls.
	gw1 := ReconcileGameWindow(nil, tree, nil, nil)

	// Find the textview and change its scroll offset.
	if tv, ok := findTextView(gw1.Window.Children[0].Control); ok {
		tv.ScrollOffset = 2
	}

	// Second reconcile — same tree. Should reuse the textview and preserve scroll.
	tree2 := &common.WidgetNode{
		Type: "vsplit",
		Children: []*common.WidgetNode{
			{Type: "textview", Lines: []string{"line1", "line2", "line3", "line4", "line5"}},
			{Type: "label", Text: "footer", Height: 1},
		},
	}
	gw2 := ReconcileGameWindow(gw1, tree2, nil, nil)

	if tv, ok := findTextView(gw2.Window.Children[0].Control); ok {
		if tv.ScrollOffset != 2 {
			t.Errorf("expected scroll offset preserved (2), got %d", tv.ScrollOffset)
		}
	} else {
		t.Error("textview not found after reconcile")
	}
}

func TestReconcileTable(t *testing.T) {
	tree := &common.WidgetNode{
		Type: "table",
		Rows: [][]string{
			{"Name", "Score"},
			{"Alice", "100"},
			{"Bob", "200"},
		},
	}
	gw := ReconcileGameWindow(nil, tree, nil, nil)
	output := gw.Window.Render(0, 0, 30, 5, theme.Default().LayerAt(0))
	s := newScreen(output)
	if !strings.Contains(s.String(), "Alice") || !strings.Contains(s.String(), "Score") {
		t.Errorf("table content missing:\n%s", s.String())
	}
}

func TestReconcileCacheSkipsStaticSubtree(t *testing.T) {
	// An hsplit with a static panel and a gameview. The static panel should
	// be reused from cache on the second reconcile (same hash, same path).
	tree := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "panel", Title: "Stats", Width: 12, Children: []*common.WidgetNode{
				{Type: "label", Text: "HP: 100"},
			}},
			{Type: "gameview", Weight: 1},
		},
	}

	viewCallCount := 0
	viewFn := func(buf *common.ImageBuffer, x, y, w, h int) {
		viewCallCount++
		buf.WriteString(x, y, "frame", nil, nil, common.AttrNone)
	}

	gw1 := ReconcileGameWindow(nil, tree, viewFn, nil)
	_ = gw1.Window.Render(0, 0, 30, 5, theme.Default().LayerAt(0))
	if viewCallCount != 1 {
		t.Fatalf("expected 1 viewFn call, got %d", viewCallCount)
	}

	// The panel subtree at path "0.0" should have a non-zero hash.
	panelNode := tree.Children[0]
	if panelNode.Hash() == 0 {
		t.Fatal("static panel should have non-zero hash")
	}

	// Second reconcile with same tree — static panel should be reused.
	tree2 := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "panel", Title: "Stats", Width: 12, Children: []*common.WidgetNode{
				{Type: "label", Text: "HP: 100"},
			}},
			{Type: "gameview", Weight: 1},
		},
	}

	gw2 := ReconcileGameWindow(gw1, tree2, viewFn, nil)
	_ = gw2.Window.Render(0, 0, 30, 5, theme.Default().LayerAt(0))

	// viewFn should be called again (gameview always rebuilds).
	if viewCallCount != 2 {
		t.Errorf("expected 2 viewFn calls, got %d", viewCallCount)
	}

	// The cached panel control at "0.0" should be the exact same pointer.
	cached1, ok1 := gw1.Controls["0.0"]
	cached2, ok2 := gw2.Controls["0.0"]
	if !ok1 || !ok2 {
		t.Fatal("expected panel control at path 0.0 in both reconciles")
	}
	if cached1.Control != cached2.Control {
		t.Error("static panel control should be reused (same pointer), but got different instances")
	}
}

func TestReconcileCacheInvalidatesOnChange(t *testing.T) {
	tree1 := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "v1", Width: 10},
			{Type: "gameview", Weight: 1},
		},
	}

	gw1 := ReconcileGameWindow(nil, tree1, func(buf *common.ImageBuffer, x, y, w, h int) {}, nil)

	// Change the label text — hash should differ, so it gets rebuilt.
	tree2 := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "v2", Width: 10},
			{Type: "gameview", Weight: 1},
		},
	}

	gw2 := ReconcileGameWindow(gw1, tree2, func(buf *common.ImageBuffer, x, y, w, h int) {}, nil)

	cached1 := gw1.Controls["0.0"]
	cached2 := gw2.Controls["0.0"]
	if cached1.Control == cached2.Control {
		t.Error("label control should be rebuilt when text changes, but same pointer was reused")
	}

	// Verify the new label has the updated text.
	if label, ok := cached2.Control.(*NCLabel); ok {
		if label.Text != "v2" {
			t.Errorf("expected label text 'v2', got %q", label.Text)
		}
	} else {
		t.Error("expected *NCLabel at path 0.0")
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// findButton searches a control tree for the first NCButton.
func findButton(ctrl NCControl) (*NCButton, bool) {
	if btn, ok := ctrl.(*NCButton); ok {
		return btn, true
	}
	if c, ok := ctrl.(*NCContainer); ok {
		for _, child := range c.Children {
			if btn, ok := findButton(child.Control); ok {
				return btn, true
			}
		}
	}
	if p, ok := ctrl.(*NCPanel); ok {
		for _, child := range p.Children {
			if btn, ok := findButton(child.Control); ok {
				return btn, true
			}
		}
	}
	return nil, false
}

// findTextView searches a control tree for the first NCTextView.
func findTextView(ctrl NCControl) (*NCTextView, bool) {
	if tv, ok := ctrl.(*NCTextView); ok {
		return tv, true
	}
	if c, ok := ctrl.(*NCContainer); ok {
		for _, child := range c.Children {
			if tv, ok := findTextView(child.Control); ok {
				return tv, true
			}
		}
	}
	if p, ok := ctrl.(*NCPanel); ok {
		for _, child := range p.Children {
			if tv, ok := findTextView(child.Control); ok {
				return tv, true
			}
		}
	}
	return nil, false
}
