// null-space-client is a graphical SSH client for null-space servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
//
// Use --terminal for terminal mode: local game rendering output as ANSI to
// the current terminal, no graphical window. This gives a retro terminal vibe
// while still running game logic client-side for low latency.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"null-space/internal/client"
	"null-space/internal/server"
)

func main() {
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	player := flag.String("player", defaultPlayer(), "player name")
	terminal := flag.Bool("terminal", false, "terminal mode: render to terminal instead of graphical window")
	localMode := flag.Bool("local", false, "start a headless SSH server and connect the graphical client to it")
	address := flag.String("address", ":23234", "SSH listen address (local mode)")
	dataDir := flag.String("data-dir", ".", "data directory containing games/ (local mode)")
	gameName := flag.String("game", "", "game to preload (local mode)")
	resumeName := flag.String("resume", "", "game/save to resume, e.g. orbits/autosave (local mode)")
	tickInterval := flag.Duration("tick-interval", 100*time.Millisecond, "server tick interval (local mode)")
	termFlag := flag.String("term", "", "force terminal color profile for local-mode sessions: truecolor, 256color, ansi, ascii")
	flag.Parse()

	if *localMode {
		runLocal(*address, *dataDir, *player, *port, *tickInterval, *gameName, *resumeName, *termFlag)
		return
	}

	fmt.Printf("Connecting to %s:%d as %s...\n", *host, *port, *player)

	conn, err := client.Dial(*host, *port, *player, *terminal)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	if *terminal {
		if err := client.RunTerminal(conn, *player); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println("Connected. Starting renderer...")

	fontFace := client.DefaultFontFace()
	game := client.NewGame(conn, fontFace, 1200, 800, *player)

	ebiten.SetWindowSize(1200, 800)
	ebiten.SetWindowTitle("null-space")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runLocal starts a headless SSH server in-process, then connects the
// graphical Ebitengine client to it. This exercises the full network pipeline
// (SSH transport, session middleware, PTY, etc.) in a single process.
func runLocal(address, dataDir, playerName string, port int, tickInterval time.Duration, gameName, resumeName, termFlag string) {
	app, err := server.New(address, "", dataDir, tickInterval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating server: %v\n", err)
		os.Exit(1)
	}
	if termFlag != "" {
		app.SetTermOverride(parseTermFlag(termFlag))
	}
	app.InstallConsoleSlogHandler()

	// Preload or resume a game before the client connects.
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Parse port from address.
	sshPort := port
	if idx := strings.LastIndex(address, ":"); idx >= 0 {
		if p := address[idx+1:]; p != "" {
			fmt.Sscanf(p, "%d", &sshPort)
		}
	}

	// Start SSH server and wait for it to be ready.
	ready := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.StartWithReady(ctx, ready)
	}()

	select {
	case <-ready:
		// Server is listening.
	case err := <-serverErr:
		fmt.Fprintf(os.Stderr, "server failed to start: %v\n", err)
		os.Exit(1)
	case <-ctx.Done():
		return
	}

	// Connect via SSH using the full client stack (graphical mode).
	conn, err := client.Dial("127.0.0.1", sshPort, playerName, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "local SSH dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Run the graphical client.
	fontFace := client.DefaultFontFace()
	game := client.NewGame(conn, fontFace, 1200, 800, playerName)

	ebiten.SetWindowSize(1200, 800)
	ebiten.SetWindowTitle("null-space (local)")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	stop()
}

// parseTermFlag maps a --term string to a colorprofile.Profile int value.
// Returns -1 (auto-detect) for unknown values.
func parseTermFlag(s string) int {
	switch strings.ToLower(s) {
	case "truecolor", "24bit":
		return 0 // colorprofile.TrueColor
	case "256color", "256":
		return 1 // colorprofile.ANSI256
	case "ansi", "16color", "16":
		return 2 // colorprofile.ANSI
	case "ascii", "none", "no-color":
		return 3 // colorprofile.ASCII
	default:
		fmt.Fprintf(os.Stderr, "unknown --term value %q (valid: truecolor, 256color, ansi, ascii)\n", s)
		return -1
	}
}

func defaultPlayer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "player"
}
