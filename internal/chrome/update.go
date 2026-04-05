package chrome

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"dev-null/internal/domain"
	"dev-null/internal/render"
	"dev-null/internal/widget"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case domain.TickMsg:
		return m.handleTick(msg)
	case domain.ChatMsg:
		return m.handleChat(msg)
	case domain.PlayerJoinedMsg, domain.PlayerLeftMsg:
		m.syncChat()
		return m, nil
	case domain.TeamUpdatedMsg:
		return m, tea.ClearScreen
	case domain.GameLoadedMsg:
		return m.handleGameLoaded(msg)
	case domain.GameUnloadedMsg:
		return m.handleGameUnloaded()
	case domain.GamePhaseMsg:
		return m.handleGamePhase(msg)
	case widget.ShowDialogMsg:
		m.overlay.PushDialog(msg.Dialog)
		return m, nil
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Forward other messages to textinput for cursor blink etc.
	if !m.inActiveGame {
		updated, cmd := m.lobbyInput.Model.Update(msg)
		*m.lobbyInput.Model = updated
		return m, cmd
	}
	updated, cmd := m.playingInput.Model.Update(msg)
	*m.playingInput.Model = updated
	return m, cmd
}

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = max(1, msg.Width)
	m.height = max(8, msg.Height)
	m.resizeViewports()
	m.syncChat()
	return m, nil
}

func (m Model) handleTick(_ domain.TickMsg) (tea.Model, tea.Cmd) {
	if len(m.InitCommands) > 0 {
		for _, cmd := range m.InitCommands {
			if strings.HasPrefix(cmd, "/plugin") {
				m.handlePluginCommand(cmd)
			} else if strings.HasPrefix(cmd, "/theme") {
				m.handleThemeCommand(cmd)
			} else if strings.HasPrefix(cmd, "/") {
				m.dispatchPluginReply(cmd)
			}
		}
		m.InitCommands = nil
	}
	return m, nil
}

func (m Model) handleChat(msg domain.ChatMsg) (tea.Model, tea.Cmd) {
	chatMsg := msg.Msg
	if chatMsg.IsPrivate {
		if chatMsg.ToID != m.playerID && chatMsg.FromID != m.playerID {
			return m, nil
		}
	}

	// Extract sound OSC for graphical clients before any early-return.
	if m.IsEnhancedClient && !m.IsTerminalClient {
		if chatMsg.SoundStop {
			m.pendingSoundOSC = append(m.pendingSoundOSC, render.EncodeStopSoundOSC(chatMsg.SoundFile))
		} else if chatMsg.SoundFile != "" {
			m.pendingSoundOSC = append(m.pendingSoundOSC, render.EncodeSoundOSC(chatMsg.SoundFile, chatMsg.SoundLoop))
		}
	}

	// Messages with no text (sound-only events) have nothing to display.
	if chatMsg.Text == "" {
		return m, nil
	}

	var line string
	switch {
	case chatMsg.IsReply:
		line = chatMsg.Text
	case chatMsg.IsPrivate:
		from := chatMsg.FromID
		if p := m.api.State().GetPlayer(from); p != nil {
			from = p.Name
		}
		if from == "" {
			from = "admin"
		}
		line = fmt.Sprintf("[PM from %s] %s", from, chatMsg.Text)
	case chatMsg.Author == "":
		line = fmt.Sprintf("[system] %s", chatMsg.Text)
	default:
		line = fmt.Sprintf("<%s> %s", chatMsg.Author, chatMsg.Text)
	}
	for _, l := range strings.Split(line, "\n") {
		m.chatLines = append(m.chatLines, l)
	}
	if len(m.chatLines) > domain.MaxChatDisplayLines {
		m.chatLines = m.chatLines[len(m.chatLines)-domain.MaxChatDisplayLines:]
	}

	// Run per-player plugins on this message.
	if chatMsg.FromID != m.playerID && !chatMsg.IsReply && !chatMsg.IsFromPlugin {
		isSystem := chatMsg.Author == ""
		for _, pl := range m.plugins {
			if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
				m.dispatchPluginReply(reply)
			}
		}
	}
	return m, nil
}

func (m Model) handleGameLoaded(_ domain.GameLoadedMsg) (tea.Model, tea.Cmd) {
	m.inActiveGame = true
	m.charmapSent = false
	m.gameSrcSent = false
	m.assetsSent = false
	m.pendingSoundOSC = nil
	m.lastStateJSON = ""
	m.localModeSent = false
	m.localRendering = m.IsEnhancedClient // default on for enhanced clients
	m.renderMode = m.bestRenderMode()
	m.invalidateMenuCache()
	m.lobbyInput.Model.Blur()
	m.playingWindow.FocusIdx = 0
	m.resizeViewports()
	return m, nil
}

func (m Model) handleGameUnloaded() (tea.Model, tea.Cmd) {
	m.inActiveGame = false
	m.invalidateMenuCache()
	m.lobbyWindow.FocusIdx = 4
	cmd := m.lobbyInput.Model.Focus()
	m.playingInput.Model.Blur()
	m.resizeViewports()
	return m, cmd
}

func (m Model) handleGamePhase(msg domain.GamePhaseMsg) (tea.Model, tea.Cmd) {
	if msg.Phase == domain.PhaseEnding {
		m.gameOverStart = time.Now()
	}
	if msg.Phase == domain.PhaseNone {
		m.inActiveGame = false
		m.lobbyWindow.FocusIdx = 4
		cmd := m.lobbyInput.Model.Focus()
		m.resizeViewports()
		return m, cmd
	}
	m.resizeViewports()
	return m, nil
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseLeft {
		m.menuCache = nil // force fresh closures bound to current &m
		if m.overlay.HandleClick(msg.X, msg.Y, 0, m.width, m.height, m.cachedMenus(), m.playerID) {
			return m, nil
		}
	}
	if !m.inActiveGame && msg.Button == tea.MouseLeft {
		if m.lobbyWindow.HandleClick(msg.X, msg.Y) {
			if m.lobbyWindow.FocusIdx == 2 {
				cx, cy, _, _ := m.lobbyWindow.ChildRect(2)
				clickedPlayer := m.handleTeamPanelClick(msg.X-cx, msg.Y-cy)
				if clickedPlayer != "" {
					m.lobbyWindow.FocusIdx = 4
					m.lobbyInput.Model.Focus()
					if m.lobbyInput.Model.Value() == "" {
						m.lobbyInput.Model.SetValue("/msg " + clickedPlayer + " ")
						m.lobbyInput.Model.CursorEnd()
					} else {
						val := m.lobbyInput.Model.Value()
						pos := m.lobbyInput.Model.Position()
						m.lobbyInput.Model.SetValue(val[:pos] + clickedPlayer + val[pos:])
						m.lobbyInput.Model.SetCursor(pos + len(clickedPlayer))
					}
					return m, nil
				}
			}
		}
	}
	return m, nil
}
