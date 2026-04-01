package server

import (
	"strings"
	"testing"

	"null-space/common"
)

func TestRenderGameViewNode(t *testing.T) {
	theme := DefaultTheme()
	node := &common.WidgetNode{Type: "gameview"}

	called := false
	result := renderWidgetTree(node, 20, 5, theme, func(w, h int) string {
		called = true
		if w != 20 || h != 5 {
			t.Errorf("gameview got w=%d h=%d, want 20x5", w, h)
		}
		return "hello"
	})

	if !called {
		t.Fatal("viewFn was not called for gameview node")
	}
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestRenderPanelNode(t *testing.T) {
	theme := DefaultTheme()
	node := &common.WidgetNode{
		Type:  "panel",
		Title: "Test",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "Hello"},
		},
	}

	result := renderWidgetTree(node, 20, 5, theme, nil)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
	// Top border should contain title
	if !strings.Contains(lines[0], "Test") {
		t.Errorf("top border should contain title, got %q", lines[0])
	}
	// Should contain border characters
	if !strings.Contains(lines[0], "╔") {
		t.Errorf("should use double border, got %q", lines[0])
	}
}

func TestRenderHSplit(t *testing.T) {
	theme := DefaultTheme()
	node := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "LEFT", Weight: 1},
			{Type: "label", Text: "RIGHT", Width: 10},
		},
	}

	result := renderWidgetTree(node, 30, 3, theme, nil)
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	// First line should contain both texts
	if !strings.Contains(lines[0], "LEFT") || !strings.Contains(lines[0], "RIGHT") {
		t.Errorf("hsplit should show both children, got %q", lines[0])
	}
}

func TestRenderVSplit(t *testing.T) {
	theme := DefaultTheme()
	node := &common.WidgetNode{
		Type: "vsplit",
		Children: []*common.WidgetNode{
			{Type: "label", Text: "TOP", Height: 2},
			{Type: "label", Text: "BOTTOM", Weight: 1},
		},
	}

	result := renderWidgetTree(node, 20, 5, theme, nil)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "TOP") {
		t.Errorf("top section should contain TOP, got %q", lines[0])
	}
	if !strings.Contains(lines[2], "BOTTOM") {
		t.Errorf("bottom section should contain BOTTOM, got %q", lines[2])
	}
}

func TestNilViewNCFallsBack(t *testing.T) {
	// When ViewNC returns nil, chrome should fall back to View()
	// This is tested at the integration level, but verify the node rendering handles nil
	theme := DefaultTheme()
	result := renderWidgetTree(nil, 10, 3, theme, nil)
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("nil node should produce %d empty lines, got %d", 3, len(lines))
	}
}

func TestGameViewInsidePanel(t *testing.T) {
	theme := DefaultTheme()
	// A panel containing a gameview — the hybrid use case
	node := &common.WidgetNode{
		Type: "hsplit",
		Children: []*common.WidgetNode{
			{Type: "panel", Title: "Info", Width: 12, Children: []*common.WidgetNode{
				{Type: "label", Text: "Score: 42"},
			}},
			{Type: "gameview", Weight: 1},
		},
	}

	viewCalled := false
	result := renderWidgetTree(node, 30, 5, theme, func(w, h int) string {
		viewCalled = true
		return "GAME CONTENT"
	})

	if !viewCalled {
		t.Fatal("viewFn not called for embedded gameview")
	}
	if !strings.Contains(result, "Score: 42") {
		t.Error("panel content missing")
	}
	if !strings.Contains(result, "GAME CONTENT") {
		t.Error("gameview content missing")
	}
}
