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

	var password string
	var address string
	var dataDir string
	flag.StringVar(&password, "password", "", "admin password (required)")
	flag.StringVar(&address, "address", ":23234", "listen address")
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "directory containing apps/, plugins/, logs/")
	flag.Parse()

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
		finishBootStep("FAILED")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	finishBootStep("DONE")

	startBootStep("UPnP port mapping")
	if app.SetupUPnP(port) {
		finishBootStep("DONE")
	} else {
		finishBootStep("IGNORED")
	}

	startBootStep("Public IP detection")
	if app.SetupPublicIP() != "" {
		finishBootStep("DONE")
	} else {
		finishBootStep("IGNORED")
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
		finishBootStep("IGNORED")
	}

	startBootStep("Generating invite script")
	buildInviteScript(app.State(), port)
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

	finishBootStep("DONE")
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
	}

	startBootStep("Initiating shutdown")
	finishBootStep("DONE")
	startBootStep("Stopping SSH server")
	if err := <-serverErr; err == nil || errors.Is(err, ssh.ErrServerClosed) {
		finishBootStep("DONE")
	} else {
		finishBootStep("FAILED")
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

var currentBootLabel string

// statusTokenWidth is the fixed display width of every status token: "[ IGNORED ]" = 11.
const statusTokenWidth = 11

// statusToken returns a fixed-width 11-char token with the status text centered.
func statusToken(status string) string {
	const inner = 7 // widest status (IGNORED/SKIPPED) is 7 chars
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
	case "FAILED":
		code = "\033[91m"
	case "IGNORED":
		code = "\033[93m"
	case "SKIPPED":
		code = "\033[90m"
	default:
		return token
	}
	// token is "[ <padded> ]" — color only the inner text, not the brackets
	const inner = 7
	pad := inner - len(status)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	right := pad - left
	return "[ " + strings.Repeat(" ", left) + code + status + "\033[0m" + strings.Repeat(" ", right) + " ]"
}

func bootTermWidth() int {
	w, _, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil || w < 40 {
		return 80
	}
	return w
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

func buildInviteScript(state *server.CentralState, port string) string {
	local := fmt.Sprintf("ssh -t -p %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost", port)

	var parts []string
	parts = append(parts, local)

	state.RLock()
	net := state.Net
	state.RUnlock()

	if net.PublicIP != "" && net.UPnPMapped {
		direct := fmt.Sprintf("ssh -t -p %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s", port, net.PublicIP)
		parts = append(parts, direct)
	}

	if net.PinggyURL != "" {
		host := net.PinggyURL
		pinggyPort := "22"
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			pinggyPort = host[idx+1:]
			host = host[:idx]
		}
		relay := fmt.Sprintf("ssh -t -p %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s", pinggyPort, host)
		parts = append(parts, relay)
	}

	if len(parts) == 1 {
		return parts[0]
	}

	// PowerShell one-liner: try in order, fall back on non-zero exit
	result := parts[0]
	for _, p := range parts[1:] {
		result = fmt.Sprintf("%s; if($LASTEXITCODE -ne 0){%s}", result, p)
	}
	return result
}
