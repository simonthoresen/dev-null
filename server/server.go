package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishbubbletea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"

	"null-space/common"
)

type App struct {
	state    *CentralState
	registry *commandRegistry
	dataDir  string // root of apps/, plugins/, logs/

	programs   map[string]*tea.Program // key = playerID
	programsMu sync.Mutex

	sessions   map[string]ssh.Session
	sessionsMu sync.RWMutex

	// channels for communicating events to the console
	logCh  chan string         // server log lines
	chatCh chan common.Message // new chat messages

	shutdownFn func()
	sshServer  *ssh.Server

	consoleProgramMu sync.Mutex
	consoleProgram   *tea.Program

	upnpMapping *upnpMapping
}

func New(address, password, dataDir string) (*App, error) {
	app := &App{
		state:    newState(password),
		registry: newCommandRegistry(),
		dataDir:  dataDir,
		programs: make(map[string]*tea.Program),
		sessions: make(map[string]ssh.Session),
		logCh:    make(chan string, 256),
		chatCh:   make(chan common.Message, 256),
	}

	app.registerBuiltins(address)

	server, err := wish.NewServer(
		ssh.EmulatePty(),
		wish.WithAddress(address),
		wish.WithHostKeyPath("null-space_ed25519"),
		wish.WithMiddleware(
			wishbubbletea.MiddlewareWithProgramHandler(app.programHandler),
			app.sessionMiddleware(),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		return nil, err
	}
	app.sshServer = server
	slog.Info("server created", "address", address)
	return app, nil
}

func (a *App) SetShutdownFunc(fn func()) {
	a.shutdownFn = fn
}

func (a *App) State() *CentralState {
	return a.state
}

func (a *App) LogCh() <-chan string {
	return a.logCh
}

func (a *App) ChatCh() <-chan common.Message {
	return a.chatCh
}

func (a *App) Start(ctx context.Context) error {
	go a.runTicker(ctx)

	errCh := make(chan error, 1)
	go func() {
		ln, err := newNoDelayListener(a.sshServer.Addr)
		if err != nil {
			errCh <- err
			return
		}
		slog.Info("TCP_NODELAY listener ready", "address", a.sshServer.Addr)
		err = a.sshServer.Serve(ln)
		if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		slog.Info("server context cancelled, shutting down")
		return a.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = ctx
	slog.Info("server shutdown requested")
	a.upnpMapping.removeMapping()
	if err := a.sshServer.Close(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		slog.Error("server shutdown failed", "error", err)
		return err
	}
	slog.Info("server shutdown completed")
	return nil
}

// SetupUPnP attempts UPnP port mapping and returns true if successful.
// Should be called after New() and before Start().
func (a *App) SetupUPnP(port string) bool {
	a.upnpMapping = tryUPnP(a.state, port)
	return a.state.Net.UPnPMapped
}

// SetupPublicIP detects the public IP and stores it in state.
// Returns the detected IP, or empty string if detection failed.
func (a *App) SetupPublicIP() string {
	publicIP := detectPublicIP()
	if publicIP != "" {
		a.state.mu.Lock()
		a.state.Net.PublicIP = publicIP
		a.state.mu.Unlock()
	}
	return publicIP
}

func (a *App) sessionMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			player := a.registerSession(sess)
			defer a.unregisterSession(player.ID)
			next(sess)
		}
	}
}

func (a *App) programHandler(sess ssh.Session) *tea.Program {
	playerID := sess.Context().SessionID()
	program := tea.NewProgram(newChromeModel(a, playerID), a.sessionProgramOptions(sess)...)
	a.programsMu.Lock()
	a.programs[playerID] = program
	a.programsMu.Unlock()
	return program
}

func (a *App) sessionProgramOptions(sess ssh.Session) []tea.ProgramOption {
	envs := sess.Environ()
	if pty, _, ok := sess.Pty(); ok && pty.Term != "" {
		envs = append(envs, "TERM="+pty.Term)
	}
	opts := wishbubbletea.MakeOptions(sess)
	opts = append(opts,
		tea.WithFPS(60),
		tea.WithColorProfile(colorprofile.Env(envs)),
		tea.WithOutput(newKittyStripWriter(sess)),
	)
	return opts
}

