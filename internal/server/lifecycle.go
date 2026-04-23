package server

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/network"
	"dev-null/internal/state"
)

// checkGameOver detects if the JS runtime signaled game-over, posts the
// results to chat as a system message, and unloads the game so everyone
// returns to the lobby. There is no ending phase or dedicated screen —
// the results stay in chat history (scrollable via PgUp/PgDn) so
// players can read and discuss them after the game ends.
func (a *Server) checkGameOver() {
	a.state.RLock()
	game := a.state.ActiveGame
	gameName := a.state.GameName
	phase := a.state.GamePhase
	a.state.RUnlock()

	if game == nil || phase != domain.PhasePlaying {
		return
	}
	srt, ok := game.(engine.ScriptRuntime)
	if !ok || !srt.IsGameOverPending() {
		return
	}

	game.End()
	results := srt.GameOverResults()

	a.broadcastChat(domain.Message{Text: formatGameOverChat(gameName, results)})
	slog.Info("Game over, posted results to chat and unloading.")
	a.unloadGame()
}

// formatGameOverChat renders the game-over banner + ranked results as a
// multi-line string, suitable for a single system chat message. Each
// "\n" becomes a separate chat line on the client, so the result is a
// compact scoreboard in the chat panel.
func formatGameOverChat(gameName string, results []domain.GameResult) string {
	var b strings.Builder
	title := "Game Over"
	if gameName != "" {
		title += ": " + gameName
	}
	b.WriteString(title)
	if len(results) > 0 {
		maxName := 0
		for _, r := range results {
			if len(r.Name) > maxName {
				maxName = len(r.Name)
			}
		}
		for i, r := range results {
			b.WriteString(fmt.Sprintf("\n  %d. %-*s  %s", i+1, maxName, r.Name, r.Result))
		}
	}
	return b.String()
}

func (a *Server) loadGame(path string) error {
	if network.IsURL(path) {
		if network.IsZipURL(path) {
			gamesDir := filepath.Join(a.dataDir, "games")
			local, err := network.DownloadAndExtractZip(path, gamesDir)
			if err != nil {
				return fmt.Errorf("download game zip: %w", err)
			}
			path = local
		} else {
			cacheDir := filepath.Join(a.dataDir, "games", ".cache")
			local, err := network.DownloadToCache(path, cacheDir)
			if err != nil {
				return fmt.Errorf("download game: %w", err)
			}
			path = local
		}
	}
	if a.state.ActiveGame != nil {
		a.unloadGame()
	}

	// Derive game name: for folder games use the folder name,
	// for flat games use the filename stem (strip .js or .lua).
	name := engine.TrimScriptExt(filepath.Base(path))
	if name == "main" {
		name = filepath.Base(filepath.Dir(path))
	}
	if err := state.ValidateName(name); err != nil {
		return fmt.Errorf("invalid game name %q: %w", name, err)
	}

	// Create a buffered channel for JS→server chat; drained by a goroutine below.
	gameChatCh := make(chan domain.Message, 64)

	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return err
	}
	if srt, ok := rt.(engine.ScriptRuntime); ok {
		srt.SetShowDialogFn(a.ShowDialog)
	}

	// Validate team count against game's declared range.
	teams := a.state.GetTeams()
	tr := rt.TeamRange()
	teamCount := len(teams)
	if tr.Min > 0 && teamCount < tr.Min {
		close(gameChatCh)
		return fmt.Errorf("game requires at least %d teams (have %d)", tr.Min, teamCount)
	}
	if tr.Max > 0 && teamCount > tr.Max {
		close(gameChatCh)
		return fmt.Errorf("game supports at most %d teams (have %d)", tr.Max, teamCount)
	}

	a.state.Lock()
	// Snapshot teams for the game — lobby teams stay independent.
	a.state.GameTeams = teams
	a.state.GameDisconnected = make(map[string]string)
	a.state.ActiveGame = rt
	a.state.GameName = name
	a.state.GamePhase = domain.PhaseStarting
	a.state.StartingReady = make(map[string]bool)
	a.state.StartingStart = a.clock.Now()
	a.state.Unlock()

	// Populate the teams cache so script teams() returns correct data.
	if srt, ok := rt.(engine.ScriptRuntime); ok {
		srt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat messages on a background goroutine.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call Load — teams() now returns game participants via cached snapshot.
	savedState, err := state.LoadGameState(a.dataDir, name)
	if err != nil {
		slog.Warn("could not load saved state", "error", err)
	}
	rt.Load(savedState)

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.broadcastMsg(domain.GameLoadedMsg{Name: name})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseStarting})
	slog.Info(fmt.Sprintf("Game loaded: %s", name), "path", path)

	// Start starting goroutine: waits up to 10s or until admin triggers start.
	a.startingDone = make(chan struct{})
	go a.startingTimer()

	return nil
}

