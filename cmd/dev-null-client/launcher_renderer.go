package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hajimehoshi/ebiten/v2"

	"dev-null/internal/client"
	"dev-null/internal/datadir"
	"dev-null/internal/display"
	"dev-null/internal/network"
	"dev-null/internal/render"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

const (
	defaultServerPort = 23234
	serverProbeEvery  = 2 * time.Second
	lanDiscoverEvery  = 6 * time.Second
	lanDiscoverWait   = 900 * time.Millisecond
)

type launcherRendererConfig struct {
	Player       string
	Term         string
	Password     string
	InstallDir   string
	DataDir      string
	LocalPort    int
	WindowWidth  int
	WindowHeight int
	InitCommands []string
}

type launcherServer struct {
	Name      string
	Host      string
	Port      int
	Source    string
	Available bool
}

func (s launcherServer) endpoint() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
}

func (s launcherServer) key() string {
	return strings.ToLower(s.endpoint())
}

func (s launcherServer) itemLabel() string {
	return fmt.Sprintf("[%s] %s (%s)", s.Source, s.Name, s.endpoint())
}

type launcherRenderer struct {
	player       string
	term         string
	password     string
	installDir   string
	dataDir      string
	localPort    int
	windowWidth  int
	windowHeight int
	initCommands []string

	cols int
	rows int

	sessionConn     *client.SSHConn
	sessionRenderer *client.ClientRenderer

	localServer      *localServerSupervisor
	localPassword    string
	pinggyStatusFile string
	pinggyHelper     *exec.Cmd
	pinggyDone       chan error
	pinggyStdout     *os.File
	pinggyStderr     *os.File

	servers      []launcherServer
	lanCache     []launcherServer
	lastProbe    time.Time
	lastLAN      time.Time
	refreshing   bool
	refreshDone  chan refreshResult
	status       string
	closing      bool

	launcherTheme  *theme.Theme
	launcherWindow *widget.Window
	serverList     *widget.ListBox
	statusLabel    *widget.Label
	connectBtn     *widget.Button
	createBtn      *widget.Button
	refreshBtn     *widget.Button
	tunnelBtn      *widget.Button

	background      *launcherScene
	backgroundStart time.Time

	frameCount     uint64
	lastFrameLogAt time.Time
	lastFrameAt    time.Time
}

func newLauncherRenderer(cfg launcherRendererConfig) *launcherRenderer {
	r := &launcherRenderer{
		player:           cfg.Player,
		term:             cfg.Term,
		password:         cfg.Password,
		installDir:       cfg.InstallDir,
		dataDir:          cfg.DataDir,
		localPort:        cfg.LocalPort,
		windowWidth:      cfg.WindowWidth,
		windowHeight:     cfg.WindowHeight,
		initCommands:     append([]string(nil), cfg.InitCommands...),
		launcherTheme:    theme.Default(),
		status:           "Select a server or create a local one.",
		pinggyStatusFile: filepath.Join(os.TempDir(), fmt.Sprintf("DevNull-pinggy-%d.status.log", os.Getpid())),
	}
	if r.localPort <= 0 {
		r.localPort = defaultServerPort
	}
	r.refreshDone = make(chan refreshResult, 1)
	slog.Info("launcher: setupLauncherUI begin")
	t := time.Now()
	r.setupLauncherUI()
	slog.Info("launcher: setupLauncherUI done", "took", time.Since(t))
	t = time.Now()
	r.setupBackground()
	slog.Info("launcher: setupBackground done", "took", time.Since(t))
	t = time.Now()
	r.refreshServers(true)
	slog.Info("launcher: initial refreshServers dispatch done (sync portion)", "took", time.Since(t))
	return r
}

func (r *launcherRenderer) setupBackground() {
	r.background = newLauncherScene()
	r.backgroundStart = time.Now()
}

