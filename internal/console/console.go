package console

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/internal/domain"
	"null-space/internal/render"
	"null-space/internal/engine"
	"null-space/internal/state"
	"null-space/internal/theme"
	"null-space/internal/widget"
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

	// All log lines (tagged), and filter state.
	allLines []taggedLine
	filter   map[LogCategory]bool // true = show this category

	// Tab completion state (used by tabComplete callback).
	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Per-console theme
	theme *theme.Theme

	// NC overlay (menus, dialogs)
	overlay widget.OverlayState

	// Init commands from ~/.null-space/server.txt (dispatched on first tick)
	initCommands []string

	// Per-console plugins
	plugins     []*engine.JSPlugin
	pluginNames []string

	// Per-console shaders (post-processing, run in order)
	shaders     []domain.Shader
	shaderNames []string
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

func NewModel(api ServerAPI, cancel context.CancelFunc) *Model {
	input := new(textinput.Model)
	*input = textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	logView := &widget.TextView{BottomAlign: true, Scrollable: true}
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
		filter: map[LogCategory]bool{
			CatInfo:    true,
			CatWarn:    true,
			CatError:   true,
			CatChat:    true,
			CatCommand: true,
			// CatDebug defaults to false
		},
		theme:   theme.Default(),
		overlay: widget.OverlayState{OpenMenu: -1},
	}

	// Wire the menu bar to share the model's overlay state.
	m.menuBar.Overlay = &m.overlay

	// Wire callbacks — the NCTextInput handles Enter/Esc/Up/Down/Tab internally.
	inputCtrl.OnSubmit = m.submitInput
	inputCtrl.OnTab = m.tabComplete

	// Load init commands from ~/.null-space/server.txt if it exists.
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".null-space", "server.txt")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					m.initCommands = append(m.initCommands, line)
				}
			}
		}
	}

	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.inputCtrl.Model.Focus(), listenForEvents(m.api.ChatCh(), m.api.SlogCh()))
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
		// Dispatch init commands from ~/.null-space/server.txt on the first tick
		// (after the console UI is fully running).
		if len(m.initCommands) > 0 {
			for _, cmd := range m.initCommands {
				m.submitInput(cmd)
			}
			m.initCommands = nil
		}
		engine.UpdateShaders(m.shaders, 0.1)
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
		// Hard exit on Ctrl+C / Ctrl+D — always works, bypasses everything.
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+d" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		// Let the overlay handle F10/menu/dialog keys first.
		if m.overlay.HandleKey(msg.String(), m.consoleMenus(), "") {
			return m, nil
		}
		// Everything else goes to the focused window control.
		cmd := m.window.HandleUpdate(msg)
		return m, cmd

	case tea.MouseWheelMsg:
		// Route scroll events to the panel (NCTextView handles them).
		m.window.HandleUpdate(msg)
		return m, nil
	}

	// Forward other messages to focused control (cursor blink etc.)
	cmd := m.window.HandleUpdate(msg)
	return m, cmd
}

