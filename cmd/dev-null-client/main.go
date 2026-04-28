// DevNullClient is a graphical SSH client for DevNull servers.
//
// It connects via standard SSH but additionally supports charmap-based
// sprite rendering: games that declare a charmap have their PUA codepoints
// rendered as sprites from a sprite sheet instead of terminal glyphs.
package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"dev-null/internal/bootstep"
	"dev-null/internal/client"
	"dev-null/internal/datadir"
	"dev-null/internal/display"
	"dev-null/internal/engine"
	"dev-null/internal/invite"
)

//go:embed winres/icon.ico
var appIcon []byte

// buildCommit, buildDate, and buildRemote are injected at build time via -ldflags.
var buildCommit = "dev"
var buildDate = "unknown"
var buildRemote = ""

func main() {
	engine.SetBuildInfo(buildDate, buildRemote)
	rawArgs := append([]string(nil), os.Args[1:]...)
	host := flag.String("host", "localhost", "server hostname")
	port := flag.Int("port", 23234, "server SSH port")
	local := flag.Bool("local", false, "start a local headless server and connect to it")
	player := flag.String("player", defaultPlayer(), "player name")
	dataDirFlag := flag.String("data-dir", datadir.CommonDir(), "data directory (SoundFonts, etc.)")
	gameName := flag.String("game", "", "game to load on connect (sends /game-load command)")
	resumeName := flag.String("resume", "", "game/save to resume on connect, e.g. orbits/autosave (sends /game-resume command)")
	password := flag.String("password", "", "admin password (authenticates as admin on connect)")
	termFlag := flag.String("term", "", "force terminal color profile: truecolor, 256color, ansi, ascii")
	inviteToken := flag.String("invite", "", "invite token (overrides --host/--port; picks the first reachable endpoint)")
	flag.Parse()

	if *local {
		*host = "localhost"
	}

	// --invite overrides --host/--port: decode the token, TCP-probe each
	// endpoint in priority order (localhost → LAN → public → Pinggy), and
	// pick the first reachable one.
	if *inviteToken != "" && !*local {
		endpoints, err := invite.Decode(*inviteToken)
		if err != nil {
			log.Fatalf("Invalid --invite token: %v", err)
		}
		picked := pickReachable(endpoints, 3*time.Second)
		if picked == nil {
			fmt.Fprintln(os.Stderr, "No reachable endpoint in invite. Tried:")
			for _, ep := range endpoints {
				fmt.Fprintf(os.Stderr, "  %s\n", ep.FormatHostPort())
			}
			os.Exit(1)
		}
		*host = picked.Host
		*port = picked.Port
	}

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
	const winW, winH = 1200, 800

	explicitConnect := *local ||
		*inviteToken != "" ||
		hasCLIFlag(rawArgs, "host") ||
		hasCLIFlag(rawArgs, "port")
	if !explicitConnect {
		menu := newMenuRenderer(menuRendererConfig{
			Player:       *player,
			Term:         *termFlag,
			Password:     *password,
			InstallDir:   datadir.InstallDir(),
			DataDir:      *dataDirFlag,
			LocalPort:    *port,
			WindowWidth:  winW,
			WindowHeight: winH,
			InitCommands: initCommands,
		})
		defer menu.Stop()
		if err := display.RunWindow(menu, "DevNull", winW, winH, appIcon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Init font before dialing so CellW/CellH are set to their real values.
	// This lets us request the correct PTY size from the very first frame,
	// avoiding a size mismatch between the initial server render and the window.
	display.InitGUIFont()
	ptyW := display.WindowCols(winW)
	ptyH := display.WindowRows(winH)

	var localServer *localServerSupervisor
	if *local {
		localPassword, err := randomHexSecret(16)
		if err != nil {
			log.Fatalf("Failed to generate local password: %v", err)
		}
		*password = localPassword
		bootstep.Start(fmt.Sprintf("Starting local server on port %d", *port))
		localServer, err = startLocalServer(localServerConfig{
			InstallDir: datadir.InstallDir(),
			DataDir:    *dataDirFlag,
			Term:       *termFlag,
			Port:       *port,
			Password:   localPassword,
		})
		if err != nil {
			bootstep.Finish("FAIL")
			log.Fatalf("Failed to start local server: %v", err)
		}
		bootstep.Finish("DONE")
		defer localServer.Stop()
	}

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

	if err := display.RunWindow(renderer, "DevNull", winW, winH, appIcon); err != nil {
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

// pickReachable returns the first endpoint that accepts a TCP connection
// within the given timeout. Returns nil if none are reachable.
func pickReachable(endpoints []invite.Endpoint, timeout time.Duration) *invite.Endpoint {
	for i := range endpoints {
		ep := endpoints[i]
		conn, err := net.DialTimeout("tcp", ep.FormatHostPort(), timeout)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return &ep
	}
	return nil
}

type localServerConfig struct {
	InstallDir       string
	DataDir          string
	Term             string
	Port             int
	Password         string
	PinggyStatusFile string
}

type localServerSupervisor struct {
	cmd    *exec.Cmd
	done   chan error
	stdout *os.File
	stderr *os.File
}

func startLocalServer(cfg localServerConfig) (*localServerSupervisor, error) {
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("invalid local server port: %d", cfg.Port)
	}

	serverExe, err := findServerBinary(cfg.InstallDir)
	if err != nil {
		return nil, err
	}

	logsDir := filepath.Join(cfg.DataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	stdoutPath := filepath.Join(logsDir, "local-server-stdout.log")
	stderrPath := filepath.Join(logsDir, "local-server-stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open local server stdout log: %w", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("open local server stderr log: %w", err)
	}

	args := []string{
		"--headless",
		"--port", fmt.Sprintf("%d", cfg.Port),
		"--password", cfg.Password,
		"--data-dir", cfg.DataDir,
	}
	if cfg.Term != "" {
		args = append(args, "--term", cfg.Term)
	}

	cmd := exec.Command(serverExe, args...)
	cmd.Dir = cfg.DataDir
	if cfg.PinggyStatusFile != "" {
		cmd.Env = append(os.Environ(), "DEV_NULL_PINGGY_STATUS_FILE="+cfg.PinggyStatusFile)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start local server process: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", cfg.Port))
	if err := waitForServerReady(addr, 15*time.Second, done); err != nil {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}

	return &localServerSupervisor{
		cmd:    cmd,
		done:   done,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

func (s *localServerSupervisor) Stop() {
	if s == nil {
		return
	}

	if s.cmd != nil && s.cmd.Process != nil {
		select {
		case <-s.done:
		default:
			_ = s.cmd.Process.Kill()
			select {
			case <-s.done:
			case <-time.After(2 * time.Second):
			}
		}
	}

	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	if s.stderr != nil {
		_ = s.stderr.Close()
	}
}

func findServerBinary(installDir string) (string, error) {
	candidates := []string{
		filepath.Join(installDir, "DevNullServer.exe"),
		filepath.Join(installDir, "DevNullServer"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("local server binary not found in %s", installDir)
}

func findPinggyHelperBinary(installDir string) (string, error) {
	candidates := []string{
		filepath.Join(installDir, "PinggyHelper.exe"),
		filepath.Join(installDir, "PinggyHelper"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Pinggy helper binary not found in %s", installDir)
}

func waitForServerReady(addr string, timeout time.Duration, done <-chan error) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		select {
		case procErr := <-done:
			if procErr != nil {
				return fmt.Errorf("local server exited before ready: %w", procErr)
			}
			return errors.New("local server exited before ready")
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}

	select {
	case procErr := <-done:
		if procErr != nil {
			return fmt.Errorf("local server exited before ready: %w", procErr)
		}
		return errors.New("local server exited before ready")
	default:
	}

	return fmt.Errorf("local server failed to start on %s within %s", addr, timeout)
}

func randomHexSecret(byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", fmt.Errorf("invalid secret length: %d", byteLen)
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hasCLIFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || arg == "-"+name || strings.HasPrefix(arg, "--"+name+"=") || strings.HasPrefix(arg, "-"+name+"=") {
			return true
		}
	}
	return false
}
