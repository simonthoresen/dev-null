// dev-null-client is a graphical SSH client for dev-null servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"strings"

	"dev-null/internal/bootstep"
	"dev-null/internal/client"
	"dev-null/internal/datadir"
	"dev-null/internal/display"
	"dev-null/internal/engine"
)

//go:embed winres/icon.ico
var appIcon []byte

// buildCommit, buildDate, and buildRemote are injected at build time via -ldflags.
var buildCommit = "dev"
var buildDate = "unknown"
var buildRemote = ""

func main() {
	engine.SetBuildInfo(buildDate, buildRemote)
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	player := flag.String("player", defaultPlayer(), "player name")
	dataDirFlag := flag.String("data-dir", datadir.DefaultDataDir(), "data directory (SoundFonts, etc.)")
	gameName := flag.String("game", "", "game to load on connect (sends /game-load command)")
	resumeName := flag.String("resume", "", "game/save to resume on connect, e.g. orbits/autosave (sends /game-resume command)")
	password := flag.String("password", "", "admin password (authenticates as admin on connect)")
	termFlag := flag.String("term", "", "force terminal color profile: truecolor, 256color, ansi, ascii")
	flag.Parse()

	// Build init commands from flags.
	var initCommands []string
	if *resumeName != "" {
		if !strings.Contains(*resumeName, "/") {
			fmt.Fprintf(os.Stderr, "--resume requires game/save format, e.g. orbits/autosave\n")
			os.Exit(1)
		}
		initCommands = append(initCommands, "/game-resume "+*resumeName)
	} else if *gameName != "" {
		initCommands = append(initCommands, "/game-load "+*gameName)
	}

	bootstep.Init(*termFlag)

	// Init font before dialing so CellW/CellH are set to their real values.
	// This lets us request the correct PTY size from the very first frame,
	// avoiding a size mismatch between the initial server render and the window.
	const winW, winH = 1200, 800
	display.InitGUIFont()
	ptyW := display.WindowCols(winW)
	ptyH := display.WindowRows(winH)

	bootstep.Start(fmt.Sprintf("Connecting to %s:%d as %s", *host, *port, *player))
	conn, err := client.Dial(*host, *port, *player, *termFlag, *password, ptyW, ptyH, initCommands)
	if err != nil {
		bootstep.Finish("FAIL")
		log.Fatalf("Failed to connect: %v", err)
	}
	bootstep.Finish("DONE")
	defer conn.Close()

	bootstep.Start("Starting renderer")
	renderer := client.NewClientRenderer(conn, winW, winH, *player, datadir.InstallDir(), *dataDirFlag)
	bootstep.Finish("DONE")

	if err := display.RunWindow(renderer, "dev-null", winW, winH, appIcon); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func defaultPlayer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "player"
}
