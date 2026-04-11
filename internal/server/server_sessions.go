package server

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"

	"dev-null/internal/chrome"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/network"
)

// --- SSH session middleware and program handler ---

func (a *Server) sessionMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			player := a.registerSession(sess)
			defer a.unregisterSession(player.ID)
			next(sess)
		}
	}
}

func (a *Server) programHandler(sess ssh.Session) *tea.Program {
	playerID := sess.Context().SessionID()
	model := chrome.NewModel(a, playerID)

	// Check for init commands and enhanced client flag from SSH env vars.
	for _, e := range sess.Environ() {
		if strings.HasPrefix(e, "DEV_NULL_INIT=") {
			encoded := strings.TrimPrefix(e, "DEV_NULL_INIT=")
			if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(decoded)), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						model.InitCommands = append(model.InitCommands, line)
					}
				}
			}
		}
		if e == "DEV_NULL_CLIENT=enhanced" {
			model.IsEnhancedClient = true
			model.SessionWriter = sess
		}
	}

	// DEV_NULL_PASSWORD: auto-authenticate as admin on connect.
	for _, e := range sess.Environ() {
		if strings.HasPrefix(e, "DEV_NULL_PASSWORD=") {
			pw := strings.TrimPrefix(e, "DEV_NULL_PASSWORD=")
			model.InitCommands = append(model.InitCommands, "/password "+pw)
			break
		}
	}

	opts, cp := a.sessionProgramOptions(sess)
	model.ColorProfile = cp

	program := tea.NewProgram(model, opts...)
	a.programsMu.Lock()
	a.programs[playerID] = program
	a.programsMu.Unlock()

	// Bootstrap the new player with the current game state. The broadcasts from
	// loadGame/resumeGame fire before this player connected (especially in
	// --game/--resume preload mode), so we re-send them here. sessionMiddleware
	// runs before programHandler in the wish middleware chain, so the
	// sendToPlayer call in registerSession is always a no-op — the program
	// doesn't exist in a.programs yet when that runs.
	a.state.RLock()
	phase := a.state.GamePhase
	gameName := a.state.GameName
	a.state.RUnlock()
	if gameName != "" || phase != domain.PhaseNone {
		p := program
		go func() {
			if gameName != "" {
				p.Send(domain.GameLoadedMsg{Name: gameName})
			}
			if phase != domain.PhaseNone {
				p.Send(domain.GamePhaseMsg{Phase: phase})
			}
		}()
	}

	return program
}

func (a *Server) sessionProgramOptions(sess ssh.Session) ([]tea.ProgramOption, colorprofile.Profile) {
	envs := sess.Environ()
	if pty, _, ok := sess.Pty(); ok && pty.Term != "" {
		envs = append(envs, "TERM="+pty.Term)
	}
	// Default to TrueColor if the client didn't send COLORTERM.
	// Most modern terminals (Windows Terminal, iTerm2, etc.) support it,
	// and without this the UI degrades to ugly ANSI-16 approximations.
	hasColorTerm := false
	for _, e := range envs {
		if strings.HasPrefix(e, "COLORTERM=") {
			hasColorTerm = true
			break
		}
	}
	if !hasColorTerm {
		envs = append(envs, "COLORTERM=truecolor")
	}
	cp := colorprofile.Env(envs)
	// DEV_NULL_TERM allows the connecting client to request a specific color
	// depth for this session (used by --local --term on the server/client side).
	for _, e := range envs {
		if strings.HasPrefix(e, "DEV_NULL_TERM=") {
			if p, ok := parseDevNullTerm(strings.TrimPrefix(e, "DEV_NULL_TERM=")); ok {
				cp = p
			}
			break
		}
	}
	slog.Debug("SSH session color profile", "profile", cp.String(), "envs_count", len(envs))
	opts := wishbubbletea.MakeOptions(sess)
	opts = append(opts,
		tea.WithFPS(60),
		tea.WithEnvironment(envs), // override MakeOptions' env to include COLORTERM
		tea.WithColorProfile(cp),
		tea.WithOutput(network.NewKittyStripWriter(sess)),
		tea.WithoutSignalHandler(), // SSH sessions don't need console signal handling;
		// on Windows, signal.Notify installs SetConsoleCtrlHandler which
		// prevents Ebitengine from creating its window in --local mode.
	)
	return opts, cp
}

// parseDevNullTerm maps a DEV_NULL_TERM env value to a colorprofile.Profile.
func parseDevNullTerm(s string) (colorprofile.Profile, bool) {
	switch s {
	case "truecolor":
		return colorprofile.TrueColor, true
	case "256color":
		return colorprofile.ANSI256, true
	case "ansi":
		return colorprofile.ANSI, true
	case "ascii", "none", "no-color":
		return colorprofile.ASCII, true
	}
	return 0, false
}

var romanNumerals = []string{
	"", "", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X",
	"XI", "XII", "XIII", "XIV", "XV", "XVI", "XVII", "XVIII", "XIX", "XX",
}

// --- Player registration and lifecycle ---

