package console

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/input"
	"dev-null/internal/localcmd"
	"dev-null/internal/render"
	"dev-null/internal/state"
	"dev-null/internal/theme"
	"dev-null/internal/widget"
)

// ServerAPI is the interface that the console model uses to interact with the server.
type ServerAPI interface {
	State() *state.CentralState
	Clock() domain.Clock
	DataDir() string
	Uptime() string

	// Communication
	BroadcastChat(msg domain.Message)
	ChatCh() <-chan domain.Message
	SlogCh() <-chan SlogLine

	// Commands
	TabCandidates(input string, playerNames []string) (prefix string, candidates []string)
	DispatchCommand(input string, ctx domain.CommandContext)

	// Console-specific
	SetConsoleWidth(w int)

	// Invite
	InviteLinks() (win, mac string)
}

// taggedLine is a log line with its category.
type taggedLine struct {
	cat  LogCategory
	text string
}

// Model is the Bubble Tea model for the server console.
type Model struct {
	api    ServerAPI
	cancel context.CancelFunc
	width  int
	height int

	inputCtrl *widget.CommandInput
	logView   *widget.TextView
	window    *widget.Window
	screen    *widget.Screen
	menuBar   *widget.MenuBar
	statusBar *widget.StatusBar

	// All log lines (tagged), and display threshold.
	allLines  []taggedLine
	showLevel slog.Level // minimum slog level shown in the console (default Info)

	// Tab completion state (used by tabComplete callback).
	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Per-console theme
	theme     *theme.Theme
	themeName string // file stem used to load theme (empty = default)

	// NC overlay (menus, dialogs)
	overlay widget.OverlayState

	// Init commands from ~/.dev-null/server.txt (dispatched on first tick)
	initCommands []string

	// Per-console plugins
	plugins     []engine.Plugin
	pluginNames []string

	// Per-console shaders (post-processing, run in order)
	shaders     []domain.Shader
	shaderNames []string

	// Reusable render buffer — cleared and resized each frame instead of allocated.
	renderBuf *render.ImageBuffer

	// pendingClipboard is set by commands that want to copy text to the clipboard.
	// Consumed by View() (OSC 52 for TUI) or PopClipboard() (for GUI backend).
	pendingClipboard string

	// profile is the color depth for the console's own terminal.
	// Set from the operator's terminal environment, overridden by --term.
	profile colorprofile.Profile
}

// tea.Msg types for channel-based updates.
type chatLineMsg domain.Message
type slogLineMsg SlogLine

func listenForEvents(chatCh <-chan domain.Message, slogCh <-chan SlogLine) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-chatCh:
			if !ok {
				return nil
			}
			return chatLineMsg(msg)
		case sl, ok := <-slogCh:
			if !ok {
				return nil
			}
			return slogLineMsg(sl)
		}
	}
}

func NewModel(api ServerAPI, cancel context.CancelFunc, profile colorprofile.Profile) *Model {
	input := new(textinput.Model)
	*input = textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	logView := &widget.TextView{BottomAlign: true, Scrollable: true, NoFocus: true}
	inputCtrl := &widget.CommandInput{TextInput: widget.TextInput{Model: input}}

	window := &widget.Window{
		Children: []widget.GridChild{
			{Control: logView, Constraint: widget.GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: widget.FillBoth}, TabIndex: 1},
			{Control: &widget.HDivider{Connected: true}, Constraint: widget.GridConstraint{Col: 0, Row: 1}},
			{Control: inputCtrl, Constraint: widget.GridConstraint{Col: 0, Row: 2, WeightX: 1, Fill: widget.FillHorizontal}, TabIndex: 0},
		},
	}
	window.FocusFirst()

	menuBarCtrl := &widget.MenuBar{}
	statusBarCtrl := &widget.StatusBar{}
	screen := &widget.Screen{
		MenuBar:   menuBarCtrl,
		Window:    window,
		StatusBar: statusBarCtrl,
	}

	m := &Model{
		api:       api,
		cancel:    cancel,
		inputCtrl: inputCtrl,
		logView:   logView,
		window:    window,
		screen:    screen,
		menuBar:   menuBarCtrl,
		statusBar: statusBarCtrl,
		showLevel: slog.LevelInfo,
		theme:     theme.Default(),
		overlay:   widget.OverlayState{OpenMenu: -1},
		profile:   profile,
	}

	// Wire the menu bar to share the model's overlay state.
	m.menuBar.Overlay = &m.overlay

	// Wire callbacks — the NCTextInput handles Enter/Esc/Up/Down/Tab internally.
	inputCtrl.OnSubmit = m.submitInput
	inputCtrl.OnTab = m.tabComplete

	// Load init commands from <ConfigDir>/server.txt; fall back to the
	// legacy ~/.dev-null/server.txt and migrate it on first read.
	if data, ok := readInitFile("server.txt"); ok {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				m.initCommands = append(m.initCommands, line)
			}
		}
	}

	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.inputCtrl.Model.Focus(), listenForEvents(m.api.ChatCh(), m.api.SlogCh()))
}

