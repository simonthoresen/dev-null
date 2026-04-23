// Package input is the single source of truth for keyboard routing.
//
// The router is a pure function: given a key string, a mode, and the
// currently focused widget (as an opaque any), it returns an Action for
// the caller to execute. Mutations live at the call site.
//
// Design notes:
//
//   - A small, fixed set of keys is framework-reserved (Ctrl+C, Esc,
//     Enter, PgUp/PgDn, Tab). Games never receive these via OnInput.
//     Ctrl+D is deliberately NOT reserved — games may bind it (some
//     roguelikes use it). Users can still exit via Ctrl+C or the
//     File → Exit menu item.
//   - Esc and Enter are two-step on Desktop: the focused widget may
//     consume them by implementing EscConsumer / EnterConsumer and
//     returning true from WantsEsc / WantsEnter. Otherwise the framework
//     takes the key (activate menu / focus chat).
//   - Dialogs are strictly modal: Esc always closes the top dialog; all
//     other keys are routed to the dialog's focused widget.
//   - The menu owns all input while active.
package input

// Mode is the current top-level input mode. Exactly one mode is active.
type Mode int

const (
	// ModeDesktop is the default mode: no overlay is active.
	ModeDesktop Mode = iota
	// ModeMenu is active when the menu bar is focused or a dropdown is open.
	ModeMenu
	// ModeDialog is active when at least one modal dialog is on the stack.
	ModeDialog
)

// Action describes what the caller should do in response to a key.
type Action int

const (
	// ActionNone means the key was ignored (no-op).
	ActionNone Action = iota
	// ActionQuit means terminate the session (Ctrl+C).
	ActionQuit
	// ActionScrollChatUp means scroll the chat panel up.
	ActionScrollChatUp
	// ActionScrollChatDown means scroll the chat panel down.
	ActionScrollChatDown
	// ActionCloseTopDialog means pop the top dialog from the stack.
	ActionCloseTopDialog
	// ActionRouteToDialog means forward the key to the top dialog's focused widget.
	ActionRouteToDialog
	// ActionRouteToMenu means forward the key to the menu state machine.
	ActionRouteToMenu
	// ActionActivateMenu means enter ModeMenu with the menu bar focused
	// on the first menu, no dropdown open.
	ActionActivateMenu
	// ActionFocusChat means move focus to the chat command input.
	ActionFocusChat
	// ActionRouteToFocused means forward the key to the focused widget in
	// the current Desktop window (GameView, CommandInput, NC widget, etc.).
	ActionRouteToFocused
)

// EnterConsumer is implemented by focused widgets that want to handle
// Enter themselves instead of letting the framework focus the chat input.
type EnterConsumer interface {
	WantsEnter() bool
}

// EscConsumer is implemented by focused widgets that want to handle Esc
// themselves instead of letting the framework activate the menu.
type EscConsumer interface {
	WantsEsc() bool
}

// Route decides what to do with a key press. It is a pure function: it
// never mutates state, it never calls callbacks. The caller inspects the
// returned Action and performs the side effect.
//
// key is the Bubble Tea key string (e.g. "enter", "esc", "ctrl+c", "a").
// focused is the currently focused widget, or nil if there is no focus.
// The router type-asserts focused against EnterConsumer / EscConsumer to
// decide whether Enter / Esc should pass through.
func Route(key string, mode Mode, focused any) Action {
	// Quit is never overridable.
	if key == "ctrl+c" {
		return ActionQuit
	}

	switch mode {
	case ModeDialog:
		if key == "esc" {
			return ActionCloseTopDialog
		}
		return ActionRouteToDialog

	case ModeMenu:
		return ActionRouteToMenu
	}

	// ModeDesktop from here on.

	if key == "pgup" {
		return ActionScrollChatUp
	}
	if key == "pgdown" {
		return ActionScrollChatDown
	}

	if key == "esc" {
		if ec, ok := focused.(EscConsumer); ok && ec.WantsEsc() {
			return ActionRouteToFocused
		}
		return ActionActivateMenu
	}

	if key == "enter" {
		if ec, ok := focused.(EnterConsumer); ok && ec.WantsEnter() {
			return ActionRouteToFocused
		}
		return ActionFocusChat
	}

	return ActionRouteToFocused
}