func (a *Server) registerSession(sess ssh.Session) *domain.Player {
	// Enforce connection limit.
	if a.maxPlayers > 0 {
		a.programsMu.Lock()
		count := len(a.programs)
		a.programsMu.Unlock()
		if count >= a.maxPlayers {
			slog.Warn("connection rejected: server full", "max", a.maxPlayers)
			fmt.Fprintf(sess, "Server full (%d/%d players). Try again later.\n", count, a.maxPlayers)
			sess.Close()
			return &domain.Player{ID: sess.Context().SessionID(), Name: "rejected"}
		}
	}

	player := &domain.Player{
		ID:   sess.Context().SessionID(),
		Name: a.uniqueName(sess.User()),
	}

	a.sessionsMu.Lock()
	a.sessions[player.ID] = sess
	a.sessionsMu.Unlock()

	a.state.AddPlayer(player)
	slog.Info(player.Name+" joined.", "player_id", player.ID)

	// Check if this player was disconnected from a running game.
	// Do this BEFORE auto-team-creation so reconnecting players rejoin their
	// existing game team instead of getting a new lobby team.
	rejoined := false
	a.state.Lock()
	if oldID, ok := a.state.GameDisconnected[player.Name]; ok {
		// Replace the old session ID with the new one in GameTeams and lobby Teams.
		a.state.ReplaceGamePlayerID(oldID, player.ID)
		delete(a.state.GameDisconnected, player.Name)
		game := a.state.ActiveGame
		a.state.Unlock()
		rejoined = true
		slog.Info(player.Name+" rejoined the game.", "old_id", oldID, "new_id", player.ID)
		// Refresh the teams cache so JS sees the updated player ID.
		if jrt, ok := game.(*engine.Runtime); ok {
			jrt.SetTeamsCache(a.buildTeamsCache())
		}
	} else {
		a.state.Unlock()
	}

	if !rejoined {
		// Auto-assign to a new solo team (avoids manual team setup for solo play).
		a.state.MovePlayerToTeam(player.ID, a.state.TeamCount())
	}

	a.broadcastMsg(domain.PlayerJoinedMsg{Player: player})

	a.broadcastMsg(domain.TeamUpdatedMsg{})

	// Send the current game phase to the joining player so their model
	// initialises correctly (focus, inActiveGame, etc.) without waiting for
	// a future phase transition.
	a.state.RLock()
	currentPhase := a.state.GamePhase
	a.state.RUnlock()
	a.sendToPlayer(player.ID, domain.GamePhaseMsg{Phase: currentPhase})

	return player
}

func (a *Server) unregisterSession(playerID string) {
	player := a.state.GetPlayer(playerID)
	if player != nil {
		slog.Info(player.Name+" left.", "player_id", playerID)
	}

	// Notify the game if this player was in the active game.
	if a.state.ActiveGame != nil && a.state.IsGamePlayer(playerID) {
		a.state.ActiveGame.OnPlayerLeave(playerID)
		if player != nil {
			a.state.Lock()
			a.state.GameDisconnected[player.Name] = playerID
			a.state.Unlock()
		}
	}

	// Always clean up lobby teams (game teams are a separate snapshot).
	a.state.RemovePlayerFromTeams(playerID)
	a.broadcastMsg(domain.TeamUpdatedMsg{})

	a.state.RemovePlayer(playerID)

	a.programsMu.Lock()
	delete(a.programs, playerID)
	a.programsMu.Unlock()

	a.sessionsMu.Lock()
	delete(a.sessions, playerID)
	a.sessionsMu.Unlock()

	a.broadcastMsg(domain.PlayerLeftMsg{PlayerID: playerID})

}

func (a *Server) kickPlayer(playerID string) error {
	a.sessionsMu.RLock()
	sess := a.sessions[playerID]
	a.sessionsMu.RUnlock()
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	return sess.Close()
}

// sanitizePlayerName strips ANSI escapes, non-printable characters, and
// truncates to MaxPlayerNameLen. Returns a safe display name.
func sanitizePlayerName(raw string) string {
	// Strip ANSI escape sequences (\x1b[...m, \x1b]...\x07, etc.)
	cleaned := ansiEscapeRe.ReplaceAllString(raw, "")
	// Keep only printable ASCII and common Unicode letters/digits.
	var b strings.Builder
	for _, r := range cleaned {
		if r >= ' ' && r != 0x7f { // printable, non-DEL
			b.WriteRune(r)
		}
	}
	name := strings.TrimSpace(b.String())
	if len(name) > domain.MaxPlayerNameLen {
		name = name[:domain.MaxPlayerNameLen]
	}
	return name
}

// ansiEscapeRe matches ANSI escape sequences (CSI, OSC, and simple escapes).
var ansiEscapeRe = regexp.MustCompile(`\x1b(?:\[[0-9;]*[a-zA-Z]|\][^\x07]*\x07|[^[\]])`)

func (a *Server) uniqueName(raw string) string {
	base := sanitizePlayerName(raw)
	if base == "" {
		base = "pilot"
	}
	// Replace spaces with hyphens.
	base = strings.ReplaceAll(base, " ", "-")

	// If this name belongs to a disconnected game player, let them reclaim it.
	a.state.RLock()
	_, isReconnect := a.state.GameDisconnected[base]
	a.state.RUnlock()
	if isReconnect {
		return base
	}

	name := base
	index := 2
	for a.state.PlayerByName(name) != nil {
		if index < len(romanNumerals) {
			name = fmt.Sprintf("%s-%s", base, romanNumerals[index])
		} else {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		index++
	}
	return name
}
