// Package main runs a load profile against an in-process dev-null server.
//
// It boots a server on a random port, connects N SSH clients (split between
// plain "ssh" sessions and "enhanced" GUI-style sessions that take the
// local-render path), loads a game, lets it start, then measures:
//
//   - Per-client received-byte rate (used to derive SSH frame rate)
//   - Server-side metrics: tick budget, JS update/render time, canvas-render
//     count and duration, View() count and duration
//   - For each "GUI" client, an in-process LocalRenderer-style benchmark of
//     the game's renderCanvas hook so we can verify the 60fps target is
//     reachable on the client side
//
// Captures CPU and goroutine pprof profiles into the working directory while
// the measurement window is active.
//
// Usage:
//
//	go run ./cmd/profile-load --ssh 8 --gui 8 --game wolf3d --duration 15s
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
	"dev-null/internal/server"
)

func main() {
	var (
		nSSH       = flag.Int("ssh", 8, "number of plain SSH clients to spawn")
		nGUI       = flag.Int("gui", 8, "number of enhanced (DEV_NULL_CLIENT=enhanced) SSH clients to spawn")
		gameName   = flag.String("game", "wolf3d", "game to load (must be in dist/games/)")
		duration   = flag.Duration("duration", 15*time.Second, "measurement window")
		warmup     = flag.Duration("warmup", 12*time.Second, "wait this long after game-load before starting measurement (covers 10s starting countdown)")
		dataDir    = flag.String("data-dir", "dist", "data directory containing games/")
		tickInt    = flag.Duration("tick", 100*time.Millisecond, "server tick interval")
		ptyW       = flag.Int("pty-w", 120, "PTY width")
		ptyH       = flag.Int("pty-h", 40, "PTY height")
		cpuProfile = flag.String("cpuprofile", "cpu.prof", "write CPU profile here")
		guiBench   = flag.Bool("gui-bench", true, "benchmark game.renderCanvas in N parallel goroutines to estimate GUI-client local FPS")
		quiet      = flag.Bool("quiet", true, "suppress server slog output")
	)
	flag.Parse()

	if *quiet {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	}

	gamesDir := filepath.Join(*dataDir, "games")
	gamePath := engine.ResolveGamePath(gamesDir, *gameName)
	if gamePath == "" {
		fail("game %q not found under %s", *gameName, gamesDir)
	}

	// Bind to :0 to get a free port; hand the listener to the server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail("listen: %v", err)
	}
	addr := ln.Addr().String()
	fmt.Printf("server: %s  game: %s  ssh=%d gui=%d duration=%s\n", addr, gamePath, *nSSH, *nGUI, *duration)

	srv, err := server.New(addr, "", *dataDir, *tickInt)
	if err != nil {
		fail("server.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ServeListener(ctx, ln, ready) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		fail("server did not become ready")
	}

	// Connect SSH clients in parallel. Plain SSH first, enhanced second —
	// the enhanced clients turn on local rendering, exercising the OSC state
	// stream rather than the server-side raycast.
	conns := make([]*connInfo, 0, *nSSH+*nGUI)
	for i := 0; i < *nSSH; i++ {
		conns = append(conns, &connInfo{idx: i, closed: make(chan struct{})})
	}
	for i := 0; i < *nGUI; i++ {
		conns = append(conns, &connInfo{idx: *nSSH + i, enhanced: true, closed: make(chan struct{})})
	}

	var dialWG sync.WaitGroup
	for _, c := range conns {
		c := c
		dialWG.Add(1)
		go func() {
			defer dialWG.Done()
			if err := dialClient(addr, c, *ptyW, *ptyH); err != nil {
				fmt.Fprintf(os.Stderr, "dial %d: %v\n", c.idx, err)
			}
		}()
	}
	dialWG.Wait()

	// Drop sessions that didn't connect.
	live := conns[:0]
	for _, c := range conns {
		if c.conn != nil {
			live = append(live, c)
		}
	}
	conns = live
	if len(conns) == 0 {
		fail("no clients connected")
	}
	fmt.Printf("connected %d/%d clients\n", len(conns), *nSSH+*nGUI)

	// Start byte-counting reader on each client.
	for _, c := range conns {
		c := c
		go func() {
			defer close(c.closed)
			buf := make([]byte, 32*1024)
			for {
				n, err := c.stdout.Read(buf)
				if n > 0 {
					c.bytes.Add(uint64(n))
				}
				if err != nil {
					return
				}
			}
		}()
	}

	// Wait for SSH state to settle: each session needs to register, set its
	// viewport size, etc. ~500 ms is enough at 100 ms tick.
	time.Sleep(500 * time.Millisecond)

	// Set teams: wolf3d wants 1..6 teams. We'll move all players into 2 teams.
	st := srv.State()
	players := st.ListPlayers()
	for i, p := range players {
		st.MovePlayerToTeam(p.ID, i%2)
	}

	// Load game via the registered command (admin context).
	loadOK := make(chan string, 1)
	srv.DispatchCommand(fmt.Sprintf("/game-load %s", *gameName), domain.CommandContext{
		IsConsole: true,
		IsAdmin:   true,
		Reply:     func(s string) { select { case loadOK <- s: default: } },
		Broadcast: func(string) {},
		ServerLog: func(string) {},
	})
	select {
	case msg := <-loadOK:
		if msg != "" {
			fmt.Printf("load reply: %s\n", msg)
		}
	default:
	}

	// Verify game loaded.
	st.RLock()
	loaded := st.GameName
	st.RUnlock()
	if loaded == "" {
		fail("game did not load (check stderr for reason)")
	}
	fmt.Printf("game loaded: %s\n", loaded)

	// Wait through the starting screen (10s) plus a small buffer so all
	// clients enter PhasePlaying before we measure.
	fmt.Printf("warmup %s...\n", *warmup)
	time.Sleep(*warmup)

	// Verify we are actually playing.
	st.RLock()
	phase := st.GamePhase
	st.RUnlock()
	fmt.Printf("phase at measure-start: %v\n", phase)

	// Spawn the GUI-side benchmark (replicates client-local renderCanvas).
	var guiBenchResult *guiBenchOut
	if *guiBench && *nGUI > 0 {
		guiBenchResult = startGUIBench(*nGUI, gamePath, *dataDir, *ptyW, *ptyH)
	}

	// Start CPU profile.
	cpuFile, err := os.Create(*cpuProfile)
	if err != nil {
		fail("create cpuprofile: %v", err)
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		fail("start cpu profile: %v", err)
	}

	// Snapshot before/after metrics.
	before := srv.MetricsSnapshot()
	beforeBytes := make([]uint64, len(conns))
	for i, c := range conns {
		beforeBytes[i] = c.bytes.Load()
	}

	startWall := time.Now()
	time.Sleep(*duration)
	elapsed := time.Since(startWall)

	pprof.StopCPUProfile()
	cpuFile.Close()

	after := srv.MetricsSnapshot()

	// Stop GUI bench.
	var guiSummary string
	if guiBenchResult != nil {
		guiBenchResult.stop()
		guiSummary = guiBenchResult.summarize()
	}

	// Per-client byte rate (proxy for FPS).
	type clientStat struct {
		idx       int
		enhanced  bool
		bytes     uint64
		bytesPerS float64
	}
	stats := make([]clientStat, len(conns))
	for i, c := range conns {
		got := c.bytes.Load() - beforeBytes[i]
		stats[i] = clientStat{
			idx:       c.idx,
			enhanced:  c.enhanced,
			bytes:     got,
			bytesPerS: float64(got) / elapsed.Seconds(),
		}
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].idx < stats[j].idx })

	// Print report.
	fmt.Printf("\n=== profile report ===\n")
	fmt.Printf("measurement: %v\n", elapsed)
	dt := after.At.Sub(before.At).Seconds()
	tickN := after.TickCount - before.TickCount
	fmt.Printf("server ticks: %d (target %.1f Hz, actual %.2f Hz)\n", tickN, 1.0/(*tickInt).Seconds(), float64(tickN)/dt)

	deltaTick := after.TickDurationNs - before.TickDurationNs
	deltaUpdate := after.UpdateDurationNs - before.UpdateDurationNs
	deltaPreRender := after.PreRenderNs - before.PreRenderNs
	deltaSnapshot := after.SnapshotNs - before.SnapshotNs
	deltaBroadcast := after.BroadcastNs - before.BroadcastNs
	deltaCanvas := after.CanvasRenders - before.CanvasRenders
	deltaCanvasNs := after.CanvasRenderNs - before.CanvasRenderNs
	deltaView := after.ViewCalls - before.ViewCalls
	deltaViewNs := after.ViewNs - before.ViewNs

	if tickN > 0 {
		fmt.Printf("\nper-tick averages (n=%d):\n", tickN)
		fmt.Printf("  total       %7.2f ms / tick\n", msPerN(deltaTick, tickN))
		fmt.Printf("  └ update    %7.2f ms / tick\n", msPerN(deltaUpdate, tickN))
		fmt.Printf("  └ preRender %7.2f ms / tick\n", msPerN(deltaPreRender, tickN))
		fmt.Printf("  └ snapshot  %7.2f ms / tick\n", msPerN(deltaSnapshot, tickN))
		fmt.Printf("  └ broadcast %7.2f ms / tick\n", msPerN(deltaBroadcast, tickN))
	}
	if deltaCanvas > 0 {
		fmt.Printf("\nserver-side canvas renders (per-frame, RenderCanvasImage):\n")
		fmt.Printf("  count: %d  rate: %.1f /s  avg: %.2f ms\n",
			deltaCanvas, float64(deltaCanvas)/elapsed.Seconds(), msPerN(deltaCanvasNs, deltaCanvas))
	}
	if deltaView > 0 {
		fmt.Printf("chrome View() calls:\n")
		fmt.Printf("  count: %d  rate: %.1f /s  avg: %.2f ms\n",
			deltaView, float64(deltaView)/elapsed.Seconds(), msPerN(deltaViewNs, deltaView))
	}

	fmt.Printf("\nper-client SSH bytes received (proxy for frame rate):\n")
	totalSSH := 0.0
	totalGUI := 0.0
	cntSSH, cntGUI := 0, 0
	for _, s := range stats {
		role := "ssh "
		if s.enhanced {
			role = "gui "
			totalGUI += s.bytesPerS
			cntGUI++
		} else {
			totalSSH += s.bytesPerS
			cntSSH++
		}
		fmt.Printf("  %s#%02d  %10d B  %8.0f B/s\n", role, s.idx, s.bytes, s.bytesPerS)
	}
	if cntSSH > 0 {
		fmt.Printf("  ssh avg %.0f B/s\n", totalSSH/float64(cntSSH))
	}
	if cntGUI > 0 {
		fmt.Printf("  gui avg %.0f B/s\n", totalGUI/float64(cntGUI))
	}

	if guiSummary != "" {
		fmt.Printf("\nGUI client local-render benchmark (%d goroutines):\n%s", *nGUI, guiSummary)
	}

	fmt.Printf("\nCPU profile written to %s\n", *cpuProfile)
	fmt.Printf("inspect with: go tool pprof -top -cum -nodecount 30 %s\n", *cpuProfile)

	// Clean up.
	for _, c := range conns {
		if c.sess != nil {
			c.sess.Close()
		}
		if c.conn != nil {
			c.conn.Close()
		}
	}
	cancel()
	<-srvErr
}