func (r *launcherRenderer) setupLauncherUI() {
	r.serverList = &widget.ListBox{
		Items: []string{"Scanning for servers..."},
		Tags:  []string{""},
	}
	r.connectBtn = &widget.Button{
		Label: "Connect",
		Disabled: func() bool {
			i := r.serverList.Cursor
			return i < 0 || i >= len(r.servers) || !r.servers[i].Available
		},
		OnPress: r.connectSelected,
	}
	r.createBtn = &widget.Button{
		Label:   "Create server",
		OnPress: r.createLocalAndConnect,
	}
	r.refreshBtn = &widget.Button{
		Label:   "Refresh",
		OnPress: func() { r.refreshServers(true) },
	}
	r.tunnelBtn = &widget.Button{
		Label:   "Open tunnel",
		OnPress: r.openTunnelOnDemand,
	}

	buttonRow := &widget.Container{
		Horizontal: true,
		Children: []widget.ContainerChild{
			{Control: r.connectBtn, Fixed: len(r.connectBtn.Label) + 6},
			{Control: &widget.Label{Text: " "}, Fixed: 1},
			{Control: r.createBtn, Fixed: len(r.createBtn.Label) + 6},
			{Control: &widget.Label{Text: " "}, Fixed: 1},
			{Control: r.tunnelBtn, Fixed: len(r.tunnelBtn.Label) + 6},
			{Control: &widget.Label{Text: " "}, Fixed: 1},
			{Control: r.refreshBtn, Fixed: len(r.refreshBtn.Label) + 6},
		},
	}

	r.statusLabel = &widget.Label{Align: "left"}
	r.launcherWindow = &widget.Window{
		Title: "DevNull",
		Children: []widget.GridChild{
			{
				Control: &widget.Label{Text: "Launcher", Align: "left"},
				Constraint: widget.GridConstraint{
					Col: 0, Row: 0, WeightX: 1, Fill: widget.FillHorizontal, MinH: 1,
				},
			},
			{
				Control: r.serverList, TabIndex: 0,
				Constraint: widget.GridConstraint{
					Col: 0, Row: 1, WeightX: 1, WeightY: 1, Fill: widget.FillBoth, MinH: 7,
				},
			},
			{
				Control: &widget.HDivider{Connected: true},
				Constraint: widget.GridConstraint{
					Col: 0, Row: 2, Fill: widget.FillHorizontal, MinH: 1,
				},
			},
			{
				Control: buttonRow, TabIndex: 1,
				Constraint: widget.GridConstraint{
					Col: 0, Row: 3, WeightX: 1, Fill: widget.FillHorizontal, MinH: 1,
				},
			},
			{
				Control: &widget.Label{
					Text:  "Enter: connect   F5: refresh   Esc: quit   Tunnel opens on demand",
					Align: "left",
				},
				Constraint: widget.GridConstraint{
					Col: 0, Row: 4, WeightX: 1, Fill: widget.FillHorizontal, MinH: 1,
				},
			},
			{
				Control: r.statusLabel,
				Constraint: widget.GridConstraint{
					Col: 0, Row: 5, WeightX: 1, Fill: widget.FillHorizontal, MinH: 1,
				},
			},
		},
	}
	r.launcherWindow.FocusFirst()
}

func (r *launcherRenderer) Stop() {
	r.stopPinggyHelper()
	if r.sessionConn != nil {
		_ = r.sessionConn.Close()
		r.sessionConn = nil
		r.sessionRenderer = nil
	}
	if r.localServer != nil {
		r.localServer.Stop()
		r.localServer = nil
	}
	if r.pinggyStatusFile != "" {
		_ = os.Remove(r.pinggyStatusFile)
	}
}