func (a *Server) startingTimer() {
	select {
	case <-time.After(10 * time.Second):
	case <-a.startingDone:
	}
	// Only transition if still in starting phase.
	a.state.Lock()
	if a.state.GamePhase != domain.PhaseStarting {
		a.state.Unlock()
		return
	}

	a.state.GamePhase = domain.PhasePlaying
	game := a.state.ActiveGame

	// Collect current game players while holding the lock so we can call
	// OnPlayerJoin after releasing it (Runtime must not acquire state.mu).
	type playerEntry struct{ id, name string }
	var gamePlayers []playerEntry
	for _, t := range a.state.GameTeams {
		for _, id := range t.Players {
			if p := a.state.Players[id]; p != nil {
				gamePlayers = append(gamePlayers, playerEntry{p.ID, p.Name})
			}
		}
	}
	a.state.Unlock()

	a.lastUpdateMu.Lock()
	a.lastUpdate = a.clock.Now()
	a.lastUpdateMu.Unlock()
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhasePlaying})
	slog.Info("Game started!")

	if game != nil {
		game.Begin()
		for _, p := range gamePlayers {
			game.OnPlayerJoin(p.id, p.name)
		}
	}
}

// ReadyUp marks a player as ready on the starting screen.
// If all players are ready, the game starts immediately.
func (a *Server) ReadyUp(playerID string) {
	a.state.MarkStartingReady(playerID)
	a.broadcastMsg(domain.StartingReadyMsg{PlayerID: playerID})
	if a.state.AllPlayersStartingReady() {
		a.StartGame()
	}
}

// StartGame is called when an admin acknowledges the starting screen.
func (a *Server) StartGame() {
	select {
	case <-a.startingDone:
		// already closed
	default:
		close(a.startingDone)
	}
}

func (a *Server) unloadGame() {
	// Cancel any pending starting timer.
	if a.startingDone != nil {
		select {
		case <-a.startingDone:
		default:
			close(a.startingDone)
		}
	}

	a.state.Lock()
	game := a.state.ActiveGame
	gameName := a.state.GameName
	if game == nil {
		a.state.Unlock()
		return // already unloaded
	}
	a.state.ActiveGame = nil
	a.state.GameName = ""
	a.state.GamePhase = domain.PhaseNone
	a.state.StartingReady = nil
	a.state.Unlock()

	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}

	// Unload returns the session state to persist (nil if no state).
	gameState := game.Unload()

	// Close the script chat channel so the drainer goroutine exits.
	if srt, ok := game.(engine.ScriptRuntime); ok {
		srt.CloseChatCh()
	}

	// Save state returned by Unload.
	if gameState != nil && gameName != "" {
		if err := state.SaveGameState(a.dataDir, gameName, gameState); err != nil {
			slog.Warn("could not save game state", "error", err)
		} else {
			a.serverLog(fmt.Sprintf("game state saved: %s", gameName))
		}
	}

	a.broadcastMsg(domain.GameUnloadedMsg{})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseNone})
	slog.Info("Game unloaded.")
}

// buildTeamsCache builds a pre-resolved teams snapshot for JS teams().
// Resolves player IDs to names so the game runtime doesn't need CentralState access.
func (a *Server) buildTeamsCache() []map[string]any {
	teams := a.state.GetGameTeams()
	result := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		playerList := make([]any, 0, len(t.Players))
		for _, pid := range t.Players {
			entry := map[string]any{"id": pid}
			if p := a.state.GetPlayer(pid); p != nil {
				entry["name"] = p.Name
			} else {
				entry["name"] = pid
			}
			playerList = append(playerList, entry)
		}
		result = append(result, map[string]any{
			"name":    t.Name,
			"color":   t.Color,
			"players": playerList,
		})
	}
	return result
}

