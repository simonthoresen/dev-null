package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"null-space/common"
	"null-space/internal/engine"
	"null-space/internal/network"
	"null-space/internal/state"
)

// NewLocal creates a Server for local (non-SSH) use: a single player on the
// local terminal. No SSH server, no networking, no host key file.
func NewLocal(dataDir string) *Server {
	app := &Server{
		state:    state.New(""),
		registry: newCommandRegistry(),
		dataDir:  dataDir,
		programs: make(map[string]*tea.Program),
		// sessions left nil — SSH middleware never runs in local mode;
		// map reads against a nil map are safe (return zero value).
		logCh:  make(chan string, 256),
		slogCh: make(chan slogLine, 256),
		chatCh: make(chan common.Message, 256),
	}
	app.registerBuiltins()
	engine.LoadFigletFonts(dataDir)
	return app
}

// RunLocal registers a local player (as admin), optionally pre-loads a game
// and plugins, then runs the full client TUI on stdin/stdout. This is the
// entry point for both the local single-player mode and the render test-bed.
func (a *Server) RunLocal(ctx context.Context, playerName, gameName string) error {
	const playerID = "local"

	player := &common.Player{
		ID:      playerID,
		Name:    playerName,
		IsAdmin: true,
	}
	a.state.AddPlayer(player)

	if gameName != "" {
		var path string
		if network.IsURL(gameName) {
			path = gameName
		} else {
			path = filepath.Join(a.dataDir, "games", gameName+".js")
		}
		if err := a.loadGame(path); err != nil {
			return fmt.Errorf("load game %s: %w", gameName, err)
		}
	}

	model := newChromeModel(a, playerID)
	model.isLocal = true

	// Load init commands from ~/.null-space/client.txt if it exists.
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".null-space", "client.txt")); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					model.initCommands = append(model.initCommands, line)
				}
			}
		}
	}
	program := tea.NewProgram(
		model,
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