func (m *Model) consoleMenus() []domain.MenuDef {
	return []domain.MenuDef{
		{
			Label: "&File",
			Items: []domain.MenuItemDef{
				{Label: "&Themes...", Handler: func(_ string) { m.showListDialog("Themes", "themes", ".json") }},
				{Label: "&Plugins...", Handler: func(_ string) { m.showListDialog("Plugins", "plugins", ".js") }},
				{Label: "&Shaders...", Handler: func(_ string) { m.showShaderDialog() }},
				{Label: "&Games...", Handler: func(_ string) { m.showListDialog("Games", "games", ".js") }},
				{Label: "---"},
				{Label: "E&xit", Hotkey: "ctrl+q", Handler: func(_ string) {
					m.overlay.PushDialog(domain.DialogRequest{
						Title:   "Exit",
						Body:    "Are you sure you want to shut down the server?",
						Buttons: []string{"Yes", "No"},
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
				{Label: "&Debug", Toggle: true, Checked: func() bool { return m.filter[CatDebug] }, Handler: func(_ string) {
					m.filter[CatDebug] = !m.filter[CatDebug]; m.rebuildVisibleLines()
				}},
				{Label: "&Info", Toggle: true, Checked: func() bool { return m.filter[CatInfo] }, Handler: func(_ string) {
					m.filter[CatInfo] = !m.filter[CatInfo]; m.rebuildVisibleLines()
				}},
				{Label: "&Warnings", Toggle: true, Checked: func() bool { return m.filter[CatWarn] }, Handler: func(_ string) {
					m.filter[CatWarn] = !m.filter[CatWarn]; m.rebuildVisibleLines()
				}},
				{Label: "&Errors", Toggle: true, Checked: func() bool { return m.filter[CatError] }, Handler: func(_ string) {
					m.filter[CatError] = !m.filter[CatError]; m.rebuildVisibleLines()
				}},
				{Label: "---"},
				{Label: "&Chat", Toggle: true, Checked: func() bool { return m.filter[CatChat] }, Handler: func(_ string) {
					m.filter[CatChat] = !m.filter[CatChat]; m.rebuildVisibleLines()
				}},
				{Label: "C&ommands", Toggle: true, Checked: func() bool { return m.filter[CatCommand] }, Handler: func(_ string) {
					m.filter[CatCommand] = !m.filter[CatCommand]; m.rebuildVisibleLines()
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

// showListDialog opens a dialog listing available items from a dist/ subdirectory.
func (m *Model) showShaderDialog() {
	available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	var lines []string
	if len(m.shaderNames) > 0 {
		lines = append(lines, "Active (in order):")
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Available:")
	if len(available) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, name := range available {
			tag := ""
			if loadedSet[name] {
				tag = "  [active]"
			}
			lines = append(lines, "  "+name+tag)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Use /shader load|unload|up|down <name>")

	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Shaders",
		Body:    strings.Join(lines, "\n"),
		Buttons: []string{"Close"},
	})
}

func (m *Model) showListDialog(title, subdir, ext string) {
	dir := filepath.Join(m.api.DataDir(), subdir)
	items := engine.ListDir(dir, ext)
	body := "(empty)"
	if len(items) > 0 {
		var lines []string
		for _, name := range items {
			lines = append(lines, "  "+name)
		}
		body = strings.Join(lines, "\n")
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   title,
		Body:    body,
		Buttons: []string{"Add", "Remove", "Close"},
		OnClose: func(btn string) {
			switch btn {
			case "Add":
				m.showAddDialog(title, subdir, ext)
			case "Remove":
				m.showRemoveDialog(title, subdir, ext, items)
			}
		},
	})
}

// showAddDialog asks for a URL or filename to add.
func (m *Model) showAddDialog(title, subdir, ext string) {
	// Use the command input to get user input — chain back via a command.
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Add " + title[:len(title)-1], // "Themes" -> "Theme"
		Body:    "Type a /command to add:\n\n  For games:   /game load <name or url>\n  For plugins: /plugin load <name or url>\n  For themes:  /theme <name>",
		Buttons: []string{"OK"},
		OnClose: func(_ string) {
			m.showListDialog(title, subdir, ext)
		},
	})
}

// showRemoveDialog asks which item to remove and confirms.
func (m *Model) showRemoveDialog(title, subdir, ext string, items []string) {
	if len(items) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Remove",
			Body:    "No items to remove.",
			Buttons: []string{"OK"},
			OnClose: func(_ string) {
				m.showListDialog(title, subdir, ext)
			},
		})
		return
	}
	body := "Select item to remove:\n"
	for i, name := range items {
		body += fmt.Sprintf("\n  %d. %s", i+1, name)
	}
	body += "\n\nType the number in the command bar, or press Close."

	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Remove " + title[:len(title)-1],
		Body:    body,
		Buttons: []string{"Close"},
		OnClose: func(_ string) {
			m.showListDialog(title, subdir, ext)
		},
	})
}

func (m *Model) View() tea.View {
	EnterRenderPath()
	defer LeaveRenderPath()

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading server console...")
		view.AltScreen = true
		return view
	}

	t := m.theme
	secondary := t.LayerAt(1) // menus, status bar

	buf := render.NewImageBuffer(m.width, m.height)

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
		case domain.PhaseSplash:
			gameLabel += " [splash]"
		case domain.PhasePlaying:
			gameLabel += " [playing]"
		case domain.PhaseGameOver:
			gameLabel += " [game over]"
		}
	}
	m.statusBar.RightText = fmt.Sprintf("game: %s | players: %d | uptime %s | %s", gameLabel, m.api.State().PlayerCount(), m.api.Uptime(), time.Now().Format("15:04:05"))

	// Render the full screen: menu bar + window + status bar.
	m.screen.RenderToBuf(buf, 0, 0, m.width, m.height, t)

	// Post-processing shaders: run in sequence on the fully-rendered buffer.
	engine.ApplyShaders(m.shaders, buf)

	// Overlay layers: render to sub-buffers, blit, then recolor for shadow.
	shadowFg := t.ShadowFgC()
	shadowBg := t.ShadowBgC()
	if m.overlay.OpenMenu >= 0 {
		if dd := m.overlay.RenderDropdown(menus, 0, secondary); dd.Content != "" {
			sub := render.NewImageBuffer(dd.Width, dd.Height)
			sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, secondary.FgC(), secondary.BgC())
			buf.Blit(dd.Col, dd.Row, sub)
			render.BlitShadow(buf, dd.Col, dd.Row, dd.Width, dd.Height, shadowFg, shadowBg)
		}
	}
	if m.overlay.HasDialog() {
		if dlg := m.overlay.RenderDialog(m.width, m.height, t.LayerAt(2)); dlg.Content != "" {
			sub := render.NewImageBuffer(dlg.Width, dlg.Height)
			dlgLayer := t.LayerAt(2)
			sub.PaintANSI(0, 0, dlg.Width, dlg.Height, dlg.Content, dlgLayer.FgC(), dlgLayer.BgC())
			buf.Blit(dlg.Col, dlg.Row, sub)
			render.BlitShadow(buf, dlg.Col, dlg.Row, dlg.Width, dlg.Height, shadowFg, shadowBg)
		}
	}

	view.SetContent(buf.ToString())
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

