package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"null-space/common"
)

// NewLocal creates a Server for local (non-SSH) use: a single player on the
// local terminal. No SSH server, no networking, no host key file.
func NewLocal(dataDir string) *Server {
	app := &Server{
		state:    newState(""),
		registry: newCommandRegistry(),
		dataDir:  dataDir,
		programs: make(map[string]*tea.Program),
		// sessions left nil — SSH middleware never runs in local mode;
		// map reads against a nil map are safe (return zero value).
		logCh:  make(chan string, 256),
		chatCh: make(chan common.Message, 256),
	}
	app.registerBuiltins()
	return app
}

// RunLocal registers a local player (as admin), optionally pre-loads a game
// and plugins, then runs the full client TUI on stdin/stdout. This is the
// entry point for both the local single-player mode and the render test-bed.
func (a *Server) RunLocal(ctx context.Context, playerName, gameName string, pluginNames []string) error {
	const playerID = "local"

	player := &common.Player{
		ID:      playerID,
		Name:    playerName,
		IsAdmin: true,
	}
	a.state.AddPlayer(player)
	a.state.EnsurePlayerTeam(playerID)

	for _, name := range pluginNames {
		var path string
		if isURL(name) {
			path = name
		} else {
			path = filepath.Join(a.dataDir, "plugins", name+".js")
		}
		if err := a.loadPlugin(name, path); err != nil {
			return fmt.Errorf("load plugin %s: %w", name, err)
		}
	}

	if gameName != "" {
		var path string
		if isURL(gameName) {
			path = gameName
		} else {
			path = filepath.Join(a.dataDir, "games", gameName+".js")
		}
		if err := a.loadGame(path); err != nil {
			return fmt.Errorf("load game %s: %w", gameName, err)
		}
	}

	program := tea.NewProgram(
		newChromeModel(a, playerID),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
		tea.WithFPS(60),
		tea.WithColorProfile(colorprofile.Env(os.Environ())),
	)

	a.programsMu.Lock()
	a.programs[playerID] = program
	a.programsMu.Unlock()

	go a.runTicker(ctx)

	go func() {
		<-ctx.Done()
		program.Quit()
	}()

	_, err := program.Run()
	return err
}
