package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/ssh"

	"null-space/common"
	"null-space/games/towerdefense"
	"null-space/server"
)

func main() {
	var gameName string
	var password string
	var address string

	flag.StringVar(&gameName, "game", "towerdefense", "game module to run")
	flag.StringVar(&password, "password", "", "admin password")
	flag.StringVar(&address, "address", ":23234", "listen address")
	flag.Parse()

	game, resolvedName, err := loadGame(gameName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app, err := server.New(address, resolvedName, game, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create server: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Start(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
		os.Exit(1)
	}
}

func loadGame(name string) (common.Game, string, error) {
	switch name {
	case "towerdefense", "tower-defense", "td":
		return towerdefense.New(), "towerdefense", nil
	default:
		return nil, "", fmt.Errorf("unknown game %q", name)
	}
}