func (a *App) registerSession(sess ssh.Session) *common.Player {
	player := &common.Player{
		ID:   sess.Context().SessionID(),
		Name: a.uniqueName(sess.User()),
	}

	a.sessionsMu.Lock()
	a.sessions[player.ID] = sess
	a.sessionsMu.Unlock()

	a.state.AddPlayer(player)
	slog.Info("player joined", "player_id", player.ID, "name", player.Name)

	joinMsg := common.Message{
		Author: "",
		Text:   fmt.Sprintf("%s joined.", player.Name),
	}
	a.broadcastChat(joinMsg)
	a.broadcastMsg(common.PlayerJoinedMsg{Player: player})

	// notify active app and all plugins
	if a.state.ActiveApp != nil {
		a.state.ActiveApp.OnPlayerJoin(player.ID, player.Name)
	}
	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		p.OnPlayerJoin(player.ID, player.Name)
	}
	return player
}

func (a *App) unregisterSession(playerID string) {
	player := a.state.GetPlayer(playerID)
	if player != nil {
		slog.Info("player left", "player_id", playerID, "name", player.Name)
		leaveMsg := common.Message{
			Author: "",
			Text:   fmt.Sprintf("%s left.", player.Name),
		}
		a.broadcastChat(leaveMsg)
	}

	// notify active app and all plugins
	if a.state.ActiveApp != nil {
		a.state.ActiveApp.OnPlayerLeave(playerID)
	}
	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		p.OnPlayerLeave(playerID)
	}
	a.state.RemovePlayer(playerID)

	a.programsMu.Lock()
	delete(a.programs, playerID)
	a.programsMu.Unlock()

	a.sessionsMu.Lock()
	delete(a.sessions, playerID)
	a.sessionsMu.Unlock()

	a.broadcastMsg(common.PlayerLeftMsg{PlayerID: playerID})
}

func (a *App) runTicker(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.state.mu.Lock()
			a.state.TickN++
			n := a.state.TickN
			a.state.mu.Unlock()
			a.broadcastMsg(common.TickMsg{N: n})
		}
	}
}

func (a *App) broadcastMsg(msg tea.Msg) {
	a.programsMu.Lock()
	progs := make([]*tea.Program, 0, len(a.programs))
	for _, p := range a.programs {
		progs = append(progs, p)
	}
	a.programsMu.Unlock()

	for _, p := range progs {
		go p.Send(msg)
	}

	a.consoleProgramMu.Lock()
	cp := a.consoleProgram
	a.consoleProgramMu.Unlock()
	if cp != nil {
		go cp.Send(msg)
	}
}

func (a *App) broadcastChat(msg common.Message) {
	// run through plugin pipeline before committing
	current := &msg
	plugins, _ := a.state.GetPlugins()
	for _, p := range plugins {
		current = p.OnChatMessage(current)
		if current == nil {
			return // message dropped by a plugin
		}
	}
	msg = *current

	a.state.AddChat(msg)

	select {
	case a.chatCh <- msg:
	default:
	}

	a.broadcastMsg(common.ChatMsg{Msg: msg})
}

func (a *App) sendToPlayer(playerID string, msg tea.Msg) {
	a.programsMu.Lock()
	p := a.programs[playerID]
	a.programsMu.Unlock()
	if p != nil {
		go p.Send(msg)
	}
}

func (a *App) kickPlayer(playerID string) error {
	a.sessionsMu.RLock()
	sess := a.sessions[playerID]
	a.sessionsMu.RUnlock()
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	return sess.Close()
}

func (a *App) serverLog(line string) {
	slog.Info(line)
	select {
	case a.logCh <- line:
	default:
	}
}