func (m *Model) rebuildVisibleLines() {
	var visible []string
	for _, tl := range m.allLines {
		if m.filter[tl.cat] {
			visible = append(visible, tl.text)
		}
	}
	m.logView.Lines = visible
}

// tabComplete handles tab completion for the input control.
func (m *Model) tabComplete(current string) (string, bool) {
	if !strings.HasPrefix(current, "/") {
		return "", false
	}
	if m.tabCandidates == nil {
		m.tabPrefix, m.tabCandidates = m.api.TabCandidates(current, m.api.State().PlayerNames())
		m.tabIndex = 0
	}
	if len(m.tabCandidates) == 0 {
		return "", false
	}
	result := m.tabPrefix + m.tabCandidates[m.tabIndex]
	m.tabIndex = (m.tabIndex + 1) % len(m.tabCandidates)
	return result, true
}

// submitInput is called by NCTextInput.OnSubmit when Enter is pressed.
// text is already trimmed and non-empty; input is already cleared; history is already added.
func (m *Model) submitInput(text string) {
	// Reset tab completion on submit.
	m.tabCandidates = nil

	// Echo the command to the log so the user sees what was typed.
	m.appendLog("> " + text)

	if !strings.HasPrefix(text, "/") {
		m.appendLog("Type /help for available commands.")
		return
	}
	if strings.HasPrefix(text, "/plugin") {
		m.handlePluginCommand(text)
		return
	}
	if strings.HasPrefix(text, "/theme") {
		m.handleThemeCommand(text)
		return
	}
	if strings.HasPrefix(text, "/shader") {
		m.handleShaderCommand(text)
		return
	}
	ctx := domain.CommandContext{
		PlayerID:  "",
		IsConsole: true,
		IsAdmin:   true,
		Reply: func(s string) {
			m.appendLog(s)
		},
		Broadcast: func(s string) {
			m.api.BroadcastChat(domain.Message{Text: s})
		},
		ServerLog: func(s string) {
			m.appendLog(s)
		},
	}
	m.api.DispatchCommand(text, ctx)
}