func (r *launcherRenderer) HandleInput(w *display.Window) {
	if r.sessionRenderer != nil {
		r.sessionRenderer.HandleInput(w)
		if r.sessionRenderer.ShouldClose() {
			r.closeSession()
			r.status = "Disconnected. Back in launcher."
			r.refreshServers(true)
		}
		return
	}

	if time.Since(r.lastProbe) >= serverProbeEvery {
		r.refreshServers(false)
	}
	r.applyRefreshResult()

	msgs := append(display.PollKeyMessages(), display.PollMouseMessages()...)
	for _, msg := range msgs {
		switch m := msg.(type) {
		case tea.MouseClickMsg:
			r.launcherWindow.HandleClick(m.X, m.Y)
			continue
		case tea.KeyPressMsg:
			switch m.String() {
			case "esc":
				r.closing = true
				return
			case "f5", "ctrl+r":
				r.refreshServers(true)
				continue
			case "enter":
				if r.launcherWindow.FocusedControl() == r.serverList {
					r.connectSelected()
					continue
				}
			}
		}

		r.launcherWindow.HandleUpdate(msg)
	}
}

func (r *launcherRenderer) Draw(w *display.Window, screen *ebiten.Image) {
	now := time.Now()
	r.frameCount++
	gap := time.Duration(0)
	if !r.lastFrameAt.IsZero() {
		gap = now.Sub(r.lastFrameAt)
	}
	// Log every frame whose gap >= 100ms (a stutter), plus once per second
	// at minimum so we can see the timeline even when smooth.
	if gap >= 100*time.Millisecond || now.Sub(r.lastFrameLogAt) >= time.Second {
		slog.Info("launcher: Draw",
			"frame", r.frameCount,
			"gapMs", gap.Milliseconds(),
			"refreshing", r.refreshing,
			"servers", len(r.servers),
			"cols", r.cols, "rows", r.rows,
		)
		r.lastFrameLogAt = now
	}
	r.lastFrameAt = now

	if r.sessionRenderer != nil {
		r.sessionRenderer.Draw(w, screen)
		return
	}

	if r.cols <= 0 || r.rows <= 0 {
		return
	}

	buf := render.NewImageBuffer(r.cols, r.rows)

	// Render the animated 3D background scene through the same pipeline used
	// by the in-session blocks-local mode: render to a 2×W × 4×H RGBA image
	// and convert to quadrant block characters in the cell buffer.
	if r.background != nil {
		elapsed := time.Since(r.backgroundStart).Seconds()
		if img := r.background.Render(r.cols*2, r.rows*4, elapsed); img != nil {
			render.ImageToQuadrants(img, buf, 0, 0, r.cols, r.rows)
		}
	}

	dialogW := min(86, max(56, r.cols-12))
	dialogH := min(20, max(12, r.rows-6))
	dialogX := max(0, (r.cols-dialogW)/2)
	dialogY := max(0, (r.rows-dialogH)/2)

	r.statusLabel.Text = r.clipStatus(r.status)
	r.launcherWindow.RenderToBuf(buf, dialogX, dialogY, dialogW, dialogH, r.launcherTheme.LayerAt(0))
	display.DrawImageBuffer(screen, buf, w.FontFace, nil)
}

func (r *launcherRenderer) Resize(cols, rows int) {
	r.cols = cols
	r.rows = rows
	if r.sessionRenderer != nil {
		r.sessionRenderer.Resize(cols, rows)
	}
}

func (r *launcherRenderer) ShouldClose() bool {
	return r.closing
}

func (r *launcherRenderer) connectSelected() {
	i := r.serverList.Cursor
	if i < 0 || i >= len(r.servers) {
		r.status = "No server selected."
		return
	}
	target := r.servers[i]
	if !target.Available {
		r.status = "Selected server is offline."
		return
	}

	password := r.password
	if strings.EqualFold(target.Source, "Local") && r.localPassword != "" {
		password = r.localPassword
	}
	if err := r.connectTo(target.Host, target.Port, password); err != nil {
		r.status = fmt.Sprintf("Connect failed: %v", err)
		return
	}
	r.status = fmt.Sprintf("Connected to %s", target.endpoint())
}

