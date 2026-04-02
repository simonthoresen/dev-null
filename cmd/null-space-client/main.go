// null-space-client is a graphical SSH client for null-space servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
//
// This is a skeleton — the rendering engine (Ebitengine) and SSH transport
// will be implemented in internal/client/.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
)

func main() {
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	player := flag.String("player", defaultPlayer(), "player name")
	flag.Parse()

	fmt.Printf("null-space-client: connecting to %s:%d as %s\n", *host, *port, *player)
	fmt.Println("(not yet implemented — see internal/client/)")
	os.Exit(0)
}

func defaultPlayer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "player"
}
