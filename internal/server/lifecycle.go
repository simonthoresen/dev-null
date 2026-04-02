package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"null-space/internal/domain"
	"null-space/internal/engine"
	"null-space/internal/network"
	"null-space/internal/state"
)

// checkGameOver detects if the JS runtime signaled game over and initiates the transition.
func (a *Server) checkGameOver() {
	a.state.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	a.state.RUnlock()

	if game == nil || phase != domain.PhasePlaying {
		return
	}
	rt, ok := game.(*engine.JSRuntime)
	if !ok || !rt.IsGameOverPending() {
		return
	}

	// Save state if the game passed one as the second arg to gameOver().
	gameOverState := rt.GameOverStateExport()

	a.state.RLock()
	gameName := a.state.GameName
	a.state.RUnlock()

	if gameOverState != nil {
		if err := state.SaveGameState(a.dataDir, gameName, gameOverState); err != nil {
			a.serverLog(fmt.Sprintf("warning: could not save game state: %v", err))
		} else {
			a.serverLog(fmt.Sprintf("game state saved: %s", gameName))
		}
	}

	a.state.SetGamePhase(domain.PhaseGameOver)
	a.state.Lock()
	a.state.GameOverResults = rt.GameOverResults()
	a.state.Unlock()

	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseGameOver})
	a.broadcastChat(domain.Message{Text: "Game over!"})
	a.serverLog("game over — waiting for players to acknowledge")

	// Start 15s timeout for game-over acknowledgment.
	a.gameOverTimer = make(chan struct{})
	go a.gameOverTimeout()
}

func (a *Server) gameOverTimeout() {
	select {
	case <-time.After(15 * time.Second):
	case <-a.gameOverTimer:
	}
	// Only unload if still in game-over phase.
	if a.state.GetGamePhase() == domain.PhaseGameOver {
		a.unloadGame()
	}
}

// AcknowledgeGameOver marks a player as ready and ends game-over if all are ready.
func (a *Server) AcknowledgeGameOver(playerID string) {
	a.state.MarkPlayerReady(playerID)
	if a.state.AllPlayersReady() {
		select {
		case <-a.gameOverTimer:
		default:
			close(a.gameOverTimer)
		}
	}
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

	// Derive game name: for folder games (games/nethack/main.js) use the folder name,
	// for flat games (games/example.js) use the filename stem.
	name := strings.TrimSuffix(filepath.Base(path), ".js")
	if name == "main" {
		name = filepath.Base(filepath.Dir(path))
	}

	// Create a buffered channel for JS→server chat; drained by a goroutine below.
	gameChatCh := make(chan domain.Message, 64)

	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return err
	}
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetShowDialogFn(a.ShowDialog)
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
	a.state.GamePhase = domain.PhaseSplash
	a.state.Unlock()

	// Populate the teams cache so JS teams() returns correct data.
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat messages on a background goroutine.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call init — teams() now returns game participants via cached snapshot.
	savedState, err := state.LoadGameState(a.dataDir, name)
	if err != nil {
		a.serverLog(fmt.Sprintf("warning: could not load saved state: %v", err))
	}
	rt.Init(savedState)

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.broadcastMsg(domain.GameLoadedMsg{Name: name})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseSplash})
	a.broadcastChat(domain.Message{Text: fmt.Sprintf("Game loaded: %s", name)})
	a.serverLog(fmt.Sprintf("game loaded: %s (splash)", name))

	// Start splash goroutine: waits up to 10s or until admin triggers start.
	a.splashDone = make(chan struct{})
	go a.splashTimer()

	return nil
}

func (a *Server) splashTimer() {
	select {
	case <-time.After(10 * time.Second):
	case <-a.splashDone:
	}
	// Only transition if still in splash phase.
	a.state.Lock()
	if a.state.GamePhase != domain.PhaseSplash {
		a.state.Unlock()
		return
	}

	a.state.GamePhase = domain.PhasePlaying
	game := a.state.ActiveGame
	a.state.Unlock()

	a.lastUpdate = a.clock.Now()
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhasePlaying})
	a.serverLog("game started (playing)")

	if game != nil {
		game.Start()
	}
}

// StartGame is called when an admin acknowledges the splash screen.
func (a *Server) StartGame() {
	select {
	case <-a.splashDone:
		// already closed
	default:
		close(a.splashDone)
	}
}

func (a *Server) unloadGame() {
	// Cancel any pending splash or game-over timers.
	if a.splashDone != nil {
		select {
		case <-a.splashDone:
		default:
			close(a.splashDone)
		}
	}
	if a.gameOverTimer != nil {
		select {
		case <-a.gameOverTimer:
		default:
			close(a.gameOverTimer)
		}
	}

	a.state.Lock()
	game := a.state.ActiveGame
	if game == nil {
		a.state.Unlock()
		return // already unloaded
	}
	a.state.ActiveGame = nil
	a.state.GameName = ""
	a.state.GamePhase = domain.PhaseNone
	a.state.GameOverReady = nil
	a.state.Unlock()

	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}
	game.Unload()

	// Close the JS chat channel so the drainer goroutine exits.
	if jrt, ok := game.(*engine.JSRuntime); ok {
		jrt.CloseChatCh()
	}

	a.broadcastMsg(domain.GameUnloadedMsg{})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseNone})
	a.broadcastChat(domain.Message{Text: "Game unloaded."})
	a.serverLog("game unloaded")
}

// buildTeamsCache builds a pre-resolved teams snapshot for JS teams().
// Resolves player IDs to names so JSRuntime doesn't need CentralState access.
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