func msPerN(ns, n uint64) float64 {
	if n == 0 {
		return 0
	}
	return float64(ns) / float64(n) / 1e6
}

type connInfo struct {
	idx      int
	enhanced bool
	bytes    atomic.Uint64
	conn     *gossh.Client
	sess     *gossh.Session
	stdout   io.Reader
	stdin    io.WriteCloser
	closed   chan struct{}
}

func dialClient(addr string, c *connInfo, ptyW, ptyH int) error {
	cfg := &gossh.ClientConfig{
		User:            fmt.Sprintf("p%02d", c.idx),
		Auth:            []gossh.AuthMethod{gossh.Password("")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	conn, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	sess, err := conn.NewSession()
	if err != nil {
		conn.Close()
		return fmt.Errorf("new session: %w", err)
	}
	if c.enhanced {
		_ = sess.Setenv("DEV_NULL_CLIENT", "enhanced")
	}
	_ = sess.Setenv("DEV_NULL_TERM", "ascii")

	modes := gossh.TerminalModes{gossh.ECHO: 0}
	if err := sess.RequestPty("xterm-256color", ptyH, ptyW, modes); err != nil {
		sess.Close()
		conn.Close()
		return fmt.Errorf("pty: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		conn.Close()
		return fmt.Errorf("stdout: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		conn.Close()
		return fmt.Errorf("stdin: %w", err)
	}
	if err := sess.Shell(); err != nil {
		sess.Close()
		conn.Close()
		return fmt.Errorf("shell: %w", err)
	}
	c.conn = conn
	c.sess = sess
	c.stdout = stdout
	c.stdin = stdin
	return nil
}

// guiBench loads the game JS into N independent goja runtimes (the same path
// the client uses for local rendering) and spins each in a goroutine,
// hammering renderCanvas back-to-back. It reports the achieved FPS and the
// average renderCanvas cost.
type guiBenchOut struct {
	stopCh   chan struct{}
	wg       *sync.WaitGroup
	frames   []*atomic.Uint64
	totalNs  []*atomic.Uint64
	startAt  time.Time
	endAt    time.Time
	canvasW  int
	canvasH  int
}

func (g *guiBenchOut) stop() {
	close(g.stopCh)
	g.wg.Wait()
	g.endAt = time.Now()
}

func (g *guiBenchOut) summarize() string {
	dt := g.endAt.Sub(g.startAt).Seconds()
	if dt <= 0 {
		return "  (no time elapsed)\n"
	}
	type row struct {
		idx int
		fps float64
		ms  float64
	}
	rows := make([]row, len(g.frames))
	var sumFps, sumMs float64
	for i := range g.frames {
		f := g.frames[i].Load()
		ns := g.totalNs[i].Load()
		fps := float64(f) / dt
		ms := 0.0
		if f > 0 {
			ms = float64(ns) / float64(f) / 1e6
		}
		rows[i] = row{idx: i, fps: fps, ms: ms}
		sumFps += fps
		sumMs += ms
	}
	out := fmt.Sprintf("  canvas size: %dx%d (px) — same as one GUI viewport\n", g.canvasW, g.canvasH)
	for _, r := range rows {
		out += fmt.Sprintf("  gui-render #%02d  %6.1f fps  %7.2f ms/frame\n", r.idx, r.fps, r.ms)
	}
	if len(rows) > 0 {
		out += fmt.Sprintf("  avg %.1f fps   avg %.2f ms/frame\n", sumFps/float64(len(rows)), sumMs/float64(len(rows)))
	}
	return out
}

func startGUIBench(n int, gamePath, dataDir string, ptyW, ptyH int) *guiBenchOut {
	out := &guiBenchOut{
		stopCh:  make(chan struct{}),
		wg:      &sync.WaitGroup{},
		frames:  make([]*atomic.Uint64, n),
		totalNs: make([]*atomic.Uint64, n),
		startAt: time.Now(),
	}

	// Mirror chrome's blocks-mode canvas size for a 120x40 PTY: gameH ≈ ptyH-12,
	// gameW ≈ ptyW-2; canvas = w*2, h*4. We use a comparable size for the
	// pixels mode (Pixels uses real window pixels, but for a fair "can a GUI
	// client run wolf3d at 60fps" check, the same viewport works.)
	gameW := ptyW - 2
	gameH := ptyH - 12
	if gameH < 8 {
		gameH = 8
	}
	canvasW := gameW * 2
	canvasH := gameH * 4
	out.canvasW = canvasW
	out.canvasH = canvasH

	// Build N independent runtimes, share one chat sink (we're not measuring
	// chat).
	chatSink := make(chan domain.Message, 64)
	go func() {
		for range chatSink {
		}
	}()

	for i := 0; i < n; i++ {
		i := i
		out.frames[i] = &atomic.Uint64{}
		out.totalNs[i] = &atomic.Uint64{}
		rt, err := engine.LoadGame(gamePath, func(string) {}, chatSink, domain.RealClock{}, dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "guiBench load %d: %v\n", i, err)
			continue
		}
		// Set up minimal teams so begin() succeeds.
		if srt, ok := rt.(engine.ScriptRuntime); ok {
			srt.SetTeamsCache([]map[string]any{
				{"name": "Red", "color": "#ff5555", "players": []map[string]any{{"id": fmt.Sprintf("p%02d", i), "name": fmt.Sprintf("p%02d", i)}}},
				{"name": "Blue", "color": "#5555ff", "players": []map[string]any{{"id": fmt.Sprintf("q%02d", i), "name": fmt.Sprintf("q%02d", i)}}},
			})
		}
		rt.Load(nil)
		rt.Begin()
		// Run a few updates so state is populated.
		for j := 0; j < 5; j++ {
			rt.Update(0.1)
		}

		out.wg.Add(1)
		playerID := fmt.Sprintf("p%02d", i)
		go func() {
			defer out.wg.Done()
			defer rt.Unload()
			scratch := make([]byte, 0)
			_ = scratch
			for {
				select {
				case <-out.stopCh:
					return
				default:
				}
				t0 := time.Now()
				rt.RenderCanvasImage(playerID, canvasW, canvasH)
				out.totalNs[i].Add(uint64(time.Since(t0).Nanoseconds()))
				out.frames[i].Add(1)
			}
		}()
	}
	return out
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "profile-load: "+format+"\n", args...)
	os.Exit(1)
}

// keep linter happy on unused imports for cross-platform builds
var _ = render.NewImageBuffer
var _ = runtime.NumGoroutine