func (a *App) uptime() string {
	duration := time.Since(a.state.StartTime).Truncate(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func (a *App) uniqueName(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = "pilot"
	}
	name := base
	index := 2
	for a.state.PlayerByName(name) != nil {
		name = fmt.Sprintf("%s-%d", base, index)
		index++
	}
	return name
}

func (a *App) loadApp(path string) error {
	if a.state.ActiveApp != nil {
		a.unloadApp()
	}

	rt, err := LoadApp(path, a.state, a.serverLog, func(msg common.Message) {
		a.broadcastChat(msg)
	})
	if err != nil {
		return err
	}

	a.state.mu.Lock()
	a.state.ActiveApp = rt
	a.state.AppName = path
	a.state.mu.Unlock()

	// register app commands
	for _, cmd := range rt.Commands() {
		a.registry.Register(cmd)
	}

	// notify existing players
	players := a.state.ListPlayers()
	for _, p := range players {
		rt.OnPlayerJoin(p.ID, p.Name)
	}

	a.broadcastMsg(common.GameLoadedMsg{Name: path})
	a.broadcastChat(common.Message{Text: fmt.Sprintf("App loaded: %s", path)})
	a.serverLog(fmt.Sprintf("app loaded: %s", path))
	return nil
}

func (a *App) unloadApp() {
	a.state.mu.Lock()
	app := a.state.ActiveApp
	a.state.ActiveApp = nil
	a.state.AppName = ""
	a.state.mu.Unlock()

	if app != nil {
		for _, cmd := range app.Commands() {
			a.registry.Unregister(cmd.Name)
		}
		app.Unload()
	}

	a.broadcastMsg(common.GameUnloadedMsg{})
	a.broadcastChat(common.Message{Text: "App unloaded."})
	a.serverLog("app unloaded")
}

func (a *App) loadPlugin(name, path string) error {
	// don't load the same plugin twice
	_, names := a.state.GetPlugins()
	for _, n := range names {
		if n == name {
			return fmt.Errorf("plugin '%s' is already loaded", name)
		}
	}

	p, err := LoadPlugin(path, a.state, a.serverLog, func(msg common.Message) {
		a.broadcastChat(msg)
	})
	if err != nil {
		return err
	}

	a.state.AddPlugin(name, p)
	for _, cmd := range p.Commands() {
		a.registry.Register(cmd)
	}
	a.serverLog(fmt.Sprintf("plugin loaded: %s", name))
	return nil
}

func (a *App) unloadPlugin(name string) error {
	p := a.state.RemovePlugin(name)
	if p == nil {
		return fmt.Errorf("plugin '%s' is not loaded", name)
	}
	for _, cmd := range p.Commands() {
		a.registry.Unregister(cmd.Name)
	}
	p.Unload()
	a.serverLog(fmt.Sprintf("plugin unloaded: %s", name))
	return nil
}

func (a *App) SetConsoleProgram(p *tea.Program) {
	a.consoleProgramMu.Lock()
	a.consoleProgram = p
	a.consoleProgramMu.Unlock()
}

func (a *App) registerBuiltins(address string) {
	a.registry.Register(common.Command{
		Name:        "help",
		Description: "List available commands",
		Handler: func(ctx common.CommandContext, args []string) {
			cmds := a.registry.All()
			sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
			var lines []string
			for _, cmd := range cmds {
				if cmd.AdminOnly && !ctx.IsAdmin {
					continue
				}
				lines = append(lines, fmt.Sprintf("/%s — %s", cmd.Name, cmd.Description))
			}
			ctx.Reply(strings.Join(lines, "\n"))
		},
	})

	a.registry.Register(common.Command{
		Name:        "who",
		Description: "List online players",
		Handler: func(ctx common.CommandContext, args []string) {
			players := a.state.ListPlayers()
			names := make([]string, 0, len(players))
			for _, p := range players {
				label := p.Name
				if p.IsAdmin {
					label += " (admin)"
				}
				names = append(names, label)
			}
			sort.Strings(names)
			ctx.Reply(fmt.Sprintf("Players online (%d): %s", len(names), strings.Join(names, ", ")))
		},
	})

	a.registry.Register(common.Command{
		Name:        "admin",
		Description: "Elevate to admin (/admin <password>)",
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) != 1 {
				ctx.Reply("Usage: /admin <password>")
				return
			}
			a.state.mu.RLock()
			pw := a.state.AdminPassword
			a.state.mu.RUnlock()
			if args[0] != pw {
				ctx.Reply("Invalid password.")
				return
			}
			if ctx.PlayerID != "" {
				a.state.SetPlayerAdmin(ctx.PlayerID, true)
			}
			ctx.Reply("Admin privileges granted.")
		},
	})

	a.registry.Register(common.Command{
		Name:        "password",
		Description: "Change admin password (admin only)",
		AdminOnly:   true,
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) != 1 {
				ctx.Reply("Usage: /password <new>")
				return
			}
			a.state.mu.Lock()
			a.state.AdminPassword = args[0]
			a.state.mu.Unlock()
			ctx.Reply("Admin password changed.")
		},
	})

	a.registry.Register(common.Command{
		Name:             "kick",
		Description:      "Kick a player (admin only)",
		AdminOnly:        true,
		FirstArgIsPlayer: true,
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) < 1 {
				ctx.Reply("Usage: /kick <player>")
				return
			}
			target := a.state.PlayerByName(args[0])
			if target == nil {
				ctx.Reply("Player not found.")
				return
			}
			if err := a.kickPlayer(target.ID); err != nil {
				ctx.Reply(fmt.Sprintf("Kick failed: %v", err))
				return
			}
			ctx.Broadcast(fmt.Sprintf("%s was kicked.", target.Name))
		},
	})

	a.registry.Register(common.Command{
		Name:        "app",
		Description: "App management. No args = list available. /app load|unload [name]",
		Complete: func(before []string) []string {
			switch len(before) {
			case 0:
				return []string{"list", "load", "unload"}
			case 1:
				switch before[0] {
				case "load":
					return listDir(filepath.Join(a.dataDir, "apps"), ".js")
				case "unload":
					a.state.mu.RLock()
					name := a.state.AppName
					a.state.mu.RUnlock()
					if name != "" {
						return []string{strings.TrimSuffix(filepath.Base(name), ".js")}
					}
				}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) == 0 {
				available := listDir(filepath.Join(a.dataDir, "apps"), ".js")
				if len(available) == 0 {
					ctx.Reply("No apps found in apps/")
					return
				}
				a.state.mu.RLock()
				active := a.state.AppName
				a.state.mu.RUnlock()
				var lines []string
				for _, name := range available {
					line := "  " + name
					if strings.HasSuffix(active, name+".js") {
						line += "  [active]"
					}
					lines = append(lines, line)
				}
				ctx.Reply("Available apps:\n" + strings.Join(lines, "\n"))
				return
			}
			switch args[0] {
			case "list":
				available := listDir(filepath.Join(a.dataDir, "apps"), ".js")
				if len(available) == 0 {
					ctx.Reply("No apps found in apps/")
					return
				}
				a.state.mu.RLock()
				active := a.state.AppName
				a.state.mu.RUnlock()
				var lines []string
				for _, name := range available {
					line := "  " + name
					if strings.HasSuffix(active, name+".js") {
						line += "  [active]"
					}
					lines = append(lines, line)
				}
				ctx.Reply("Available apps:\n" + strings.Join(lines, "\n"))
			case "load":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /app load <name>")
					return
				}
				path := filepath.Join(a.dataDir, "apps", args[1]+".js")
				if err := a.loadApp(path); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to load app: %v", err))
					return
				}
				ctx.Reply(fmt.Sprintf("App loaded: %s", args[1]))
			case "unload":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				a.state.mu.RLock()
				active := a.state.AppName
				a.state.mu.RUnlock()
				if active == "" {
					ctx.Reply("No app is currently loaded.")
					return
				}
				a.unloadApp()
				ctx.Reply("App unloaded.")
			default:
				ctx.Reply(fmt.Sprintf("Unknown subcommand '%s'. Use: list, load, unload", args[0]))
			}
		},
	})

	a.registry.Register(common.Command{
		Name:        "plugin",
		Description: "Plugin management. No args = list available. /plugin load|unload <name>",
		Complete: func(before []string) []string {
			switch len(before) {
			case 0:
				return []string{"list", "load", "unload"}
			case 1:
				switch before[0] {
				case "load":
					// offer available plugins not yet loaded
					_, loaded := a.state.GetPlugins()
					loadedSet := make(map[string]bool, len(loaded))
					for _, n := range loaded {
						loadedSet[n] = true
					}
					var out []string
					for _, name := range listDir(filepath.Join(a.dataDir, "plugins"), ".js") {
						if !loadedSet[name] {
							out = append(out, name)
						}
					}
					return out
				case "unload":
					_, names := a.state.GetPlugins()
					return names
				}
			}
			return nil
		},
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) == 0 {
				// List available plugins with loaded marker.
				available := listDir(filepath.Join(a.dataDir, "plugins"), ".js")
				_, loaded := a.state.GetPlugins()
				loadedSet := make(map[string]bool, len(loaded))
				for _, n := range loaded {
					loadedSet[n] = true
				}
				if len(available) == 0 && len(loaded) == 0 {
					ctx.Reply("No plugins found in plugins/")
					return
				}
				var lines []string
				for _, name := range available {
					line := "  " + name
					if loadedSet[name] {
						line += "  [loaded]"
					}
					lines = append(lines, line)
				}
				ctx.Reply("Available plugins:\n" + strings.Join(lines, "\n"))
				return
			}
			switch args[0] {
			case "load":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /plugin load <name>")
					return
				}
				name := args[1]
				path := filepath.Join(a.dataDir, "plugins", name+".js")
				if err := a.loadPlugin(name, path); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to load plugin: %v", err))
					return
				}
				ctx.Reply(fmt.Sprintf("Plugin loaded: %s", name))
			case "unload":
				if !ctx.IsAdmin {
					ctx.Reply("Permission denied (admin only)")
					return
				}
				if len(args) < 2 {
					ctx.Reply("Usage: /plugin unload <name>")
					return
				}
				if err := a.unloadPlugin(args[1]); err != nil {
					ctx.Reply(fmt.Sprintf("Failed to unload plugin: %v", err))
					return
				}
				ctx.Reply(fmt.Sprintf("Plugin unloaded: %s", args[1]))
			case "list":
				_, names := a.state.GetPlugins()
				if len(names) == 0 {
					ctx.Reply("No plugins currently loaded.")
					return
				}
				ctx.Reply("Loaded plugins: " + strings.Join(names, ", "))
			default:
				ctx.Reply(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", args[0]))
			}
		},
	})

	a.registry.Register(common.Command{
		Name:             "msg",
		Description:      "Send a private message to a player",
		FirstArgIsPlayer: true,
		Handler: func(ctx common.CommandContext, args []string) {
			if len(args) < 2 {
				ctx.Reply("Usage: /msg <player> <text>")
				return
			}
			target := a.state.PlayerByName(args[0])
			if target == nil {
				ctx.Reply("Player not found.")
				return
			}
			text := strings.Join(args[1:], " ")
			fromName := "admin"
			if ctx.PlayerID != "" {
				if p := a.state.GetPlayer(ctx.PlayerID); p != nil {
					fromName = p.Name
				}
			}
			pm := common.Message{
				Author:    fromName,
				Text:      text,
				IsPrivate: true,
				ToID:      target.ID,
				FromID:    ctx.PlayerID,
			}
			// Send to recipient
			a.sendToPlayer(target.ID, common.ChatMsg{Msg: pm})
			// Confirm to sender
			ctx.Reply(fmt.Sprintf("[PM to %s] %s", target.Name, text))
			// Log to console
			ctx.ServerLog(fmt.Sprintf("[PM %s -> %s] %s", fromName, target.Name, text))

			select {
			case a.chatCh <- pm:
			default:
			}
		},
	})

	_ = address // used for future connection info commands
}