// suspendGame suspends the active game, persisting its session state.
func (a *Server) suspendGame(saveName string) error {
	a.state.RLock()
	game := a.state.ActiveGame
	phase := a.state.GamePhase
	gameName := a.state.GameName
	a.state.RUnlock()

	if game == nil || phase != domain.PhasePlaying {
		return fmt.Errorf("no game is currently playing")
	}

	// Read Game.state directly — no special suspend hook needed.
	sessionState := game.State()

	// Build the save.
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

	// Unregister game commands.
	for _, cmd := range game.Commands() {
		a.registry.Unregister(cmd.Name)
	}

	// Transition to suspended phase — runtime stays alive.
	a.state.Lock()
	a.state.GamePhase = domain.PhaseSuspended
	a.state.Unlock()

	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhaseSuspended})
	a.broadcastMsg(domain.GameSuspendedMsg{Name: gameName})
	a.broadcastChat(domain.Message{Text: fmt.Sprintf("Game suspended: %s (save: %s)", gameName, saveName)})
	a.serverLog(fmt.Sprintf("game suspended: %s/%s", gameName, saveName))
	return nil
}

// resumeGame resumes a suspended game. If the runtime is still alive (warm),
// calls Resume(nil). If loading from disk (cold), loads the game JS fresh,
// calls init(globalState)+start(), then Resume(sessionState).
func (a *Server) resumeGame(gameName, saveName string) error {
	save, err := state.LoadSuspend(a.dataDir, gameName, saveName)
	if err != nil {
		return fmt.Errorf("load suspend save: %w", err)
	}

	// Validate team count against the lobby teams if this is a cold resume
	// (warm resume keeps the original game teams).
	a.state.RLock()
	currentGame := a.state.ActiveGame
	currentName := a.state.GameName
	currentPhase := a.state.GamePhase
	a.state.RUnlock()

	isWarm := currentGame != nil && currentName == gameName && currentPhase == domain.PhaseSuspended

	if isWarm {
		// Warm resume — runtime is alive, Game.state is intact. Just unpause.
		// Re-register game commands.
		for _, cmd := range currentGame.Commands() {
			a.registry.Register(cmd)
		}

		a.state.Lock()
		a.state.GamePhase = domain.PhasePlaying
		a.state.Unlock()

		a.lastUpdate = a.clock.Now()
		a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhasePlaying})
		a.broadcastMsg(domain.GameResumedMsg{Name: gameName})
		a.broadcastChat(domain.Message{Text: fmt.Sprintf("Game resumed: %s", gameName)})
		a.serverLog(fmt.Sprintf("game resumed (warm): %s/%s", gameName, saveName))
		return nil
	}

	// Cold resume — load the game fresh and restore from save.
	if currentGame != nil {
		a.unloadGame()
	}

	// Validate team count.
	tr := domain.TeamRange{} // will be updated after loading
	_ = tr

	gamesDir := filepath.Join(a.dataDir, "games")
	path := engine.ResolveGamePath(gamesDir, gameName)

	gameChatCh := make(chan domain.Message, 64)
	rt, err := engine.LoadGame(path, a.serverLog, gameChatCh, a.clock, a.dataDir)
	if err != nil {
		close(gameChatCh)
		return fmt.Errorf("load game for resume: %w", err)
	}
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetShowDialogFn(a.ShowDialog)
	}

	// Validate team count against game's declared range.
	tr = rt.TeamRange()
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
	a.state.GamePhase = domain.PhasePlaying // skip splash
	a.state.Unlock()

	// Populate teams cache.
	if jrt, ok := rt.(*engine.JSRuntime); ok {
		jrt.SetTeamsCache(a.buildTeamsCache())
	}

	// Drain JS chat.
	go func() {
		for msg := range gameChatCh {
			a.broadcastChat(msg)
		}
	}()

	// Call init with global saved state (high scores etc), then start.
	globalState, err := state.LoadGameState(a.dataDir, gameName)
	if err != nil {
		a.serverLog(fmt.Sprintf("warning: could not load global state: %v", err))
	}
	rt.Init(globalState)
	rt.Start()

	// Restore Game.state from the suspend save.
	if save.GameState != nil {
		rt.SetState(save.GameState)
	}

	// Register game commands.
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	a.lastUpdate = a.clock.Now()
	a.broadcastMsg(domain.GameLoadedMsg{Name: gameName})
	a.broadcastMsg(domain.GamePhaseMsg{Phase: domain.PhasePlaying})
	a.broadcastMsg(domain.GameResumedMsg{Name: gameName})
	a.broadcastChat(domain.Message{Text: fmt.Sprintf("Game resumed: %s (from save: %s)", gameName, saveName)})
	a.serverLog(fmt.Sprintf("game resumed (cold): %s/%s", gameName, saveName))

	// Clean up the suspend save after successful resume.
	if err := state.DeleteSuspend(a.dataDir, gameName, saveName); err != nil {
		a.serverLog(fmt.Sprintf("warning: could not delete suspend save: %v", err))
	}

	return nil
}

// SuspendGame exposes suspendGame to the chrome/command layer.
func (a *Server) SuspendGame(saveName string) error { return a.suspendGame(saveName) }

// ResumeGame exposes resumeGame to the chrome/command layer.
func (a *Server) ResumeGame(gameName, saveName string) error { return a.resumeGame(gameName, saveName) }

// ListSuspends returns all suspend saves for the resume menu.
func (a *Server) ListSuspends() []state.SuspendInfo { return state.ListSuspends(a.dataDir, "") }
