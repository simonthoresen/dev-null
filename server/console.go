package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"null-space/common"
)

type consoleModel struct {
	app    *Server
	cancel context.CancelFunc
	width  int
	height int

	input textinput.Model

	logView *NCTextView
	panel   *NCPanel

	inputHistory []string
	historyIdx   int
	historyDraft string

	tabPrefix     string
	tabCandidates []string
	tabIndex      int

	// Per-console theme
	theme *Theme

	// NC overlay (menus, dialogs)
	overlay overlayState

	// Per-console plugins
	plugins     []*jsPlugin
	pluginNames []string
}

func NewConsoleModel(app *Server, cancel context.CancelFunc) *consoleModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = ""
	input.CharLimit = 256
	input.SetWidth(78)
	input.Focus()

	logView := &NCTextView{BottomAlign: true}
	tiControl := &NCTextInput{Model: &input}

	panel := &NCPanel{
		Title: "Server Log",
		Controls: []NCControl{
			logView,
			tiControl,
		},
	}
	panel.FocusFirst()

	return &consoleModel{
		app:        app,
		cancel:     cancel,
		input:      input,
		logView:    logView,
		panel:      panel,
		theme:      DefaultTheme(),
		overlay:    overlayState{openMenu: -1},
		historyIdx: -1,
	}
}

