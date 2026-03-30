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

	app, err := server.New(address, password, dataDir)
	if err != nil {
		printBootStep("SSH server", "FAILED")
		fmt.Fprintf(os.Stderr, "could not create server: %v\n", err)
		os.Exit(1)
	}
	printBootStep("SSH server", "DONE")

	// Determine port from address
	port := "23234"
	if idx := strings.LastIndex(address, ":"); idx >= 0 {
		if p := address[idx+1:]; p != "" {
			port = p
		}
	}

	// UPnP + public IP detection
	upnpMapped, publicIP := app.SetupNetwork(port)
	if upnpMapped {
		printBootStep("UPnP port mapping", "DONE")
	} else {
		printBootStep("UPnP port mapping", "IGNORED")
	}
	if publicIP != "" {
		printBootStep("Public IP detection", "DONE")
	} else {
		printBootStep("Public IP detection", "IGNORED")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app.SetShutdownFunc(func() {
		stop()
	})

	// Pinggy
	pinggyStatusFile := os.Getenv("NULL_SPACE_PINGGY_STATUS_FILE")
	if pinggyStatusFile != "" {
		app.EnablePinggyLogBridge(ctx, pinggyStatusFile)
		printBootStep("Pinggy tunnel", "DONE")
	} else {
		printBootStep("Pinggy tunnel", "IGNORED")
	}

	// Invite script
	inviteScript := buildInviteScript(app.State(), port)
	printBootStep("Invite script", "DONE")
	fmt.Println()
	fmt.Println("Connect with:")
	fmt.Println("  " + inviteScript)
	fmt.Println()

	// Start BubbleTea console
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

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
	}

	// Wait for server to stop
	if err := <-serverErr; err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func printBootStep(label string, status string) {
	const dotWidth = 50
	dots := dotWidth - len(label)
	if dots < 1 {
		dots = 1
	}
	statusColored := status
	switch status {
	case "DONE":
		statusColored = "\033[32mDONE\033[0m"
	case "FAILED":
		statusColored = "\033[31mFAILED\033[0m"
	case "IGNORED":
		statusColored = "\033[33mIGNORED\033[0m"
	case "SKIPPED":
		statusColored = "\033[90mSKIPPED\033[0m"
	}
	fmt.Printf("  %s %s [ %s ]\n", label, strings.Repeat(".", dots), statusColored)
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
