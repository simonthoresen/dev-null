package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"dev-null/internal/chrome"
	"dev-null/internal/display"
	"dev-null/internal/domain"
	"dev-null/internal/network"
)

// PreloadGame loads a game by name before any players connect.
// The name is resolved to a path under the data directory, or treated as a URL.
func (a *Server) PreloadGame(name string) error {
	var path string
	if network.IsURL(name) {
		path = name
	} else {
		path = filepath.Join(a.dataDir, "games", name+".js")
	}
	return a.loadGame(path)
}

// PreloadResume resumes a saved game before any players connect.
func (a *Server) PreloadResume(gameName, saveName string) error {
	return a.resumeGame(gameName, saveName)
}

// RunDirect runs the server without SSH: the ticker and game engine start
// normally, but a chrome model is connected directly to os.Stdin/os.Stdout
// via Bubble Tea. No SSH transport, no KittyStripWriter, no PTY negotiation.
// Use alongside --local to isolate rendering issues from transport issues.
func (a *Server) RunDirect(ctx context.Context, playerName, termOverride string, noGUI bool, iconICO []byte) error {
	// Initialise lastUpdate so the first tick dt isn't enormous.
	a.lastUpdateMu.Lock()
	a.lastUpdate = time.Now()
	a.lastUpdateMu.Unlock()

	go a.runTicker(ctx)

	// Register the player directly in state (no SSH session).
	playerID := "direct-" + playerName
	player := &domain.Player{ID: playerID, Name: playerName}
	a.state.AddPlayer(player)
	a.state.SetPlayerAdmin(playerID, true)
	defer func() {
		a.state.RemovePlayer(playerID)
		a.programsMu.Lock()
		delete(a.programs, playerID)
		a.programsMu.Unlock()
	}()

	// Detect color profile from environment, then apply termOverride.
	envs := os.Environ()
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
	if termOverride != "" {
		if p, ok := parseDevNullTerm(termOverride); ok {
			cp = p
		}
	}

	model := chrome.NewModel(a, playerID)
	model.ColorProfile = cp
	if !noGUI {
		model.IsEnhancedClient = true // GUI mode renders via Ebitengine — enable canvas/local
	}

	// Deliver the current game state so the model initialises correctly.
	// Send GameLoadedMsg first so inActiveGame is true before the phase arrives.
	deliverGameState := func(sender msgSender) {
		a.state.RLock()
		currentPhase := a.state.GamePhase
		gameName := a.state.GameName
		a.state.RUnlock()
		if gameName != "" {
			sender.Send(domain.GameLoadedMsg{Name: gameName})
		}
		if currentPhase != domain.PhaseNone {
			sender.Send(domain.GamePhaseMsg{Phase: currentPhase})
		}
	}

	if noGUI {
		// TUI mode: run chrome in the terminal via Bubble Tea.
		program := tea.NewProgram(model,
			tea.WithInput(os.Stdin),
			tea.WithOutput(os.Stdout),
			tea.WithFPS(60),
			tea.WithColorProfile(cp),
		)

		a.programsMu.Lock()
		a.programs[playerID] = program
		a.programsMu.Unlock()

		deliverGameState(program)

		defer fmt.Fprint(os.Stdout, "\x1b[?1049l\x1b[?25h")

		_, err := program.Run()
		return err
	}

	// GUI mode: run chrome in an Ebitengine window.
	renderer := display.NewServerRenderer()

	a.programsMu.Lock()
	a.programs[playerID] = renderer
	a.programsMu.Unlock()

	deliverGameState(renderer)

	return renderer.Run(model, "dev-null", 1200, 800, iconICO)
}
