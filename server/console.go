package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"null-space/common"
)

// logCategory tags console log lines for filtering.
type logCategory int

const (
	catDebug   logCategory = iota
	catInfo
	catWarn
	catError
	catChat    // player chat, system messages
	catCommand // "> /help" echo and command replies
)

// taggedLine is a log line with its category.
type taggedLine struct {
	cat  logCategory
	text string
}

type consoleModel struct {
	app    *Server
	cancel context.CancelFunc
	width  int
	height int

	inputCtrl *NCCommandInput
	logView   *NCTextView
	window    *NCWindow

	// All log lines (tagged), and filter state.
	allLines []taggedLine
	filter   map[logCategory]bool // true = show this category

	// Tab completion state (used by tabComplete callback).
	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Per-console theme
	theme *Theme

	// NC overlay (menus, dialogs)
	overlay overlayState

	// Init commands from ~/.null-space/server.txt (dispatched on first tick)
	initCommands []string

	// Per-console plugins
	plugins     []*jsPlugin
	pluginNames []string
}

func NewConsoleModel(app *Server, cancel context.CancelFunc) *consoleModel {
	input := new(textinput.Model)
	*input = textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	logView := &NCTextView{BottomAlign: true, Scrollable: true}
	inputCtrl := &NCCommandInput{NCTextInput: NCTextInput{Model: input}}

	window := &NCWindow{
		Children: []GridChild{
			{Control: logView, Constraint: GridConstraint{Col: 0, Row: 0, WeightX: 1, WeightY: 1, Fill: FillBoth}, TabIndex: 1},
			{Control: &NCHDivider{Connected: true}, Constraint: GridConstraint{Col: 0, Row: 1}},
			{Control: inputCtrl, Constraint: GridConstraint{Col: 0, Row: 2, WeightX: 1, Fill: FillHorizontal}, TabIndex: 0},
		},
	}
	window.FocusFirst()

	m := &consoleModel{
		app:       app,
		cancel:    cancel,
		inputCtrl: inputCtrl,
		logView:   logView,
		window:    window,
		filter: map[logCategory]bool{
			catInfo:    true,
			catWarn:    true,
			catError:   true,
			catChat:    true,
			catCommand: true,
			// catDebug defaults to false
		},
		theme:     DefaultTheme(),
		overlay:   overlayState{openMenu: -1},
	}

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

func (m *consoleModel) Init() tea.Cmd {
	return tea.Batch(m.inputCtrl.Model.Focus(), listenForEvents(m.app.ChatCh(), m.app.SlogCh()))
}

func (m *consoleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(40, msg.Width)
		m.height = max(6, msg.Height)
		m.app.consoleWidth = m.width
		m.resize()
		return m, nil

	case common.TickMsg:
		// Dispatch init commands from ~/.null-space/server.txt on the first tick
		// (after the console UI is fully running).
		if len(m.initCommands) > 0 {
			for _, cmd := range m.initCommands {
				m.submitInput(cmd)
			}
			m.initCommands = nil
		}
		return m, nil

	case slogLineMsg:
		m.appendTagged(msg.cat, msg.text)
		return m, listenForEvents(m.app.ChatCh(), m.app.SlogCh())

	case chatLineMsg:
		chatMsg := common.Message(msg)
		var line string
		switch {
		case chatMsg.IsReply:
			line = chatMsg.Text
		case chatMsg.IsPrivate:
			fromName := chatMsg.FromID
			if p := m.app.state.GetPlayer(fromName); p != nil {
				fromName = p.Name
			}
			if fromName == "" {
				fromName = "console"
			}
			toName := chatMsg.ToID
			if p := m.app.state.GetPlayer(toName); p != nil {
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
		m.appendTagged(catChat, line)
		if !chatMsg.IsReply {
			isSystem := chatMsg.Author == ""
			for _, pl := range m.plugins {
				if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
					m.dispatchPluginReply(reply)
				}
			}
		}
		return m, listenForEvents(m.app.ChatCh(), m.app.SlogCh())

	case common.GamePhaseMsg, common.GameLoadedMsg, common.GameUnloadedMsg, common.TeamUpdatedMsg, common.PlayerJoinedMsg, common.PlayerLeftMsg:
		return m, nil

	case showDialogMsg:
		m.overlay.pushDialog(msg.dialog)
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if m.overlay.handleClick(msg.X, msg.Y, 0, m.width, m.height, m.consoleMenus(), "") {
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
		if m.overlay.handleKey(msg.String(), m.consoleMenus(), "") {
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

func (m *consoleModel) consoleMenus() []common.MenuDef {
	return []common.MenuDef{
		{
			Label: "&File",
			Items: []common.MenuItemDef{
				{Label: "&Themes...", Handler: func(_ string) { m.showListDialog("Themes", "themes", ".json") }},
				{Label: "&Plugins...", Handler: func(_ string) { m.showListDialog("Plugins", "plugins", ".js") }},
				{Label: "&Games...", Handler: func(_ string) { m.showListDialog("Games", "games", ".js") }},
				{Label: "---"},
				{Label: "E&xit", Hotkey: "ctrl+q", Handler: func(_ string) {
					m.overlay.pushDialog(common.DialogRequest{
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
			Items: []common.MenuItemDef{
				{Label: "&Debug", Toggle: true, Checked: func() bool { return m.filter[catDebug] }, Handler: func(_ string) {
					m.filter[catDebug] = !m.filter[catDebug]; m.rebuildVisibleLines()
				}},
				{Label: "&Info", Toggle: true, Checked: func() bool { return m.filter[catInfo] }, Handler: func(_ string) {
					m.filter[catInfo] = !m.filter[catInfo]; m.rebuildVisibleLines()
				}},
				{Label: "&Warnings", Toggle: true, Checked: func() bool { return m.filter[catWarn] }, Handler: func(_ string) {
					m.filter[catWarn] = !m.filter[catWarn]; m.rebuildVisibleLines()
				}},
				{Label: "&Errors", Toggle: true, Checked: func() bool { return m.filter[catError] }, Handler: func(_ string) {
					m.filter[catError] = !m.filter[catError]; m.rebuildVisibleLines()
				}},
				{Label: "---"},
				{Label: "&Chat", Toggle: true, Checked: func() bool { return m.filter[catChat] }, Handler: func(_ string) {
					m.filter[catChat] = !m.filter[catChat]; m.rebuildVisibleLines()
				}},
				{Label: "C&ommands", Toggle: true, Checked: func() bool { return m.filter[catCommand] }, Handler: func(_ string) {
					m.filter[catCommand] = !m.filter[catCommand]; m.rebuildVisibleLines()
				}},
			},
		},
		{
			Label: "&Help",
			Items: []common.MenuItemDef{
				{Label: "&About...", Handler: func(_ string) {
					m.overlay.pushDialog(common.DialogRequest{
						Title:   "About",
						Body:    aboutLogo(),
						Buttons: []string{"OK"},
					})
				}},
			},
		},
	}
}

// showListDialog opens a dialog listing available items from a dist/ subdirectory.
func (m *consoleModel) showListDialog(title, subdir, ext string) {
	dir := filepath.Join(m.app.dataDir, subdir)
	items := listDir(dir, ext)
	body := "(empty)"
	if len(items) > 0 {
		var lines []string
		for _, name := range items {
			lines = append(lines, "  "+name)
		}
		body = strings.Join(lines, "\n")
	}
	m.overlay.pushDialog(common.DialogRequest{
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
func (m *consoleModel) showAddDialog(title, subdir, ext string) {
	// Use the command input to get user input — chain back via a command.
	m.overlay.pushDialog(common.DialogRequest{
		Title:   "Add " + title[:len(title)-1], // "Themes" → "Theme"
		Body:    "Type a /command to add:\n\n  For games:   /game load <name or url>\n  For plugins: /plugin load <name or url>\n  For themes:  /theme <name>",
		Buttons: []string{"OK"},
		OnClose: func(_ string) {
			m.showListDialog(title, subdir, ext)
		},
	})
}

// showRemoveDialog asks which item to remove and confirms.
func (m *consoleModel) showRemoveDialog(title, subdir, ext string, items []string) {
	if len(items) == 0 {
		m.overlay.pushDialog(common.DialogRequest{
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

	m.overlay.pushDialog(common.DialogRequest{
		Title:   "Remove " + title[:len(title)-1],
		Body:    body,
		Buttons: []string{"Close"},
		OnClose: func(_ string) {
			m.showListDialog(title, subdir, ext)
		},
	})
}

func (m *consoleModel) View() tea.View {
	EnterRenderPath()
	defer LeaveRenderPath()

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading server console...")
		view.AltScreen = true
		return view
	}

	t := m.theme
	primary := t.LayerAt(0)   // desktop
	secondary := t.LayerAt(1) // menus, status bar

	buf := common.NewImageBuffer(m.width, m.height)

	// NC action bar (row 0) — PaintANSI the string into the buffer.
	menus := m.consoleMenus()
	ncBar := m.overlay.renderNCBar(m.width, menus, secondary)
	buf.PaintANSI(0, 0, m.width, 1, ncBar, secondary.FgC(), secondary.BgC())

	// NC panel with log + input (rows 1 through height-2) — render directly.
	panelH := m.height - 2 // subtract NC bar and status bar
	m.window.RenderToBuf(buf, 0, 1, m.width, panelH, primary)

	// Status bar (bottom row).
	m.app.state.mu.RLock()
	gameName := m.app.state.GameName
	phase := m.app.state.GamePhase
	m.app.state.mu.RUnlock()
	gameLabel := "none"
	if gameName != "" {
		gameLabel = gameName
		switch phase {
		case common.PhaseSplash:
			gameLabel += " [splash]"
		case common.PhasePlaying:
			gameLabel += " [playing]"
		case common.PhaseGameOver:
			gameLabel += " [game over]"
		}
	}
	statusText := fmt.Sprintf("game: %s | players: %d | uptime %s | %s", gameLabel, m.app.state.PlayerCount(), m.app.uptime(), time.Now().Format("15:04:05"))
	sbFg := secondary.FgC()
	sbBg := secondary.BgC()
	buf.Fill(0, m.height-1, m.width, 1, ' ', sbFg, sbBg, common.AttrNone)
	// Right-align status text.
	statusRow := m.height - 1
	startX := m.width - len(statusText)
	if startX < 0 {
		startX = 0
	}
	buf.WriteString(startX, statusRow, statusText, sbFg, sbBg, common.AttrNone)

	// Overlay layers: render to sub-buffers, blit, then recolor for shadow.
	shadowFg := t.ShadowFgC()
	shadowBg := t.ShadowBgC()
	if m.overlay.openMenu >= 0 {
		if dd := m.overlay.renderDropdown(menus, 0, secondary); dd.content != "" {
			sub := common.NewImageBuffer(dd.width, dd.height)
			sub.PaintANSI(0, 0, dd.width, dd.height, dd.content, secondary.FgC(), secondary.BgC())
			buf.Blit(dd.col, dd.row, sub)
			common.BlitShadow(buf, dd.col, dd.row, dd.width, dd.height, shadowFg, shadowBg)
		}
	}
	if m.overlay.hasDialog() {
		if dlg := m.overlay.renderDialog(m.width, m.height, t.LayerAt(2)); dlg.content != "" {
			sub := common.NewImageBuffer(dlg.width, dlg.height)
			dlgLayer := t.LayerAt(2)
			sub.PaintANSI(0, 0, dlg.width, dlg.height, dlg.content, dlgLayer.FgC(), dlgLayer.BgC())
			buf.Blit(dlg.col, dlg.row, sub)
			common.BlitShadow(buf, dlg.col, dlg.row, dlg.width, dlg.height, shadowFg, shadowBg)
		}
	}

	view.SetContent(buf.ToString())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	// Hide cursor when overlay (menu/dialog) is active.
	if !m.overlay.isActive() {
		if cx, cy, visible := m.window.CursorPosition(); visible {
			if cursor := m.inputCtrl.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	}

	return view
}

func (m *consoleModel) resize() {
	// Width is managed by NCWindow.Render via the grid layout.
}

func (m *consoleModel) appendTagged(cat logCategory, line string) {
	for _, l := range strings.Split(line, "\n") {
		m.allLines = append(m.allLines, taggedLine{cat: cat, text: l})
	}
	if len(m.allLines) > 1000 {
		m.allLines = m.allLines[len(m.allLines)-1000:]
	}
	m.rebuildVisibleLines()
}

// appendLog adds a command/feedback line (for backwards compat with existing callers).
func (m *consoleModel) appendLog(line string) {
	m.appendTagged(catCommand, line)
}

func (m *consoleModel) rebuildVisibleLines() {
	var visible []string
	for _, tl := range m.allLines {
		if m.filter[tl.cat] {
			visible = append(visible, tl.text)
		}
	}
	m.logView.Lines = visible
}

// tabComplete handles tab completion for the input control.
func (m *consoleModel) tabComplete(current string) (string, bool) {
	if !strings.HasPrefix(current, "/") {
		return "", false
	}
	if m.tabCandidates == nil {
		m.tabPrefix, m.tabCandidates = m.app.registry.TabCandidates(current, m.app.state.PlayerNames())
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
func (m *consoleModel) submitInput(text string) {
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
	ctx := common.CommandContext{
		PlayerID:  "",
		IsConsole: true,
		IsAdmin:   true,
		Reply: func(s string) {
			m.appendLog(s)
		},
		Broadcast: func(s string) {
			m.app.broadcastChat(common.Message{Text: s})
		},
		ServerLog: func(s string) {
			m.appendLog(s)
		},
	}
	m.app.registry.Dispatch(text, ctx)
}

func (m *consoleModel) handleThemeCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := ListThemes(m.app.dataDir)
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
	path := filepath.Join(m.app.dataDir, "themes", name+".json")
	t, err := LoadTheme(path)
	if err != nil {
		m.appendLog(fmt.Sprintf("Failed to load theme: %v", err))
		return
	}
	m.theme = t
	m.appendLog(fmt.Sprintf("Theme changed to: %s", t.Name))
}

func (m *consoleModel) handlePluginCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) <= 1 {
		available := listDir(filepath.Join(m.app.dataDir, "plugins"), ".js")
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
		name, path, err := resolvePluginPath(parts[2], m.app.dataDir)
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
		pl, err := LoadPlugin(path, m.app.clock)
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
func (m *consoleModel) dispatchPluginReply(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		ctx := common.CommandContext{
			PlayerID:  "",
			IsConsole: true,
			IsAdmin:   true,
			Reply: func(s string) {
				m.appendLog(s)
			},
			Broadcast: func(s string) {
				m.app.broadcastChat(common.Message{Text: s})
			},
			ServerLog: func(s string) {
				m.appendLog(s)
			},
		}
		m.app.registry.Dispatch(text, ctx)
		return
	}
	// Plain text from console plugin → broadcast as admin chat.
	m.app.broadcastChat(common.Message{Author: "admin", Text: text})
}

// slogLine carries a formatted slog record to the console.
type slogLine struct {
	cat  logCategory
	text string
}

// tea.Msg types for channel-based updates.
type chatLineMsg common.Message
type slogLineMsg slogLine

func listenForEvents(chatCh <-chan common.Message, slogCh <-chan slogLine) tea.Cmd {
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
