package input

import "testing"

// Test fixtures: widgets implementing various consumer combinations.

type noConsumer struct{}

type enterWanter struct{ want bool }

func (e enterWanter) WantsEnter() bool { return e.want }

type escWanter struct{ want bool }

func (e escWanter) WantsEsc() bool { return e.want }

type bothWanter struct{ enter, esc bool }

func (b bothWanter) WantsEnter() bool { return b.enter }
func (b bothWanter) WantsEsc() bool   { return b.esc }

func TestQuitKeys(t *testing.T) {
	// Ctrl+C always quits, in any mode, regardless of focus. Ctrl+D is
	// deliberately NOT reserved so games may bind it.
	modes := []Mode{ModeDesktop, ModeMenu, ModeDialog}
	focuses := []any{nil, noConsumer{}, bothWanter{enter: true, esc: true}}
	for _, mode := range modes {
		for _, f := range focuses {
			if got := Route("ctrl+c", mode, f); got != ActionQuit {
				t.Errorf("Route(ctrl+c, mode=%d, focus=%T) = %v, want ActionQuit", mode, f, got)
			}
		}
	}
}

func TestCtrlDNotReserved(t *testing.T) {
	// Ctrl+D must pass through to the focused widget so games can bind it.
	// Dialog/menu modes route it to the modal handler (not Quit).
	if got := Route("ctrl+d", ModeDesktop, nil); got != ActionRouteToFocused {
		t.Errorf("Route(ctrl+d, Desktop) = %v, want ActionRouteToFocused", got)
	}
	if got := Route("ctrl+d", ModeDialog, nil); got != ActionRouteToDialog {
		t.Errorf("Route(ctrl+d, Dialog) = %v, want ActionRouteToDialog", got)
	}
	if got := Route("ctrl+d", ModeMenu, nil); got != ActionRouteToMenu {
		t.Errorf("Route(ctrl+d, Menu) = %v, want ActionRouteToMenu", got)
	}
}

func TestDialogMode(t *testing.T) {
	cases := []struct {
		key  string
		want Action
	}{
		{"esc", ActionCloseTopDialog},
		{"enter", ActionRouteToDialog},
		{"a", ActionRouteToDialog},
		{"tab", ActionRouteToDialog},
		{"pgup", ActionRouteToDialog}, // dialog takes everything except Esc
		{"up", ActionRouteToDialog},
	}
	for _, tc := range cases {
		if got := Route(tc.key, ModeDialog, nil); got != tc.want {
			t.Errorf("dialog Route(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestMenuMode(t *testing.T) {
	// Menu mode routes every non-quit key to the menu state machine.
	keys := []string{"esc", "enter", "a", "tab", "pgup", "up", "down", "left", "right", "alt+f"}
	for _, key := range keys {
		if got := Route(key, ModeMenu, nil); got != ActionRouteToMenu {
			t.Errorf("menu Route(%q) = %v, want ActionRouteToMenu", key, got)
		}
	}
}

func TestDesktopScroll(t *testing.T) {
	if got := Route("pgup", ModeDesktop, nil); got != ActionScrollChatUp {
		t.Errorf("Route(pgup) = %v, want ActionScrollChatUp", got)
	}
	if got := Route("pgdown", ModeDesktop, nil); got != ActionScrollChatDown {
		t.Errorf("Route(pgdown) = %v, want ActionScrollChatDown", got)
	}
}

func TestDesktopEsc(t *testing.T) {
	cases := []struct {
		name    string
		focused any
		want    Action
	}{
		{"nil focus activates menu", nil, ActionActivateMenu},
		{"non-consumer activates menu", noConsumer{}, ActionActivateMenu},
		{"WantsEsc=false activates menu", escWanter{want: false}, ActionActivateMenu},
		{"WantsEsc=true routes to focused", escWanter{want: true}, ActionRouteToFocused},
		{"enter-only widget still activates menu", enterWanter{want: true}, ActionActivateMenu},
		{"both wanter with esc=true routes", bothWanter{enter: false, esc: true}, ActionRouteToFocused},
		{"both wanter with esc=false activates menu", bothWanter{enter: true, esc: false}, ActionActivateMenu},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Route("esc", ModeDesktop, tc.focused); got != tc.want {
				t.Errorf("Route(esc, focus=%+v) = %v, want %v", tc.focused, got, tc.want)
			}
		})
	}
}

func TestDesktopEnter(t *testing.T) {
	cases := []struct {
		name    string
		focused any
		want    Action
	}{
		{"nil focus focuses chat", nil, ActionFocusChat},
		{"non-consumer focuses chat", noConsumer{}, ActionFocusChat},
		{"WantsEnter=false focuses chat", enterWanter{want: false}, ActionFocusChat},
		{"WantsEnter=true routes to focused", enterWanter{want: true}, ActionRouteToFocused},
		{"esc-only widget still focuses chat", escWanter{want: true}, ActionFocusChat},
		{"both wanter with enter=true routes", bothWanter{enter: true, esc: false}, ActionRouteToFocused},
		{"both wanter with enter=false focuses chat", bothWanter{enter: false, esc: true}, ActionFocusChat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Route("enter", ModeDesktop, tc.focused); got != tc.want {
				t.Errorf("Route(enter, focus=%+v) = %v, want %v", tc.focused, got, tc.want)
			}
		})
	}
}

func TestDesktopOtherKeys(t *testing.T) {
	// All non-reserved keys fall through to the focused widget regardless
	// of consumer interfaces.
	keys := []string{"a", "z", "0", "tab", "shift+tab", "up", "down", "left", "right", "space", "f1", "alt+f", "ctrl+u"}
	focuses := []any{nil, noConsumer{}, enterWanter{want: true}, escWanter{want: true}, bothWanter{enter: true, esc: true}}
	for _, key := range keys {
		for _, f := range focuses {
			if got := Route(key, ModeDesktop, f); got != ActionRouteToFocused {
				t.Errorf("Route(%q, focus=%T) = %v, want ActionRouteToFocused", key, f, got)
			}
		}
	}
}

// Two-step Esc behavior: a focused CommandInput-like widget reports
// WantsEsc() true while it has a draft (consume to clear), then false
// once empty (next Esc activates the menu). Simulate the transition.
func TestTwoStepEsc(t *testing.T) {
	// Turn 1: widget has text, WantsEsc=true → route to focused.
	drafted := escWanter{want: true}
	if got := Route("esc", ModeDesktop, drafted); got != ActionRouteToFocused {
		t.Errorf("first Esc with draft = %v, want ActionRouteToFocused", got)
	}
	// Turn 2: widget cleared its value, WantsEsc=false → activate menu.
	empty := escWanter{want: false}
	if got := Route("esc", ModeDesktop, empty); got != ActionActivateMenu {
		t.Errorf("second Esc after clear = %v, want ActionActivateMenu", got)
	}
}

// Two-step Enter isn't really "two step" — Enter either submits or
// focuses chat — but the routing contract mirrors Esc symmetrically.
func TestEnterSymmetryWithEsc(t *testing.T) {
	// Unfocused command input: WantsEnter=false → focus chat.
	unfocused := enterWanter{want: false}
	if got := Route("enter", ModeDesktop, unfocused); got != ActionFocusChat {
		t.Errorf("Enter on unfocused = %v, want ActionFocusChat", got)
	}
	// Now focused: WantsEnter=true → route to widget.
	focused := enterWanter{want: true}
	if got := Route("enter", ModeDesktop, focused); got != ActionRouteToFocused {
		t.Errorf("Enter on focused = %v, want ActionRouteToFocused", got)
	}
}
