// dev-null-client is a graphical SSH client for dev-null servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
//
// Use --no-gui for terminal mode: local game rendering output as ANSI to
// the current terminal, no graphical window. This gives a retro terminal vibe
// while still running game logic client-side for low latency.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"
	"github.com/hajimehoshi/ebiten/v2"

	"dev-null/internal/client"
	"dev-null/internal/datadir"
	"dev-null/internal/display"
	"dev-null/internal/engine"
	"dev-null/internal/server"
)

// buildCommit, buildDate, and buildRemote are injected at build time via -ldflags.
var buildCommit = "dev"
var buildDate = "unknown"
var buildRemote = ""

func main() {
	fmt.Printf("dev-null-client %s (%s)\n", buildCommit, buildDate)
	engine.SetBuildInfo(buildDate, buildRemote)
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	player := flag.String("player", defaultPlayer(), "player name")
	noGUI := flag.Bool("no-gui", false, "run in terminal mode (TUI) instead of opening a graphical window")
	localMode := flag.Bool("local", false, "start a headless server and connect to it")
	noSSH := flag.Bool("no-ssh", false, "skip SSH transport; connect chrome directly (requires --local, for testing)")
	address := flag.String("address", ":23234", "SSH listen address (local mode)")
	dataDir := flag.String("data-dir", datadir.DefaultDataDir(), "data directory containing games/ (local mode)")
	gameName := flag.String("game", "", "game to preload (local mode)")
	resumeName := flag.String("resume", "", "game/save to resume, e.g. orbits/autosave (local mode)")
	tickInterval := flag.Duration("tick-interval", 100*time.Millisecond, "server tick interval (local mode)")
	password := flag.String("password", "", "admin password (authenticates as admin on connect)")
	termFlag := flag.String("term", "", "force terminal color profile: truecolor, 256color, ansi, ascii")
	flag.Parse()

	if *noSSH && !*localMode {
		fmt.Fprintf(os.Stderr, "--no-ssh requires --local\n")
		os.Exit(1)
	}

	// Bootstrap bundled assets for local mode.
	if *localMode && *dataDir == datadir.DefaultDataDir() {
		if err := datadir.Bootstrap(datadir.InstallDir(), *dataDir, buildCommit); err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap error: %v\n", err)
			os.Exit(1)
		}
	}

	// --local --no-ssh: direct transport, no SSH session.
	if *localMode && *noSSH {
		runDirect(*address, *dataDir, *player, *tickInterval, *gameName, *resumeName, *termFlag, *noGUI, *password)
		return
	}

	// --- All SSH paths: local and non-local, GUI and TUI ---
	// Establish the SSH connection, then run the appropriate renderer.
	// For --local, start a headless server first (in a background goroutine).

	var (
		conn    *client.SSHConn
		cleanup func()
	)

	// TUI paths: SSH dial can happen on the main goroutine (no Ebitengine).
	if *noGUI {
		if *localMode {
			var err error
			conn, cleanup, err = dialLocal(*address, *dataDir, *player, *port, *tickInterval, *gameName, *resumeName, *termFlag, true, *password)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("Connecting to %s:%d as %s...\n", *host, *port, *player)
			ptyW, ptyH, _ := xterm.GetSize(os.Stdin.Fd())
			var err error
			conn, err = client.Dial(*host, *port, *player, true, *termFlag, *password, ptyW, ptyH, nil)
			if err != nil {
				log.Fatalf("Failed to connect: %v", err)
			}
			cleanup = func() {}
		}
		defer conn.Close()
		defer cleanup()

		profile := detectClientProfile(*termFlag)
		if err := client.RunTerminal(conn, *player, profile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// --- GUI path ---
	// Ebitengine locks the main goroutine to OS thread 0. ALL work (SSH dial,
	// game creation, etc.) must happen off this thread — only RunGame may run
	// on it. Otherwise the SSH library's goroutines inherit thread 0 and block
	// the Win32 message loop, preventing Draw() from ever being called.

	gameCh := make(chan gameResult, 1)

	go func() {
		if *localMode {
			c, cl, err := dialLocal(*address, *dataDir, *player, *port, *tickInterval, *gameName, *resumeName, *termFlag, false, *password)
			if err != nil {
				gameCh <- gameResult{err: err}
				return
			}
			dd := *dataDir
			g := client.NewClientRenderer(c, 1200, 800, *player, dd)
			gameCh <- gameResult{game: g, cleanup: func() { c.Close(); cl() }}
		} else {
			fmt.Printf("Connecting to %s:%d as %s...\n", *host, *port, *player)
			c, err := client.Dial(*host, *port, *player, false, *termFlag, *password, 0, 0, nil)
			if err != nil {
				gameCh <- gameResult{err: err}
				return
			}
			g := client.NewClientRenderer(c, 1200, 800, *player, datadir.DefaultDataDir())
			gameCh <- gameResult{game: g, cleanup: func() { c.Close() }}
		}
	}()

	fmt.Println("Connecting...")

	// Run the window with a lazy renderer that connects in the background.
	title := "dev-null"
	if *localMode {
		title = "dev-null (local)"
	}
	wrapper := &lazyRenderer{gameCh: gameCh}
	if err := display.RunWindow(wrapper, title, 1200, 800); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if wrapper.cleanup != nil {
		wrapper.cleanup()
	}
}

// dialLocal starts a headless server in a background goroutine and dials it.
// Returns the SSH connection and a cleanup function that shuts down the server.
type gameResult struct {
	game    *client.ClientRenderer
	cleanup func()
	err     error
}

// lazyRenderer wraps a ClientRenderer that connects in the background.
// It shows a blank screen until the connection is ready, then delegates.
type lazyRenderer struct {
	gameCh  <-chan gameResult
	real    *client.ClientRenderer
	cleanup func()
}

func (l *lazyRenderer) HandleInput(w *display.Window) {
	if l.real != nil {
		l.real.HandleInput(w)
	}
}

func (l *lazyRenderer) Draw(w *display.Window, screen *ebiten.Image) {
	if l.real != nil {
		l.real.Draw(w, screen)
	}
}

func (l *lazyRenderer) Resize(cols, rows int) {
	if l.real != nil {
		l.real.Resize(cols, rows)
	}
}

func (l *lazyRenderer) ShouldClose() bool {
	if l.real == nil {
		// Check if connection arrived.
		select {
		case res := <-l.gameCh:
			if res.err != nil {
				fmt.Fprintf(os.Stderr, "Connection failed: %v\n", res.err)
				return true
			}
			l.real = res.game
			l.cleanup = res.cleanup
			fmt.Fprintln(os.Stderr, "Connected. Starting renderer...")
		default:
		}
		return false
	}
	return l.real.ShouldClose()
}

func dialLocal(address, dataDir, playerName string, port int, tickInterval time.Duration, gameName, resumeName, termFlag string, noGUI bool, password string) (*client.SSHConn, func(), error) {
	sshPort := port
	if idx := strings.LastIndex(address, ":"); idx >= 0 {
		if p := address[idx+1:]; p != "" {
			fmt.Sscanf(p, "%d", &sshPort)
		}
	}

	// Convert --game/--resume to init commands sent over the SSH session.
	var initCmds []string
	if resumeName != "" {
		initCmds = append(initCmds, "/game-resume "+resumeName)
	} else if gameName != "" {
		initCmds = append(initCmds, "/game-load "+gameName)
	}

	// Start headless server entirely in a background goroutine.
	serverCtx, serverCancel := context.WithCancel(context.Background())

	ready := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		app, err := server.New(address, password, dataDir, tickInterval)
		if err != nil {
			serverErr <- err
			return
		}
		app.SetLocalPlayerName(playerName)
		app.SetShutdownFunc(serverCancel)
		serverErr <- app.StartWithReady(serverCtx, ready)
	}()

	select {
	case <-ready:
	case err := <-serverErr:
		serverCancel()
		return nil, nil, fmt.Errorf("server failed to start: %w", err)
	}

	// Connect as a normal client.
	ptyW, ptyH := 0, 0
	if noGUI {
		ptyW, ptyH, _ = xterm.GetSize(os.Stdout.Fd())
	}
	conn, err := client.Dial("127.0.0.1", sshPort, playerName, noGUI, termFlag, password, ptyW, ptyH, initCmds)
	if err != nil {
		serverCancel()
		return nil, nil, fmt.Errorf("local SSH dial: %w", err)
	}

	return conn, serverCancel, nil
}

