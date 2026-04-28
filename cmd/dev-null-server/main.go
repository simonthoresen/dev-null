package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/ssh"
	"log/slog"

	"dev-null/internal/bootstep"
	"dev-null/internal/console"
	"dev-null/internal/datadir"
	"dev-null/internal/engine"
	"dev-null/internal/network"
	"dev-null/internal/runlog"
	"dev-null/internal/server"
)

//go:embed winres/icon.ico
var appIcon []byte

// buildCommit, buildDate, and buildRemote are injected at build time via -ldflags.
// They default to "dev" / "unknown" / "" for local go run / unbuilt binaries.
var buildCommit = "dev"
var buildDate = "unknown"
var buildRemote = ""

func main() {
	// Logging is configured after flag parsing (needs --data-dir).
	// For now, set build info early.
	engine.SetBuildInfo(buildDate, buildRemote)

	var password string
	var address string
	var portOverride string
	var dataDir string
	var lanMode bool
	var tickInterval time.Duration
	var termFlag string
	flag.StringVar(&password, "password", "", "admin password (optional, can be set at runtime via /password)")
	flag.StringVar(&address, "address", ":23234", "listen address")
	flag.StringVar(&portOverride, "port", "", "SSH listen port (overrides --address port, default 23234)")
	flag.StringVar(&dataDir, "data-dir", datadir.CommonDir(), "directory containing Games/, logs/")
	flag.BoolVar(&lanMode, "lan", false, "LAN-only server (no UPnP, no public IP, no Pinggy)")
	var headless bool
	flag.BoolVar(&headless, "headless", false, "run with no console UI (for --local subprocess mode)")
	flag.DurationVar(&tickInterval, "tick-interval", 100*time.Millisecond, "server tick interval (e.g. 100ms, 50ms)")
	flag.StringVar(&termFlag, "term", "", "force terminal color profile for all sessions: truecolor, 256color, ansi, ascii")
	flag.Parse()
	bootstep.Init(termFlag)
	bootProfile := bootstep.Profile()

	// Bootstrap bundled assets from install dir to data dir on first
	// run or version upgrade. Skipped in dev mode and when --data-dir
	// is explicitly set to a non-default path.
	if dataDir == datadir.CommonDir() {
		if err := datadir.Bootstrap(datadir.InstallDir(), dataDir, buildCommit); err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap error: %v\n", err)
			os.Exit(1)
		}
	}

	// Set up logging to data-dir/logs/server-<timestamp>.log.
	logsDir := filepath.Join(dataDir, "logs")
	cleanupLog, err := runlog.ConfigureAuto(logsDir, "server")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not configure logging: %v\n", err)
		os.Exit(1)
	}
	defer cleanupLog() //nolint:errcheck
	slog.Info("DevNull server", "commit", buildCommit, "built", buildDate)

	if portOverride != "" {
		address = ":" + portOverride
	}

	// Determine port from address
	port := "23234"
	if idx := strings.LastIndex(address, ":"); idx >= 0 {
		if p := address[idx+1:]; p != "" {
			port = p
		}
	}

	app, err := server.New(address, password, dataDir, tickInterval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating server: %v\n", err)
		os.Exit(1)
	}
	app.SetPort(port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app.SetShutdownFunc(func() { stop() })

	startLANAdvertiser := func() *network.LANAdvertiser {
		if os.Getenv("DEV_NULL_DISABLE_LAN_DISCOVERY") == "1" {
			return nil
		}
		lanPort, err := net.LookupPort("tcp", port)
		if err != nil {
			slog.Warn("lan discovery: invalid ssh port", "port", port, "error", err)
			return nil
		}
		advertiser, err := network.StartLANAdvertiser(lanPort)
		if err != nil {
			slog.Warn("lan discovery: failed to advertise", "error", err)
			return nil
		}
		return advertiser
	}

	// Headless mode: SSH server only, no console UI, no UPnP, no boot steps.
	// Used by --local subprocess mode where the client owns the display.
	if headless {
		if advertiser := startLANAdvertiser(); advertiser != nil {
			defer advertiser.Close()
		}
		if pinggyStatusFile := os.Getenv("DEV_NULL_PINGGY_STATUS_FILE"); pinggyStatusFile != "" {
			app.EnablePinggyLogBridge(ctx, pinggyStatusFile)
		}
		if err := app.Start(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			slog.Error("server error", "err", err)
		}
		return
	}

	// --- Interactive mode: full boot sequence with console UI ---
	bootstep.Start("SSH server")
	bootstep.Finish("DONE")

	app.InstallConsoleSlogHandler()
	app.OpenChatLog()
	defer app.CloseChatLog()

	if lanMode {
		bootstep.Start("UPnP port mapping")
		bootstep.Finish("SKIP")
		bootstep.Start("Public IP detection")
		bootstep.Finish("SKIP")
	} else {
		bootstep.Start("UPnP port mapping")
		if app.SetupUPnP(port) {
			bootstep.Finish("DONE")
		} else {
			bootstep.Finish("SKIP")
		}

		bootstep.Start("Public IP detection")
		if app.SetupPublicIP() != "" {
			bootstep.Finish("DONE")
		} else {
			bootstep.Finish("SKIP")
		}
	}

	bootstep.Start("LAN discovery")
	if advertiser := startLANAdvertiser(); advertiser != nil {
		defer advertiser.Close()
		bootstep.Finish("DONE")
	} else {
		bootstep.Finish("SKIP")
	}

	bootstep.Start("Pinggy tunnel")
	pinggyStatusFile := os.Getenv("DEV_NULL_PINGGY_STATUS_FILE")
	if lanMode || pinggyStatusFile == "" {
		bootstep.Finish("SKIP")
		go app.LogInviteCommand()
	} else {
		app.EnablePinggyLogBridge(ctx, pinggyStatusFile)
		bootstep.Finish("DONE")
	}

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Start(ctx)
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

	{
		bootstep.Start("Starting console")
		consoleModel := console.NewModel(app, stop, bootProfile)
		bootstep.Finish("DONE")

		// TUI mode: run console in the terminal via Bubble Tea.
		program := tea.NewProgram(consoleModel, tea.WithFPS(60), tea.WithColorProfile(bootProfile))
		app.SetConsoleProgram(program)
		go func() {
			<-ctx.Done()
			program.Send(tea.QuitMsg{})
		}()
		if _, err := program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "console error: %v\n", err)
		}
	}

	bootstep.Start("Initiating shutdown")
	bootstep.Finish("DONE")
	bootstep.Start("Shutting down network")
	bootstep.Finish("DONE")
	bootstep.Start("Stopping SSH server")
	if err := <-serverErr; err == nil || errors.Is(err, ssh.ErrServerClosed) {
		bootstep.Finish("DONE")
	} else {
		bootstep.Finish("FAIL")
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