// ViewBuffer returns the raw render buffer from the last View() call.
// Used by the GUI backend to skip ANSI serialization.
func (m *Model) ViewBuffer() *render.ImageBuffer {
	return m.renderBuf
}

// PopClipboard returns and clears any pending clipboard text.
// Used by the GUI backend to copy text via os/exec.
func (m *Model) PopClipboard() string {
	s := m.pendingClipboard
	m.pendingClipboard = ""
	return s
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(6, msg.Height)
		m.api.SetConsoleWidth(m.width)
		m.resize()
		return m, nil

	case domain.TickMsg:
		// Dispatch init commands from ~/.dev-null/server.txt on the first tick
		// (after the console UI is fully running).
		if len(m.initCommands) > 0 {
			for _, cmd := range m.initCommands {
				m.submitInput(cmd)
			}
			m.initCommands = nil
		}
		return m, nil

	case slogLineMsg:
		m.appendTagged(msg.Cat, msg.Text)
		return m, listenForEvents(m.api.ChatCh(), m.api.SlogCh())

	case chatLineMsg:
		chatMsg := domain.Message(msg)
		var line string
		switch {
		case chatMsg.IsReply:
			line = chatMsg.Text
		case chatMsg.IsPrivate:
			fromName := chatMsg.FromID
			if p := m.api.State().GetPlayer(fromName); p != nil {
				fromName = p.Name
			}
			if fromName == "" {
				fromName = "console"
			}
			toName := chatMsg.ToID
			if p := m.api.State().GetPlayer(toName); p != nil {
				toName = p.Name
			}
			if toName == "" {
				toName = "console"
			}
			line = fmt.Sprintf("[PM %s→%s] %s", fromName, toName, chatMsg.Text)
		case chatMsg.Author == "":
			line = fmt.Sprintf("[system] %s", chatMsg.Text)
		default:
			line = fmt.Sprintf("<%s> %s", chatMsg.Author, chatMsg.Text)
		}
		m.appendTagged(CatChat, line)
		if !chatMsg.IsReply && !chatMsg.IsFromPlugin {
			isSystem := chatMsg.Author == ""
			for _, pl := range m.plugins {
				if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
					m.dispatchPluginReply(reply)
				}
			}
		}
		return m, listenForEvents(m.api.ChatCh(), m.api.SlogCh())

	case domain.GamePhaseMsg, domain.GameLoadedMsg, domain.GameUnloadedMsg, domain.TeamUpdatedMsg, domain.PlayerJoinedMsg, domain.PlayerLeftMsg:
		return m, nil

	case widget.ShowDialogMsg:
		m.overlay.PushDialog(msg.Dialog)
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if m.overlay.HandleClick(msg.X, msg.Y, 0, m.width, m.height, m.consoleMenus(), "") {
				return m, nil
			}
			m.window.HandleClick(msg.X, msg.Y)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// Route scroll events to the panel (NCTextView handles them).
		m.window.HandleUpdate(msg)
		return m, nil
	}

	// Forward other messages to focused control (cursor blink etc.)
	cmd := m.window.HandleUpdate(msg)
	return m, cmd
}

// currentMode returns the current input mode for the router.
func (m *Model) currentMode() input.Mode {
	if m.overlay.HasDialog() {
		return input.ModeDialog
	}
	if m.overlay.MenuFocused || m.overlay.OpenMenu >= 0 {
		return input.ModeMenu
	}
	return input.ModeDesktop
}