func (m *consoleModel) Init() tea.Cmd {
	return listenForLogs(m.app.LogCh(), m.app.ChatCh())
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
		// re-render for spinner and clock update
		return m, nil

	case logLineMsg:
		m.appendLog(string(msg))
		for _, pl := range m.plugins {
			if reply := pl.OnMessage("", string(msg), true); reply != "" {
				m.dispatchPluginReply(reply)
			}
		}
		return m, listenForLogs(m.app.LogCh(), m.app.ChatCh())

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
		m.appendLog(line)
		if !chatMsg.IsReply {
			isSystem := chatMsg.Author == ""
			for _, pl := range m.plugins {
				if reply := pl.OnMessage(chatMsg.Author, chatMsg.Text, isSystem); reply != "" {
					m.dispatchPluginReply(reply)
				}
			}
		}
		return m, listenForLogs(m.app.LogCh(), m.app.ChatCh())

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
			m.panel.HandleClick(msg.X, msg.Y)
		}
		return m, nil

	case tea.KeyPressMsg:
		// Let the overlay handle F10/menu/dialog keys first.
		if m.overlay.handleKey(msg.String(), m.consoleMenus(), "") {
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
			m.tabCandidates = nil
			m.historyIdx = -1
			m.historyDraft = ""
			m.submitInput()
			return m, nil
		case "esc":
			m.tabCandidates = nil
			m.historyIdx = -1
			m.historyDraft = ""
			m.input.SetValue("")
			return m, nil
		case "up":
			if len(m.inputHistory) == 0 {
				return m, nil
			}
			if m.historyIdx == -1 {
				m.historyDraft = m.input.Value()
				m.historyIdx = len(m.inputHistory) - 1
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.input.SetValue(m.inputHistory[m.historyIdx])
			m.input.CursorEnd()
			return m, nil
		case "down":
			if m.historyIdx == -1 {
				return m, nil
			}
			if m.historyIdx < len(m.inputHistory)-1 {
				m.historyIdx++
				m.input.SetValue(m.inputHistory[m.historyIdx])
			} else {
				m.historyIdx = -1
				m.input.SetValue(m.historyDraft)
			}
			m.input.CursorEnd()
			return m, nil
		case "tab":
			if strings.HasPrefix(m.input.Value(), "/") {
				if m.tabCandidates == nil {
					m.tabPrefix, m.tabCandidates = m.app.registry.TabCandidates(m.input.Value(), m.app.state.PlayerNames())
					m.tabIndex = 0
				}
				if len(m.tabCandidates) > 0 {
					m.input.SetValue(m.tabPrefix + m.tabCandidates[m.tabIndex])
					m.input.CursorEnd()
					m.tabIndex = (m.tabIndex + 1) % len(m.tabCandidates)
				}
			}
			return m, nil
		default:
			m.tabCandidates = nil
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// Forward to textinput for cursor blink etc.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *consoleModel) consoleMenus() []common.MenuDef {
	return []common.MenuDef{
		{
			Label: "&Server",
			Items: []common.MenuItemDef{
				{Label: "&Themes...", Handler: func(_ string) { m.showListDialog("Themes", "themes", ".json") }},
				{Label: "&Plugins...", Handler: func(_ string) { m.showListDialog("Plugins", "plugins", ".js") }},
				{Label: "&Games...", Handler: func(_ string) { m.showListDialog("Games", "games", ".js") }},
				{Label: "---"},
				{Label: "S&hutdown", Handler: func(_ string) {
					m.overlay.pushDialog(common.DialogRequest{
						Title:   "Shutdown",
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
	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading server console...")
		view.AltScreen = true
		return view
	}

	t := m.theme
	statusStyle := lipgloss.NewStyle().Background(t.DesktopBgC()).Foreground(t.DesktopFgC()).Bold(true)

	// NC action bar (row 0)
	ncBar := m.overlay.renderNCBar(m.width, m.consoleMenus(), t)

	// NC panel with log + input (rows 1 through height-2)
	panelH := m.height - 2 // subtract NC bar and status bar
	panelContent := m.panel.Render(0, 1, m.width, panelH, t)

	// Status bar (bottom row)
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
	statusText := fmt.Sprintf("game: %s | teams: %d | uptime %s | %s", gameLabel, m.app.state.TeamCount(), m.app.uptime(), time.Now().Format("15:04:05"))
	statusBar := statusStyle.Width(m.width).Render(truncateStyled(statusText, m.width))

	content := lipgloss.JoinVertical(lipgloss.Left, ncBar, panelContent, statusBar)

	// Overlay layers (dropdown menus, dialogs)
	menus := m.consoleMenus()
	if m.overlay.openMenu >= 0 {
		if ddStr, ddCol, ddRow := m.overlay.renderDropdown(menus, 0, t); ddStr != "" {
			content = PlaceOverlay(ddCol, ddRow+1, ddStr, content)
		}
	}
	if m.overlay.hasDialog() {
		if dlgStr, dlgCol, dlgRow := m.overlay.renderDialog(m.width, m.height, t); dlgStr != "" {
			content = PlaceOverlay(dlgCol, dlgRow, dlgStr, content)
		}
	}

	view.SetContent(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	if cx, cy, visible := m.panel.CursorPosition(); visible {
		if cursor := m.input.Cursor(); cursor != nil {
			cursor.Position.X = cx
			cursor.Position.Y = cy
			view.Cursor = cursor
		}
	}

	return view
}

func (m *consoleModel) resize() {
	m.input.SetWidth(max(1, m.width-2))
}

func (m *consoleModel) appendLog(line string) {
	for _, l := range strings.Split(line, "\n") {
		m.logView.Lines = append(m.logView.Lines, l)
	}
	if len(m.logView.Lines) > 500 {
		m.logView.Lines = m.logView.Lines[len(m.logView.Lines)-500:]
	}
}

func (m *consoleModel) submitInput() {
	text := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if text == "" {
		return
	}
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > 50 {
			m.inputHistory = m.inputHistory[1:]
		}
	}
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
		pl, err := LoadPlugin(path)
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

// tea.Msg types for channel-based updates
type logLineMsg string
type chatLineMsg common.Message

func listenForLogs(logCh <-chan string, chatCh <-chan common.Message) tea.Cmd {
	return func() tea.Msg {
		select {
		case line, ok := <-logCh:
			if !ok {
				return nil
			}
			return logLineMsg(line)
		case msg, ok := <-chatCh:
			if !ok {
				return nil
			}
			return chatLineMsg(msg)
		}
	}
}