func (r *launcherRenderer) createLocalAndConnect() {
	if err := r.ensureLocalServer(); err != nil {
		r.status = fmt.Sprintf("Local server start failed: %v", err)
		return
	}

	if err := r.connectTo("127.0.0.1", r.localPort, r.localPassword); err != nil {
		r.status = fmt.Sprintf("Connect failed: %v", err)
		return
	}
	r.refreshServers(true)
	r.status = "Local server running and connected."
}

func (r *launcherRenderer) openTunnelOnDemand() {
	if err := r.ensureLocalServer(); err != nil {
		r.status = fmt.Sprintf("Local server start failed: %v", err)
		return
	}
	if err := r.startPinggyHelper(); err != nil {
		r.status = fmt.Sprintf("Tunnel start failed: %v", err)
		return
	}
	status, err := network.ReadPinggyStatus(r.pinggyStatusFile)
	if err != nil || status == nil || status.TcpAddress == "" {
		r.status = "Tunnel started."
		return
	}
	r.status = fmt.Sprintf("Tunnel ready: %s", status.TcpAddress)
}

func (r *launcherRenderer) ensureLocalServer() error {
	if r.localServerReady() {
		return nil
	}
	password, err := randomHexSecret(16)
	if err != nil {
		return err
	}
	srv, err := startLocalServer(localServerConfig{
		InstallDir:       r.installDir,
		DataDir:          r.dataDir,
		Term:             r.term,
		Port:             r.localPort,
		Password:         password,
		PinggyStatusFile: r.pinggyStatusFile,
	})
	if err != nil {
		return err
	}
	if r.localServer != nil {
		r.localServer.Stop()
	}
	r.stopPinggyHelper()
	r.localServer = srv
	r.localPassword = password
	return nil
}