// runDirect runs the --no-ssh path: server + chrome connected directly,
// no SSH transport. Useful for isolating rendering issues from transport.
func runDirect(address, dataDir, playerName string, tickInterval time.Duration, gameName, resumeName, termFlag string, noGUI bool, password string) {
	app, err := server.New(address, password, dataDir, tickInterval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating server: %v\n", err)
		os.Exit(1)
	}
	if resumeName != "" {
		parts := strings.SplitN(resumeName, "/", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "--resume requires game/save format, e.g. orbits/autosave\n")
			os.Exit(1)
		}
		if err := app.PreloadResume(parts[0], parts[1]); err != nil {
			fmt.Fprintf(os.Stderr, "resume %s: %v\n", resumeName, err)
			os.Exit(1)
		}
	} else if gameName != "" {
		if err := app.PreloadGame(gameName); err != nil {
			fmt.Fprintf(os.Stderr, "load game %s: %v\n", gameName, err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := app.RunDirect(ctx, playerName, termFlag, noGUI); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// detectClientProfile returns the color profile for client-side terminal rendering.
func detectClientProfile(termFlag string) colorprofile.Profile {
	if termFlag != "" {
		switch strings.ToLower(termFlag) {
		case "truecolor", "24bit":
			return colorprofile.TrueColor
		case "256color", "256":
			return colorprofile.ANSI256
		case "ansi", "16color", "16":
			return colorprofile.ANSI
		case "ascii", "none", "no-color":
			return colorprofile.ASCII
		default:
			fmt.Fprintf(os.Stderr, "unknown --term value %q (valid: truecolor, 256color, ansi, ascii)\n", termFlag)
		}
	}
	return colorprofile.Detect(os.Stderr, os.Environ())
}

func defaultPlayer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "player"
}