func (a *App) MakeCommandContext(playerID string) common.CommandContext {
	isAdmin := false
	if playerID == "" {
		isAdmin = true
	} else if p := a.state.GetPlayer(playerID); p != nil {
		isAdmin = p.IsAdmin
	}
	return common.CommandContext{
		PlayerID: playerID,
		IsAdmin:  isAdmin,
		Reply: func(text string) {
			msg := common.Message{
				Text:      text,
				IsPrivate: true,
				IsReply:   true,
				ToID:      playerID,
			}
			if playerID == "" {
				a.consoleProgramMu.Lock()
				cp := a.consoleProgram
				a.consoleProgramMu.Unlock()
				if cp != nil {
					go cp.Send(common.ChatMsg{Msg: msg})
				}
			} else {
				a.sendToPlayer(playerID, common.ChatMsg{Msg: msg})
			}
		},
		Broadcast: func(text string) {
			a.broadcastChat(common.Message{Text: text})
		},
		ServerLog: func(text string) {
			a.serverLog(text)
		},
	}
}

// listDir returns the name (without extension) of every file in dir that ends
// with ext, sorted alphabetically.
func listDir(dir, ext string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(names)
	return names
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// noDelayListener wraps a net.Listener and ensures TCP_NODELAY is set on
// every accepted connection, disabling Nagle's algorithm so that single
// keystrokes are sent immediately without waiting for more data.
type noDelayListener struct {
	net.Listener
}

func newNoDelayListener(address string) (net.Listener, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &noDelayListener{Listener: ln}, nil
}

func (l *noDelayListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	return conn, nil
}