func (r *launcherRenderer) startPinggyHelper() error {
	if r.pinggyStatusFile == "" {
		return fmt.Errorf("missing pinggy status file")
	}

	// Tunnel already up.
	if status, err := network.ReadPinggyStatus(r.pinggyStatusFile); err == nil && status != nil && status.TcpAddress != "" {
		return nil
	}

	if r.pinggyHelper != nil {
		r.stopPinggyHelper()
	}
	_ = os.Remove(r.pinggyStatusFile)

	helperPath, err := findPinggyHelperBinary(r.installDir)
	if err != nil {
		return err
	}
	logsDir := filepath.Join(r.dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	stdoutPath := filepath.Join(logsDir, "local-pinggy-stdout.log")
	stderrPath := filepath.Join(logsDir, "local-pinggy-stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open pinggy stdout log: %w", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = stdout.Close()
		return fmt.Errorf("open pinggy stderr log: %w", err)
	}

	cmd := exec.Command(
		helperPath,
		"--listen", fmt.Sprintf("127.0.0.1:%d", r.localPort),
		"--status-file", r.pinggyStatusFile,
	)
	cmd.Dir = r.dataDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("start pinggy helper: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	r.pinggyHelper = cmd
	r.pinggyDone = done
	r.pinggyStdout = stdout
	r.pinggyStderr = stderr

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := network.ReadPinggyStatus(r.pinggyStatusFile); err == nil && status != nil && status.TcpAddress != "" {
			return nil
		}

		select {
		case waitErr := <-done:
			r.stopPinggyHelper()
			if waitErr != nil {
				return fmt.Errorf("pinggy helper exited: %w", waitErr)
			}
			return fmt.Errorf("pinggy helper exited before tunnel became ready")
		default:
		}

		time.Sleep(250 * time.Millisecond)
	}

	r.stopPinggyHelper()
	return fmt.Errorf("timed out waiting for pinggy tunnel")
}

func (r *launcherRenderer) stopPinggyHelper() {
	if r.pinggyHelper != nil && r.pinggyHelper.Process != nil {
		select {
		case <-r.pinggyDone:
		default:
			_ = r.pinggyHelper.Process.Kill()
			select {
			case <-r.pinggyDone:
			case <-time.After(2 * time.Second):
			}
		}
	}
	r.pinggyHelper = nil
	r.pinggyDone = nil
	if r.pinggyStdout != nil {
		_ = r.pinggyStdout.Close()
		r.pinggyStdout = nil
	}
	if r.pinggyStderr != nil {
		_ = r.pinggyStderr.Close()
		r.pinggyStderr = nil
	}
}

func (r *launcherRenderer) connectTo(host string, port int, password string) error {
	ptyW, ptyH := r.cols, r.rows
	if ptyW <= 0 {
		ptyW = display.WindowCols(r.windowWidth)
	}
	if ptyH <= 0 {
		ptyH = display.WindowRows(r.windowHeight)
	}

	conn, err := client.Dial(host, port, r.player, r.term, password, ptyW, ptyH, r.initCommands)
	if err != nil {
		return err
	}

	r.closeSession()
	r.sessionConn = conn
	r.sessionRenderer = client.NewClientRenderer(conn, r.windowWidth, r.windowHeight, r.player, r.installDir, r.dataDir)
	r.sessionRenderer.Resize(ptyW, ptyH)
	return nil
}

func (r *launcherRenderer) closeSession() {
	if r.sessionConn != nil {
		_ = r.sessionConn.Close()
	}
	r.sessionConn = nil
	r.sessionRenderer = nil
}

func (r *launcherRenderer) localServerReady() bool {
	return probeServer("127.0.0.1", r.localPort, 300*time.Millisecond)
}

// refreshResult carries the outcome of an off-thread server scan back to
// the UI goroutine, where it gets applied via applyRefreshResult().
type refreshResult struct {
	servers  []launcherServer
	lanCache []launcherServer
	didLAN   bool
}

// refreshServers kicks off a background scan of known/LAN servers and TCP
// probes. Discovery and probing block for hundreds of milliseconds, so they
// must never run on the UI goroutine — that caused visible stutters in the
// launcher animation every few seconds. force=true bypasses the rate limits;
// the callback applies results from the channel each frame.
func (r *launcherRenderer) refreshServers(force bool) {
	if r.refreshing {
		slog.Debug("launcher: refreshServers skip — already refreshing", "force", force)
		return
	}
	if !force && time.Since(r.lastProbe) < serverProbeEvery {
		return
	}

	needLAN := force || time.Since(r.lastLAN) >= lanDiscoverEvery
	localPort := r.localPort
	lanSnapshot := append([]launcherServer(nil), r.lanCache...)

	r.refreshing = true
	r.lastProbe = time.Now()
	if needLAN {
		r.lastLAN = time.Now()
	}

	slog.Info("launcher: refresh dispatch", "force", force, "needLAN", needLAN, "lanCacheLen", len(lanSnapshot))

	go func() {
		gStart := time.Now()
		slog.Info("launcher: refresh goroutine begin")
		lanCache := lanSnapshot
		if needLAN {
			t := time.Now()
			slog.Info("launcher: discoverLANServers begin", "timeout", lanDiscoverWait)
			discovered, err := discoverLANServers(lanDiscoverWait)
			slog.Info("launcher: discoverLANServers end", "took", time.Since(t), "err", err, "found", len(discovered))
			if err == nil {
				lanCache = discovered
			}
		}
		t := time.Now()
		servers := collectLauncherServers(localPort, lanCache)
		slog.Info("launcher: collectLauncherServers done", "took", time.Since(t), "count", len(servers))
		t = time.Now()
		probeLauncherServers(servers, 350*time.Millisecond)
		slog.Info("launcher: probeLauncherServers done", "took", time.Since(t))

		// Drop any prior pending result; only the latest matters.
		select {
		case <-r.refreshDone:
		default:
		}
		r.refreshDone <- refreshResult{servers: servers, lanCache: lanCache, didLAN: needLAN}
		slog.Info("launcher: refresh goroutine end", "totalTook", time.Since(gStart))
	}()
}

// applyRefreshResult drains any pending background-scan result and updates
// the UI state. Called from the UI goroutine each frame.
func (r *launcherRenderer) applyRefreshResult() {
	var res refreshResult
	select {
	case res = <-r.refreshDone:
	default:
		return
	}
	t := time.Now()
	r.refreshing = false
	if res.didLAN {
		r.lanCache = res.lanCache
	}

	selectedKey := ""
	if i := r.serverList.Cursor; i >= 0 && i < len(r.servers) {
		selectedKey = r.servers[i].key()
	}
	r.servers = res.servers

	items := make([]string, 0, len(r.servers))
	tags := make([]string, 0, len(r.servers))
	cursor := 0
	for i, s := range r.servers {
		items = append(items, s.itemLabel())
		if s.Available {
			tags = append(tags, "UP")
		} else {
			tags = append(tags, "DOWN")
		}
		if selectedKey != "" && s.key() == selectedKey {
			cursor = i
		}
	}
	if len(items) == 0 {
		items = []string{"No servers configured. Use Create server."}
		tags = []string{""}
	}
	r.serverList.Items = items
	r.serverList.Tags = tags
	r.serverList.SetCursor(cursor)
	slog.Info("launcher: applyRefreshResult done", "took", time.Since(t), "items", len(items), "didLAN", res.didLAN)
}

func collectLauncherServers(localPort int, lan []launcherServer) []launcherServer {
	if localPort <= 0 {
		localPort = defaultServerPort
	}
	servers := []launcherServer{
		{Name: "This machine", Host: "127.0.0.1", Port: localPort, Source: "Local"},
	}
	known := readKnownServers(datadir.InitFilePath("servers.txt"))
	seen := map[string]bool{
		servers[0].key(): true,
	}
	for _, s := range known {
		if seen[s.key()] {
			continue
		}
		seen[s.key()] = true
		servers = append(servers, s)
	}
	for _, s := range lan {
		if seen[s.key()] {
			continue
		}
		seen[s.key()] = true
		servers = append(servers, s)
	}
	return servers
}

func discoverLANServers(timeout time.Duration) ([]launcherServer, error) {
	discovered, err := network.DiscoverLANServers(timeout)
	if err != nil {
		return nil, err
	}
	servers := make([]launcherServer, 0, len(discovered))
	for _, s := range discovered {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = s.Host
		}
		servers = append(servers, launcherServer{
			Name:   name,
			Host:   s.Host,
			Port:   s.Port,
			Source: "LAN",
		})
	}
	return servers, nil
}