// currentFocus returns the focused widget that the router consults for
// WantsEnter / WantsEsc. The console has a single focus hierarchy (the
// main window's children).
func (m *Model) currentFocus() any {
	if m.window == nil {
		return nil
	}
	if m.window.FocusIdx < 0 || m.window.FocusIdx >= len(m.window.Children) {
		return nil
	}
	return m.window.Children[m.window.FocusIdx].Control
}

// handleKey is the console's single entry point for keyboard input. It
// delegates to the shared input.Route and executes the returned Action.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	action := input.Route(key, m.currentMode(), m.currentFocus())
	switch action {
	case input.ActionQuit:
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case input.ActionScrollChatUp, input.ActionScrollChatDown:
		m.logView.Update(msg)
		return m, nil

	case input.ActionCloseTopDialog:
		m.overlay.PopDialog()
		return m, nil

	case input.ActionRouteToDialog:
		_, cmd := m.overlay.HandleDialogMsg(msg)
		return m, cmd

	case input.ActionRouteToMenu:
		m.overlay.HandleKey(key, m.consoleMenus(), "")
		return m, nil

	case input.ActionActivateMenu:
		m.overlay.MenuFocused = true
		m.overlay.MenuCursor = 0
		m.overlay.OpenMenu = -1
		m.overlay.SubMenus = nil
		return m, nil

	case input.ActionFocusChat:
		// Console's "chat input" is the command input at child index 2.
		m.window.FocusIdx = 2
		return m, m.inputCtrl.Model.Focus()

	case input.ActionRouteToFocused:
		return m, m.window.HandleUpdate(msg)
	}
	return m, nil
}

func (m *Model) consoleMenus() []domain.MenuDef {
	return []domain.MenuDef{
		{
			Label: "&File",
			Items: []domain.MenuItemDef{
				{Label: "&Start game", SubItems: m.buildGameSubItems()},
				{Label: "&Load game", SubItems: m.buildLoadGameSubItems()},
				{Label: "---"},
				{Label: "&Themes", SubItems: m.buildThemeSubItems()},
				{Label: "&Plugins", SubItems: m.buildPluginSubItems()},
				{Label: "S&haders", SubItems: m.buildShaderSubItems()},
				{Label: "S&ynths", SubItems: m.buildSynthSubItems()},
				{Label: "&Fonts", SubItems: m.buildFontSubItems()},
				{Label: "---"},
				{Label: "&Invite", SubItems: m.buildInviteSubItems()},
				{Label: "---"},
				{Label: "E&xit", Handler: func(_ string) {
					m.overlay.PushDialog(domain.DialogRequest{
						Title:   "Exit",
						Body:    "Disconnect all players and shut down the server?",
						Buttons: []string{"Yes", "No"},
						Warning: true,
						OnClose: func(btn string) {
							if btn == "Yes" && m.cancel != nil {
								m.cancel()
							}
						},
					})
				}},
			},
		},
		{
			Label: "&View",
			Items: []domain.MenuItemDef{
				{Label: "Show &Debug", Toggle: true, Checked: func() bool { return m.showLevel == slog.LevelDebug }, Handler: func(_ string) {
					m.submitInput("/show-log-level debug")
				}},
				{Label: "Show &Info", Toggle: true, Checked: func() bool { return m.showLevel == slog.LevelInfo }, Handler: func(_ string) {
					m.submitInput("/show-log-level info")
				}},
				{Label: "Show &Warnings", Toggle: true, Checked: func() bool { return m.showLevel == slog.LevelWarn }, Handler: func(_ string) {
					m.submitInput("/show-log-level warn")
				}},
				{Label: "Show &Errors", Toggle: true, Checked: func() bool { return m.showLevel == slog.LevelError }, Handler: func(_ string) {
					m.submitInput("/show-log-level error")
				}},
			},
		},
		{
			Label: "&Help",
			Items: []domain.MenuItemDef{
				{Label: "&About...", Handler: func(_ string) {
					m.overlay.PushDialog(domain.DialogRequest{
						Title:   "About",
						Body:    engine.AboutLogo(),
						Buttons: []string{"OK"},
					})
				}},
			},
		},
	}
}

