package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	xterm "github.com/charmbracelet/x/term"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/ssh"

	"null-space/internal/runlog"
	"null-space/server"
)

func main() {
	cleanupLog, err := runlog.ConfigureFromEnv("server")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not configure logging: %v\n", err)
		os.Exit(1)
	}
	defer cleanupLog() //nolint:errcheck
	initBootTermWidth()

	var password string
	var address string
	var portOverride string
	var dataDir string
	var localMode bool
	var localGame string
	var localPlugins string
	var localPlayer string
	flag.StringVar(&password, "password", "", "admin password (required)")
	flag.StringVar(&address, "address", ":23234", "listen address")
	flag.StringVar(&portOverride, "port", "", "SSH listen port (overrides --address port, default 23234)")
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "directory containing games/, plugins/, logs/")
	flag.BoolVar(&localMode, "local", false, "run locally without SSH (single-player / render test)")
	flag.StringVar(&localGame, "game", "", "game to preload (local mode)")
	flag.StringVar(&localPlugins, "plugins", "", "comma-separated plugins to preload (local mode)")
	flag.StringVar(&localPlayer, "player", "player", "player name (local mode)")
	flag.Parse()

	if portOverride != "" {
		address = ":" + portOverride
	}

	if localMode {
		startBootStep("Generating invite command")
		finishBootStep("SKIP")

		app := server.NewLocal(dataDir)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		var plugins []string
		if localPlugins != "" {
			plugins = strings.Split(localPlugins, ",")
		}
		if err := app.RunLocal(ctx, localPlayer, localGame, plugins); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		startBootStep("Initiating shutdown")
		finishBootStep("DONE")
		startBootStep("Shutting down network")
		finishBootStep("SKIP")
		return
	}

	if password == "" {
		fmt.Fprintln(os.Stderr, "WARNING: no admin password set (use --password)")
	}

	// Determine port from address
	port := "23234"
	if idx := strings.LastIndex(address, ":"); idx >= 0 {
		if p := address[idx+1:]; p != "" {
			port = p
		}
	}

	startBootStep("SSH server")
	app, err := server.New(address, password, dataDir)
	if err != nil {
		finishBootStep("FAIL")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	app.SetPort(port)
	finishBootStep("DONE")

	startBootStep("UPnP port mapping")
	if app.SetupUPnP(port) {
		finishBootStep("DONE")
	} else {
		finishBootStep("SKIP")
	}

	startBootStep("Public IP detection")
	if app.SetupPublicIP() != "" {
		finishBootStep("DONE")
	} else {
		finishBootStep("SKIP")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app.SetShutdownFunc(func() {
		stop()
	})

	startBootStep("Pinggy tunnel")
	pinggyStatusFile := os.Getenv("NULL_SPACE_PINGGY_STATUS_FILE")
	if pinggyStatusFile != "" {
		app.EnablePinggyLogBridge(ctx, pinggyStatusFile)
		finishBootStep("DONE")
	} else {
		finishBootStep("SKIP")
	}

	startBootStep("Generating invite command")
	finishBootStep("DONE")

	startBootStep("Starting console")
	consoleModel := server.NewConsoleModel(app, stop)
	program := tea.NewProgram(consoleModel, tea.WithFPS(60))
	app.SetConsoleProgram(program)

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Start(ctx)
	}()

	// Quit console when context is cancelled
	go func() {
		<-ctx.Done()
		program.Send(tea.QuitMsg{})
	}()

	// Force-exit on second Ctrl+C (safety valve if Bubble Tea is stuck)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh // first signal handled by NotifyContext above
		<-sigCh // second signal = force exit
		fmt.Fprintf(os.Stderr, "\nForce exit (second interrupt)\n")
		os.Exit(1)
	}()

	finishBootStep("DONE")
	go app.LogInviteCommand()
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
	}

	startBootStep("Initiating shutdown")
	finishBootStep("DONE")
	startBootStep("Shutting down network")
	finishBootStep("DONE")
	startBootStep("Stopping SSH server")
	if err := <-serverErr; err == nil || errors.Is(err, ssh.ErrServerClosed) {
		finishBootStep("DONE")
	} else {
		finishBootStep("FAIL")
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

var currentBootLabel string

// statusTokenWidth is the fixed display width of every status token: "[ DONE ]" = 8.
const statusTokenWidth = 8

// statusToken returns a fixed-width 11-char token with the status text centered.
func statusToken(status string) string {
	const inner = 4 // widest status (DONE/FAIL/SKIP) is 4 chars
	pad := inner - len(status)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	right := pad - left
	return "[ " + strings.Repeat(" ", left) + status + strings.Repeat(" ", right) + " ]"
}

// colorizedToken colors only the status text inside the brackets.
func colorizedToken(token, status string) string {
	var code string
	switch status {
	case "DONE":
		code = "\033[92m"
	case "FAIL":
		code = "\033[91m"
	case "SKIP":
		code = "\033[93m"
	default:
		return token
	}
	// token is "[ <padded> ]" — color only the inner text, not the brackets
	const inner = 4
	pad := inner - len(status)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	right := pad - left
	return "[ " + strings.Repeat(" ", left) + code + status + "\033[0m" + strings.Repeat(" ", right) + " ]"
}

var cachedTermWidth int

func bootTermWidth() int {
	if cachedTermWidth > 0 {
		return cachedTermWidth
	}
	w, _, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil || w < 40 {
		w = 80
	}
	cachedTermWidth = w
	return w
}

func initBootTermWidth() {
	// If the parent process (start.ps1) passed a width, use it for
	// consistent alignment between PS1 and Go boot steps.
	if s := os.Getenv("NULL_SPACE_TERM_WIDTH"); s != "" {
		if w, err := fmt.Sscanf(s, "%d", &cachedTermWidth); err != nil || w != 1 || cachedTermWidth < 40 {
			cachedTermWidth = 0
		}
	}
}

// bootDots returns the number of dots to fill between label and status token.
// layout: label + " " + dots + " " + token
func bootDots(label string, w int) int {
	dots := w - len(label) - 1 - 1 - statusTokenWidth
	if dots < 1 {
		dots = 1
	}
	return dots
}

// startBootStep prints the step label with dots but no status.
// finishBootStep must be called after the operation completes.
func startBootStep(label string) {
	currentBootLabel = label
	w := bootTermWidth()
	fmt.Printf("%s %s", label, strings.Repeat(".", bootDots(label, w)))
}

// finishBootStep overwrites the current boot step line with the final status.
func finishBootStep(status string) {
	w := bootTermWidth()
	token := statusToken(status)
	dots := bootDots(currentBootLabel, w)
	fmt.Printf("\r%s %s %s\n", currentBootLabel, strings.Repeat(".", dots), colorizedToken(token, status))
}

// defaultDataDir returns the directory of the running executable.
// When running via "go run" the exe lives in a temp dir, so we fall back to ".".
func defaultDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	tmp := os.TempDir()
	if strings.HasPrefix(dir, tmp) {
		return "."
	}
	return dir
}

