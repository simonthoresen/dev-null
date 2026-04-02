// null-space-client is a graphical SSH client for null-space servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"

	"github.com/hajimehoshi/ebiten/v2"

	"null-space/internal/client"
)

func main() {
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	player := flag.String("player", defaultPlayer(), "player name")
	flag.Parse()

	fmt.Printf("Connecting to %s:%d as %s...\n", *host, *port, *player)

	conn, err := client.Dial(*host, *port, *player)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

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

func defaultPlayer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "player"
}