// suspendGame suspends the active game by unloading the runtime and persisting
// its session state. After suspension the phase returns to PhaseNone; the save
// file on disk signals that the game can be resumed.
func (a *Server) suspendGame(saveName string) error {
	a.state.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	gameName := a.state.GameName
	a.state.RUnlock()

	if game == nil || phase != domain.PhasePlaying {
		return fmt.Errorf("no game is currently playing")
	}

	// Cancel any pending starting timer.
	if a.startingDone != nil {
		select {
		case <-a.startingDone:
		default:
			close(a.startingDone)
		}
	}

	// Copy team state for the save before unloading.
	a.state.RLock()
	teams := make([]domain.Team, len(a.state.GameTeams))
	for i, t := range a.state.GameTeams {
		teams[i] = domain.Team{
			Name:    t.Name,
			Color:   t.Color,
			Players: append([]string(nil), t.Players...),
		}
	}
	disc := make(map[string]string)
	for k, v := range a.state.GameDisconnected {
		disc[k] = v
	}
	a.state.RUnlock()

	// Unregister game commands.
	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}

	// Collect mid-session snapshot (does NOT interrupt the VM).
	sessionState := game.Suspend()

	// Collect persistent state and interrupt the VM.
	persistentState := game.Unload()

	// Close the script chat channel.
	if srt, ok := game.(engine.ScriptRuntime); ok {
		srt.CloseChatCh()
	}

	// Save persistent state (high scores, etc.) alongside the suspend save.
	if persistentState != nil {
		if err := state.SaveGameState(a.dataDir, gameName, persistentState); err != nil {
			slog.Warn("could not save game state on suspend", "error", err)
		}
	}

	// Persist the suspend save (session snapshot + teams).
	save := &state.SuspendSave{
		GameName:     gameName,
		SaveName:     saveName,
		SavedAt:      a.clock.Now(),
		Teams:        teams,
		Disconnected: disc,
		GameState:    sessionState,
	}
	if err := state.SaveSuspend(a.dataDir, save); err != nil {
		return fmt.Errorf("save suspend state: %w", err)
	}

	// Clean up server state — runtime is now dead.
	a.state.Lock()
	a.state.ActiveGame = nil
	a.state.GameName = ""
	a.state.GamePhase = domain.PhaseNone
	a.state.StartingReady = nil
	a.state.Unlock()

	a.broadcastMsg(domain.GameUnloadedMsg{})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseNone})
	a.broadcastMsg(domain.GameSuspendedMsg{Name: gameName})
	slog.Info(fmt.Sprintf("Game suspended: %s (save: %s)", gameName, saveName))
	return nil
}

// resumeGame loads a suspended game from the save file and restores session state
// via Load(). Suspend always tears down the runtime, so every resume is a fresh load.
func (a *Server) resumeGame(gameName, saveName string) error {
	save, err := state.LoadSuspend(a.dataDir, gameName, saveName)
	if err != nil {
		return fmt.Errorf("load suspend save: %w", err)
	}

	// Unload any currently running game.
	a.state.RLock()
	currentGame := a.state.ActiveGame
	a.state.RUnlock()
	if currentGame != nil {
		a.unloadGame()
	}

	gamesDir := filepath.Join(a.dataDir, "games")
	path := engine.ResolveGamePath(gamesDir, gameName)

	gameChatCh := make(chan domain.Message, 64)
	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return fmt.Errorf("load game for resume: %w", err)
	}
	if srt, ok := rt.(engine.ScriptRuntime); ok {
		srt.SetShowDialogFn(a.ShowDialog)
	}

	// Validate team count against game's declared range.
	tr := rt.TeamRange()
	teamCount := len(save.Teams)
	if tr.Min > 0 && teamCount < tr.Min {
		close(gameChatCh)
		return fmt.Errorf("saved session has %d teams but game requires at least %d", teamCount, tr.Min)
	}
	if tr.Max > 0 && teamCount > tr.Max {
		close(gameChatCh)
		return fmt.Errorf("saved session has %d teams but game supports at most %d", teamCount, tr.Max)
	}

	// Restore teams from save.
	a.state.Lock()
	a.state.GameTeams = save.Teams
	a.state.GameDisconnected = save.Disconnected
	if a.state.GameDisconnected == nil {
		a.state.GameDisconnected = make(map[string]string)
	}
	a.state.ActiveGame = rt
	a.state.GameName = gameName
	a.state.GamePhase = domain.PhasePlaying // skip starting screen
	a.state.Unlock()

	// Populate teams cache.
	if srt, ok := rt.(engine.ScriptRuntime); ok {
		srt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call Load with the suspend save's session state (which carries both global
	// high scores and the suspended session state).
	// Load persistent state (high scores, etc.) — same as a fresh game load.
	persistentState, err := state.LoadGameState(a.dataDir, gameName)
	if err != nil {
		slog.Warn("could not load game state on resume", "error", err)
	}
	rt.Load(persistentState)

	// Resume with session snapshot instead of Begin (falls back to Begin if hook absent).
	rt.Resume(save.GameState)

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.lastUpdateMu.Lock()
	a.lastUpdate = a.clock.Now()
	a.lastUpdateMu.Unlock()
	a.broadcastMsg(domain.GameLoadedMsg{Name: gameName})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhasePlaying})
	a.broadcastMsg(domain.GameResumedMsg{Name: gameName})
	slog.Info(fmt.Sprintf("Game resumed: %s (from save: %s)", gameName, saveName))

	// Clean up the suspend save after successful resume.
	if err := state.DeleteSuspend(a.dataDir, gameName, saveName); err != nil {
		slog.Warn("could not delete suspend save", "error", err)
	}

	return nil
}

// SuspendGame exposes suspendGame to the chrome/command layer.
func (a *Server) SuspendGame(saveName string) error { return a.suspendGame(saveName) }

// ResumeGame exposes resumeGame to the chrome/command layer.
func (a *Server) ResumeGame(gameName, saveName string) error { return a.resumeGame(gameName, saveName) }

// ListSuspends returns all suspend saves for the resume menu.
func (a *Server) ListSuspends() []state.SuspendInfo { return state.ListSuspends(a.dataDir, "") }