// ─── Sub-menu builders ───────────────────────────────────────────────────────

func (m *Model) buildGameSubItems() []domain.MenuItemDef {
	m.api.State().RLock()
	currentGame := m.api.State().GameName
	m.api.State().RUnlock()

	return localcmd.BuildGameSubItems(localcmd.GameSubMenuOptions{
		DataDir:     m.api.DataDir(),
		CurrentGame: currentGame,
		OnAdd: func(_ string) {
			localcmd.PushGameAddDialog(&m.overlay, m.api.DataDir(), func(name string) {
				m.submitInput("/game-load " + name)
			})
		},
		OnLoad:   func(name string) { m.submitInput("/game-load " + name) },
		OnDelete: func(name, _ string) { m.showGameRemoveConfirm(name) },
	})
}

func (m *Model) buildThemeSubItems() []domain.MenuItemDef {
	return localcmd.BuildThemeSubItems(localcmd.ThemeSubMenuOptions{
		DataDir:      m.api.DataDir(),
		CurrentTheme: m.themeName,
		OnAdd: func(_ string) {
			localcmd.PushThemeAddDialog(&m.overlay, m.api.DataDir(), func(name string) {
				m.submitInput("/theme-load " + name)
			})
		},
		OnLoad:   func(name string) { m.submitInput("/theme-load " + name) },
		OnDelete: func(name, _ string) { m.showThemeRemoveConfirm(name) },
	})
}

func (m *Model) buildPluginSubItems() []domain.MenuItemDef {
	return m.buildScriptSubItems("plugins", m.pluginNames, "/plugin-load ", "/plugin-unload ")
}

func (m *Model) buildShaderSubItems() []domain.MenuItemDef {
	return m.buildScriptSubItems("shaders", m.shaderNames, "/shader-load ", "/shader-unload ")
}

func (m *Model) buildScriptSubItems(subDir string, loaded []string, loadCmd, unloadCmd string) []domain.MenuItemDef {
	noun := strings.TrimSuffix(subDir, "s")
	if len(noun) > 0 {
		noun = strings.ToUpper(noun[:1]) + noun[1:]
	}
	return localcmd.BuildScriptSubItems(localcmd.ScriptSubMenuOptions{
		DataDir: m.api.DataDir(),
		SubDir:  subDir,
		Loaded:  loaded,
		OnAdd: func(_ string) {
			localcmd.PushScriptAddDialog(&m.overlay, noun, func(name string) {
				m.submitInput(loadCmd + name)
			})
		},
		OnToggle: func(name string, load bool) {
			if load {
				m.submitInput(loadCmd + name)
			} else {
				m.submitInput(unloadCmd + name)
			}
		},
		OnDelete: func(name, _ string) { m.showScriptRemoveConfirm(subDir, name) },
	})
}

func (m *Model) buildSynthSubItems() []domain.MenuItemDef {
	return localcmd.BuildSynthSubItems(localcmd.SynthSubMenuOptions{
		DataDir: m.api.DataDir(),
		OnAdd: func(_ string) {
			localcmd.PushSynthAddDialog(&m.overlay, func(_ string) {})
		},
		OnDelete: func(name, _ string) { m.showSynthRemoveConfirm(name) },
	})
}

func (m *Model) buildFontSubItems() []domain.MenuItemDef {
	return localcmd.BuildFontSubItems(localcmd.FontSubMenuOptions{
		DataDir: m.api.DataDir(),
		OnAdd: func(_ string) {
			localcmd.PushFontAddDialog(&m.overlay, func(_ string) {})
		},
		OnDelete: func(name, _ string) { m.showFontRemoveConfirm(name) },
	})
}

func (m *Model) buildInviteSubItems() []domain.MenuItemDef {
	return []domain.MenuItemDef{
		{Label: "&Windows", Handler: func(_ string) { m.submitInput("/invite-win") }},
		{Label: "&SSH", Handler: func(_ string) { m.submitInput("/invite-ssh") }},
	}
}

// ─── Load-game sub-menu ────────────────────────────────────────────────────

