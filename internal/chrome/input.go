package chrome

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/domain"
	"dev-null/internal/input"
	"dev-null/internal/widget"
)

// currentMode returns the current input mode for the router.
// Dialog takes precedence over Menu takes precedence over Desktop.
func (m *Model) currentMode() input.Mode {
	if m.overlay.HasDialog() {
		return input.ModeDialog
	}
	if m.overlay.MenuFocused || m.overlay.OpenMenu >= 0 {
		return input.ModeMenu
	}
	return input.ModeDesktop
}

// currentWindow returns the top-level window for the current game phase.
// Used for focus tracking and default key dispatch on Desktop.
func (m *Model) currentWindow() *widget.Window {
	if !m.inActiveGame {
		return m.lobbyWindow
	}
	return m.playingWindow
}

// currentFocus returns the focused widget that the router should consult
// for WantsEnter/WantsEsc. During starting/ending phases the phase buttons
// are the effective focus target; otherwise it is the focused child of
// the active window, descending into a GameView's inner widget tree when
// one has inner focus (so a focused TextInput inside a game is what the
// router asks about, not the GameView wrapper).
func (m *Model) currentFocus() any {
	if m.inActiveGame && m.api.State().GetGamePhase() == domain.PhaseStarting {
		return m.phaseReadyButton
	}
	win := m.currentWindow()
	if win == nil {
		return nil
	}
	if win.FocusIdx < 0 || win.FocusIdx >= len(win.Children) {
		return nil
	}
	ctrl := win.Children[win.FocusIdx].Control
	if gv, ok := ctrl.(*widget.GameView); ok {
		if inner := gv.FocusedChild(); inner != nil {
			return inner
		}
	}
	return ctrl
}

// focusCommandInput moves focus to the chat command input in the current
// window and starts the cursor blink.
func (m *Model) focusCommandInput() tea.Cmd {
	if !m.inActiveGame {
		// Lobby command input is already at FocusIdx 4; focus it.
		m.lobbyWindow.FocusIdx = 4
		return m.lobbyInput.Model.Focus()
	}
	m.playingWindow.FocusIdx = 4
	return m.playingInput.Model.Focus()
}

// activateMenuBar enters menu mode with the bar focused on the first item,
// no dropdown open. First Esc activates; second Esc returns to Desktop.
func (m *Model) activateMenuBar() {
	m.overlay.MenuFocused = true
	m.overlay.MenuCursor = 0
	m.overlay.OpenMenu = -1
	m.overlay.SubMenus = nil
}

// scrollChat scrolls the chat panel by the given direction: +1 page up, -1 page down.
func (m *Model) scrollChat(dir int) {
	chatH := max(1, m.chatH)
	m.chatScrollOffset += (chatH - 1) * dir
	maxOffset := len(m.chatLines) - chatH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.chatScrollOffset > maxOffset {
		m.chatScrollOffset = maxOffset
	}
	if m.chatScrollOffset < 0 {
		m.chatScrollOffset = 0
	}
}

// handleKey is the single entry point for keyboard input in the chrome.
// It consults the input router for a routing Action and executes it.
//
// The router owns the reserved-key set (Ctrl+C/D, Esc, Enter, PgUp/Dn)
// and the two-step Esc/Enter contract. Games never see reserved keys.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Rebuild menus once per key so overlays reflect current state.
	m.menuCache = nil

	action := input.Route(key, m.currentMode(), m.currentFocus())

	switch action {
	case input.ActionQuit:
		return m, tea.Quit

	case input.ActionScrollChatUp:
		m.scrollChat(+1)
		return m, nil

	case input.ActionScrollChatDown:
		m.scrollChat(-1)
		return m, nil

	case input.ActionCloseTopDialog:
		m.overlay.PopDialog()
		return m, nil

	case input.ActionRouteToDialog:
		_, cmd := m.overlay.HandleDialogMsg(msg)
		return m, cmd

	case input.ActionRouteToMenu:
		m.overlay.HandleKey(key, m.cachedMenus(), m.playerID)
		return m, nil

	case input.ActionActivateMenu:
		m.activateMenuBar()
		return m, nil

	case input.ActionFocusChat:
		cmd := m.focusCommandInput()
		return m, cmd

	case input.ActionRouteToFocused:
		return m.routeToFocused(msg)
	}

	return m, nil
}