func (m *Model) handleThemeCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := theme.ListThemes(m.api.DataDir())
		if len(available) == 0 {
			m.appendLog("No themes found in themes/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if strings.EqualFold(name, m.theme.Name) {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.appendLog("Available themes:\n" + strings.Join(lines, "\n"))
		return
	}
	name := parts[1]
	path := filepath.Join(m.api.DataDir(), "themes", name+".json")
	t, err := theme.Load(path)
	if err != nil {
		m.appendLog(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.appendLog(fmt.Sprintf("Theme changed to: %s", t.Name))
}

func (m *Model) handlePluginCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := engine.ListDir(filepath.Join(m.api.DataDir(), "plugins"), ".js")
		loadedSet := make(map[string]bool)
		for _, n := range m.pluginNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.pluginNames) == 0 {
			m.appendLog("No plugins found in plugins/")
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
		m.appendLog("Available plugins:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.appendLog("Usage: /plugin load <name|url>")
			return
		}
		name, path, err := engine.ResolvePluginPath(parts[2], m.api.DataDir())
		if err != nil {
			m.appendLog(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.pluginNames {
			if strings.EqualFold(n, name) {
				m.appendLog(fmt.Sprintf("Plugin '%s' is already loaded.", name))
				return
			}
		}
		pl, err := engine.LoadPlugin(path, m.api.Clock())
		if err != nil {
			m.appendLog(fmt.Sprintf("Failed to load plugin: %v", err))
			return
		}
		m.plugins = append(m.plugins, pl)
		m.pluginNames = append(m.pluginNames, name)
		m.appendLog(fmt.Sprintf("Plugin loaded: %s", name))
	case "unload":
		if len(parts) < 3 {
			m.appendLog("Usage: /plugin unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.pluginNames {
			if strings.EqualFold(n, target) {
				m.plugins[i].Unload()
				m.plugins = append(m.plugins[:i], m.plugins[i+1:]...)
				m.pluginNames = append(m.pluginNames[:i], m.pluginNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.appendLog(fmt.Sprintf("Plugin '%s' is not loaded.", target))
			return
		}
		m.appendLog(fmt.Sprintf("Plugin unloaded: %s", target))
	case "list":
		if len(m.pluginNames) == 0 {
			m.appendLog("No plugins currently loaded.")
			return
		}
		m.appendLog("Loaded plugins: " + strings.Join(m.pluginNames, ", "))
	default:
		m.appendLog(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list", parts[1]))
	}
}

// dispatchPluginReply handles a string returned by a console plugin's onMessage hook.
// If it starts with "/" it's treated as a command, otherwise logged as info.
func (m *Model) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		ctx := domain.CommandContext{
			PlayerID:  "",
			IsConsole: true,
			IsAdmin:   true,
			Reply: func(s string) {
				m.appendLog(s)
			},
			Broadcast: func(s string) {
				m.api.BroadcastChat(domain.Message{Text: s, IsFromPlugin: true})
			},
			ServerLog: func(s string) {
				m.appendLog(s)
			},
		}
		m.api.DispatchCommand(text, ctx)
		return
	}
	// Plain text from console plugin -> broadcast as admin chat.
	m.api.BroadcastChat(domain.Message{Author: "admin", Text: text, IsFromPlugin: true})
}

func (m *Model) handleShaderCommand(input string) {
	parts := strings.Fields(input)
	// /shader with no args -> list
	if len(parts) <= 1 {
		available := engine.ListDir(filepath.Join(m.api.DataDir(), "shaders"), ".js")
		loadedSet := make(map[string]bool)
		for _, n := range m.shaderNames {
			loadedSet[n] = true
		}
		if len(available) == 0 && len(m.shaderNames) == 0 {
			m.appendLog("No shaders found in shaders/")
			return
		}
		var lines []string
		for _, name := range available {
			line := "  " + name
			if loadedSet[name] {
				line += "  [active]"
			}
			lines = append(lines, line)
		}
		m.appendLog("Available shaders:\n" + strings.Join(lines, "\n"))
		return
	}
	switch parts[1] {
	case "load":
		if len(parts) < 3 {
			m.appendLog("Usage: /shader load <name|url>")
			return
		}
		nameOrURL := parts[2]
		name, path, err := engine.ResolveShaderPath(nameOrURL, m.api.DataDir())
		if err != nil {
			m.appendLog(fmt.Sprintf("Failed: %v", err))
			return
		}
		for _, n := range m.shaderNames {
			if strings.EqualFold(n, name) {
				m.appendLog(fmt.Sprintf("Shader '%s' is already loaded.", name))
				return
			}
		}
		sh, err := engine.LoadShader(path, m.api.Clock())
		if err != nil {
			m.appendLog(fmt.Sprintf("Failed to load shader: %v", err))
			return
		}
		m.shaders = append(m.shaders, sh)
		m.shaderNames = append(m.shaderNames, name)
		m.appendLog(fmt.Sprintf("Shader loaded: %s", name))
	case "unload":
		if len(parts) < 3 {
			m.appendLog("Usage: /shader unload <name>")
			return
		}
		target := parts[2]
		found := false
		for i, n := range m.shaderNames {
			if strings.EqualFold(n, target) {
				m.shaders[i].Unload()
				m.shaders = append(m.shaders[:i], m.shaders[i+1:]...)
				m.shaderNames = append(m.shaderNames[:i], m.shaderNames[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.appendLog(fmt.Sprintf("Shader '%s' is not loaded.", target))
			return
		}
		m.appendLog(fmt.Sprintf("Shader unloaded: %s", target))
	case "list":
		if len(m.shaderNames) == 0 {
			m.appendLog("No shaders currently loaded.")
			return
		}
		var lines []string
		for i, name := range m.shaderNames {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
		}
		m.appendLog("Active shaders (in order):\n" + strings.Join(lines, "\n"))
	case "up":
		if len(parts) < 3 {
			m.appendLog("Usage: /shader up <name>")
			return
		}
		m.moveShader(parts[2], -1)
	case "down":
		if len(parts) < 3 {
			m.appendLog("Usage: /shader down <name>")
			return
		}
		m.moveShader(parts[2], +1)
	default:
		m.appendLog(fmt.Sprintf("Unknown subcommand '%s'. Use: load, unload, list, up, down", parts[1]))
	}
}

func (m *Model) moveShader(name string, delta int) {
	idx := -1
	for i, n := range m.shaderNames {
		if strings.EqualFold(n, name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.appendLog(fmt.Sprintf("Shader '%s' is not loaded.", name))
		return
	}
	newIdx := idx + delta
	if newIdx < 0 || newIdx >= len(m.shaderNames) {
		m.appendLog(fmt.Sprintf("Shader '%s' is already at position %d.", name, idx+1))
		return
	}
	m.shaders[idx], m.shaders[newIdx] = m.shaders[newIdx], m.shaders[idx]
	m.shaderNames[idx], m.shaderNames[newIdx] = m.shaderNames[newIdx], m.shaderNames[idx]
	m.appendLog(fmt.Sprintf("Shader '%s' moved to position %d.", name, newIdx+1))
}