func (m *Model) buildLoadGameSubItems() []domain.MenuItemDef {
	return localcmd.BuildLoadGameSubItems(localcmd.LoadGameSubMenuOptions{
		DataDir: m.api.DataDir(),
		OnLoad: func(gameName, saveName string) {
			m.submitInput("/game-resume " + gameName + "/" + saveName)
		},
		OnDelete: func(gameName, saveName, _ string) {
			m.showSaveRemoveConfirm(gameName, saveName)
		},
	})
}

func (m *Model) showSaveRemoveConfirm(gameName, saveName string) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Save",
		Body:    fmt.Sprintf("Delete save?\n\n  %s/%s\n\nThis cannot be undone.", gameName, saveName),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				state.DeleteSuspend(m.api.DataDir(), gameName, saveName) //nolint:errcheck
			}
		},
	})
}

// ─── Delete confirmation dialogs (used as OnDelete handlers from sub-menus) ──

// scriptExt returns the file extension (".js" or ".lua") for a script in dir.
func scriptExt(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, name+".lua")); err == nil {
		return ".lua"
	}
	return ".js"
}

func (m *Model) showGameRemoveConfirm(name string) {
	gamesDir := filepath.Join(m.api.DataDir(), "games")
	var displayPath string
	if _, err := os.Stat(filepath.Join(gamesDir, name)); err == nil {
		displayPath = name + "/"
	} else {
		displayPath = name + ".js"
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Game",
		Body:    fmt.Sprintf("Delete game from the games folder?\n\n  %s\n\nThis cannot be undone.", displayPath),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				m.api.State().RLock()
				active := m.api.State().GameName
				m.api.State().RUnlock()
				if strings.EqualFold(active, name) {
					m.submitInput("/game-unload")
				}
				if _, err := os.Stat(filepath.Join(gamesDir, name)); err == nil {
					os.RemoveAll(filepath.Join(gamesDir, name))
				} else {
					os.Remove(filepath.Join(gamesDir, name+".js"))
				}
			}
		},
	})
}

func (m *Model) showThemeRemoveConfirm(name string) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Theme File",
		Body:    fmt.Sprintf("Delete '%s.json' from the themes folder?\nThis cannot be undone.", name),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "themes", name+".json"))
			}
		},
	})
}

func (m *Model) showScriptRemoveConfirm(subDir, name string) {
	dir := filepath.Join(m.api.DataDir(), subDir)
	ext := scriptExt(dir, name)
	noun := strings.TrimSuffix(subDir, "s")
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete " + strings.ToUpper(noun[:1]) + noun[1:] + " File",
		Body:    fmt.Sprintf("Delete '%s%s' from the %s folder?\nThis cannot be undone.", name, ext, subDir),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				// Unload first if it's a plugin/shader.
				if subDir == "plugins" {
					m.submitInput("/plugin-unload " + name)
				} else if subDir == "shaders" {
					m.submitInput("/shader-unload " + name)
				}
				os.Remove(filepath.Join(dir, name+ext))
			}
		},
	})
}

func (m *Model) showSynthRemoveConfirm(name string) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete SoundFont",
		Body:    fmt.Sprintf("Delete '%s.sf2' from the soundfonts folder?\nThis cannot be undone.", name),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "soundfonts", name+".sf2"))
			}
		},
	})
}

func (m *Model) showFontRemoveConfirm(name string) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Font",
		Body:    fmt.Sprintf("Delete '%s.flf' from the fonts folder?\nThis cannot be undone.", name),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "fonts", name+".flf"))
			}
		},
	})
}