func probeLauncherServers(servers []launcherServer, timeout time.Duration) {
	var wg sync.WaitGroup
	for i := range servers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			servers[idx].Available = probeServer(servers[idx].Host, servers[idx].Port, timeout)
		}(i)
	}
	wg.Wait()
}

func readKnownServers(path string) []launcherServer {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var out []launcherServer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name := ""
		endpoint := line
		if idx := strings.Index(line, "="); idx > 0 {
			name = strings.TrimSpace(line[:idx])
			endpoint = strings.TrimSpace(line[idx+1:])
		}
		host, port, ok := parseKnownEndpoint(endpoint)
		if !ok {
			continue
		}
		if name == "" {
			name = host
		}
		out = append(out, launcherServer{
			Name:   name,
			Host:   host,
			Port:   port,
			Source: "Known",
		})
	}
	return out
}

func parseKnownEndpoint(raw string) (string, int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, false
	}

	if !strings.Contains(raw, ":") {
		return raw, defaultServerPort, true
	}

	if host, portText, err := net.SplitHostPort(raw); err == nil {
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 {
			return "", 0, false
		}
		return host, port, true
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	host := strings.TrimSpace(parts[0])
	port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || host == "" || port <= 0 {
		return "", 0, false
	}
	return host, port, true
}

func probeServer(host string, port int, timeout time.Duration) bool {
	if host == "" || port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (r *launcherRenderer) clipStatus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	limit := 68
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	return string(rs[:limit-3]) + "..."
}