// routeToFocused dispatches a non-reserved key (or a reserved key that
// the focused widget opted into via WantsEnter/WantsEsc) to the active
// focus target.
func (m *Model) routeToFocused(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key != "tab" {
		m.tabCandidates = nil
	}
	// The Ready button is the focus target during PhaseStarting. It is
	// not part of any Window's focus hierarchy, so dispatch directly.
	if m.inActiveGame && m.api.State().GetGamePhase() == domain.PhaseStarting {
		m.phaseReadyButton.Update(msg)
		return m, nil
	}
	win := m.currentWindow()
	if win == nil {
		return m, nil
	}
	return m, win.HandleUpdate(msg)
}

// showTeamRenameDialog pushes a modal dialog for renaming the current
// player's team. Submitting with a non-empty name calls RenameTeam.
func (m *Model) showTeamRenameDialog() {
	idx := m.api.State().PlayerTeamIndex(m.playerID)
	teams := m.api.State().GetTeams()
	if idx < 0 || idx >= len(teams) {
		return
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:       "Rename Team",
		InputPrompt: "Name",
		InputValue:  teams[idx].Name,
		Buttons:     []string{"OK", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn != "OK" {
				return
			}
			value = strings.TrimSpace(value)
			if value == "" {
				return
			}
			tidx := m.api.State().PlayerTeamIndex(m.playerID)
			if m.api.State().RenameTeam(tidx, value) {
				m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
			}
		},
	})
}

// handleTeamPanelClick maps a click position (relative to content area)
// to an action: clicking Unassigned unassigns, clicking a team joins it
// (or renames/recolors if owner), clicking a player returns their name,
// clicking [+] creates a new team.
// Returns the name of a clicked player (empty string if a non-player row was clicked).
func (m *Model) handleTeamPanelClick(panelX, contentY int) string {
	teams := m.api.State().GetTeams()
	unassigned := m.api.State().UnassignedPlayers()

	// Row 0: "Unassigned" header
	row := 0
	if contentY == row {
		if m.api.State().PlayerTeamIndex(m.playerID) >= 0 {
			m.api.State().MovePlayerToTeam(m.playerID, -1)
			m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
		}
		return ""
	}
	row++ // skip header

	// Unassigned player rows.
	for _, pid := range unassigned {
		if contentY == row {
			if p := m.api.State().GetPlayer(pid); p != nil {
				return p.Name
			}
			return pid
		}
		row++
	}

	// Each team: blank line, team header, player rows.
	// Team header layout: " XX TeamName" -> X 0=space, 1-2=color swatch, 3=space, 4+=name
	for i, team := range teams {
		if contentY == row {
			// Clicked on blank separator — ignore.
			return ""
		}
		row++ // advance past blank to team header
		if contentY == row {
			myIdx := m.api.State().PlayerTeamIndex(m.playerID)
			isFirst := m.api.State().IsFirstInTeam(m.playerID)
			if myIdx == i && isFirst {
				// Owner clicked own team header.
				if panelX >= 1 && panelX <= 2 {
					// Clicked on color swatch — cycle color.
					m.api.State().NextTeamColor(i, 1)
					m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
				} else {
					// Clicked on team name — open the rename dialog.
					m.showTeamRenameDialog()
				}
			} else if myIdx != i {
				m.api.State().MovePlayerToTeam(m.playerID, i)
				m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
			}
			return ""
		}
		row++ // advance past header
		for _, pid := range team.Players {
			if contentY == row {
				if p := m.api.State().GetPlayer(pid); p != nil {
					return p.Name
				}
				return pid
			}
			row++
		}
	}

	// After all teams: blank + [+ Create Team] button.
	row++ // blank line
	if contentY == row && !m.api.State().IsSoleMemberOfTeam(m.playerID) {
		m.api.State().MovePlayerToTeam(m.playerID, len(teams))
		m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
	}
	return ""
}

// handleMouseWheel scrolls the chat panel on wheel events.
func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	scrollAmount := 3
	chatH := max(1, m.chatH)
	switch msg.Button {
	case tea.MouseWheelUp:
		m.chatScrollOffset += scrollAmount
		maxOffset := len(m.chatLines) - chatH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.chatScrollOffset > maxOffset {
			m.chatScrollOffset = maxOffset
		}
	case tea.MouseWheelDown:
		m.chatScrollOffset -= scrollAmount
		if m.chatScrollOffset < 0 {
			m.chatScrollOffset = 0
		}
	}
	return m, nil
}