func (m *Model) View() tea.View {
	EnterRenderPath()
	defer LeaveRenderPath()

	// Set monochrome on all theme layers so widgets use text cursor glyphs.
	m.theme.SetMonochrome(m.profile <= colorprofile.ASCII)

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading server console...")
		view.AltScreen = true
		return view
	}

	t := m.theme
	secondary := t.LayerAt(1) // menus, status bar

	if m.renderBuf == nil {
		m.renderBuf = render.NewImageBuffer(m.width, m.height)
	} else {
		m.renderBuf.EnsureSize(m.width, m.height)
	}
	buf := m.renderBuf

	// Update menu bar and status bar before render.
	menus := m.consoleMenus()
	m.menuBar.Menus = menus

	m.api.State().RLock()
	gameName := m.api.State().GameName
	phase := m.api.State().GamePhase
	m.api.State().RUnlock()
	gameLabel := "none"
	if gameName != "" {
		gameLabel = gameName
		switch phase {
		case domain.PhaseStarting:
			gameLabel += " [starting]"
		case domain.PhasePlaying:
			gameLabel += " [playing]"
		}
	}
	m.statusBar.RightText = fmt.Sprintf("game: %s | players: %d | uptime %s | %s ", gameLabel, m.api.State().PlayerCount(), m.api.Uptime(), m.api.Clock().Now().Format("15:04:05"))

	// Render the full screen: menu bar + window + status bar.
	m.screen.RenderToBuf(buf, 0, 0, m.width, m.height, t)

	// Overlay layers: render to sub-buffers, blit, then recolor for shadow.
	shadowFg := t.ShadowFg
	shadowBg := t.ShadowBg
	if m.overlay.OpenMenu >= 0 {
		if ddBuf, ddCol, ddRow := m.overlay.RenderDropdownBuf(menus, 0, m.width, m.height, secondary); ddBuf != nil {
			buf.Blit(ddCol, ddRow, ddBuf)
			render.BlitShadow(buf, ddCol, ddRow, ddBuf.Width, ddBuf.Height, shadowFg, shadowBg)
			for _, sub := range m.overlay.RenderSubMenusBuf(menus, 0, ddCol, ddBuf.Width, m.width, m.height, secondary) {
				buf.Blit(sub.Col, sub.Row, sub.Buf)
				render.BlitShadow(buf, sub.Col, sub.Row, sub.Buf.Width, sub.Buf.Height, shadowFg, shadowBg)
			}
		}
	}
	if m.overlay.HasDialog() {
		dlgLayer := t.LayerAt(m.overlay.DialogLayer())
		if m.overlay.DialogIsWarning() {
			dlgLayer = t.WarningLayer()
		}
		if sub, col, row := m.overlay.RenderDialogBuf(m.width, m.height, dlgLayer); sub != nil {
			buf.Blit(col, row, sub)
			render.BlitShadow(buf, col, row, sub.Width, sub.Height, shadowFg, shadowBg)
		}
	}

	// Post-processing shaders: run after all layers are composited.
	m.api.State().RLock()
	shaderElapsed := m.api.State().ElapsedSec
	m.api.State().RUnlock()
	engine.ApplyShaders(m.shaders, buf, shaderElapsed)

	view.SetContent(buf.ToString(m.profile))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	// Hide cursor when overlay (menu/dialog) is active.
	if !m.overlay.IsActive() {
		if cx, cy, visible := m.screen.CursorPosition(); visible {
			if cursor := m.inputCtrl.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	}

	return view
}

func (m *Model) resize() {
	// Width is managed by NCWindow.Render via the grid layout.
}

func (m *Model) appendTagged(cat LogCategory, line string) {
	for _, l := range strings.Split(line, "\n") {
		m.allLines = append(m.allLines, taggedLine{cat: cat, text: l})
	}
	if len(m.allLines) > 1000 {
		m.allLines = m.allLines[len(m.allLines)-1000:]
	}
	m.rebuildVisibleLines()
}

// appendLog adds a command/feedback line (for backwards compat with existing callers).
func (m *Model) appendLog(line string) {
	m.appendTagged(CatCommand, line)
}

// catToLevel maps a LogCategory to its slog.Level equivalent.
// CatChat and CatCommand have no slog level; callers must handle them separately.
func catToLevel(cat LogCategory) slog.Level {
	switch cat {
	case CatError:
		return slog.LevelError
	case CatWarn:
		return slog.LevelWarn
	case CatInfo:
		return slog.LevelInfo
	default: // CatDebug
		return slog.LevelDebug
	}
}

func (m *Model) rebuildVisibleLines() {
	var visible []string
	for _, tl := range m.allLines {
		switch tl.cat {
		case CatChat, CatCommand:
			visible = append(visible, tl.text)
		default:
			if catToLevel(tl.cat) >= m.showLevel {
				visible = append(visible, tl.text)
			}
		}
	}
	m.logView.Lines = visible
}
