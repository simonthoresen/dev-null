package chrome

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/domain"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	phase := m.api.State().GetGamePhase()

	// Ctrl+C / Ctrl+D quit from any mode.
	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		return m, tea.Quit
	}

	// Chat scroll — handled in all modes.
	switch msg.String() {
	case "pgup":
		chatH := max(1, m.chatH)
		m.chatScrollOffset += chatH - 1
		maxOffset := len(m.chatLines) - chatH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.chatScrollOffset > maxOffset {
			m.chatScrollOffset = maxOffset
		}
		return m, nil
	case "pgdown":
		chatH := max(1, m.chatH)
		m.chatScrollOffset -= chatH - 1
		if m.chatScrollOffset < 0 {
			m.chatScrollOffset = 0
		}
		return m, nil
	}

	// Dialog overlay gets the real tea.Msg for proper NC control handling.
	if m.overlay.HasDialog() {
		consumed, cmd := m.overlay.HandleDialogMsg(msg)
		if consumed {
			return m, cmd
		}
	}

	// Menu overlay intercepts keys when active (F10, menu navigation).
	m.menuCache = nil // force fresh closures bound to current &m
	if m.overlay.HandleKey(msg.String(), m.cachedMenus(), m.playerID) {
		return m, nil
	}

	// Team rename mode — capture all keys for the team name input.
	if m.teamEditing {
		return m.handleTeamEditKey(msg)
	}

	// Lobby: delegate to the NCWindow which routes to the focused child
	// (NCCommandInput or NCTeamPanel). Tab cycling is handled by the window.
	if !m.inActiveGame {
		cmd := m.lobbyWindow.HandleUpdate(msg)
		// Reset tab candidates on non-tab keys.
		if msg.String() != "tab" {
			m.tabCandidates = nil
		}
		return m, cmd
	}

	// Starting phase — admin can press Enter to start, others wait.
	if m.inActiveGame && phase == domain.PhaseStarting {
		switch msg.String() {
		case "enter":
			player := m.api.State().GetPlayer(m.playerID)
			if player != nil && player.IsAdmin {
				m.api.StartGame()
			}
		}
		return m, nil
	}

	// Ending phase — Enter acknowledges.
	if m.inActiveGame && phase == domain.PhaseEnding {
		switch msg.String() {
		case "enter":
			m.api.AcknowledgeGameOver(m.playerID)
		}
		return m, nil
	}

	// Playing: delegate to the playing NCWindow which routes to the focused child
	// (GameView, NCCommandInput, or NC-tree controls).
	cmd := m.playingWindow.HandleUpdate(msg)
	// Reset tab candidates on non-tab keys.
	if msg.String() != "tab" {
		m.tabCandidates = nil
	}
	return m, cmd
}

func (m Model) handleTeamEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := strings.TrimSpace(m.teamEditInput.Value())
		if name != "" {
			idx := m.api.State().PlayerTeamIndex(m.playerID)
			if m.api.State().RenameTeam(idx, name) {
				m.api.BroadcastMsg(domain.TeamUpdatedMsg{})
			}
		}
		m.teamEditing = false
		m.teamEditInput.Blur()
		return m, nil
	case "esc":
		m.teamEditing = false
		m.teamEditInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.teamEditInput, cmd = m.teamEditInput.Update(msg)
		return m, cmd
	}
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
					// Clicked on team name — enter rename mode.
					m.teamEditing = true
					m.teamEditInput.SetValue(team.Name)
					m.teamEditInput.Focus()
					m.teamEditInput.CursorEnd()
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
func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
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
