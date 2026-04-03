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

	// Reusable render buffer — cleared and resized each frame instead of allocated.
	renderBuf *render.ImageBuffer
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
				{Label: "&Shaders...", Handler: func(_ string) { m.showShaderDialog() }},
				{Label: "---"},
				{Label: "&Shutdown", Hotkey: "ctrl+q", Handler: func(_ string) {
					m.overlay.PushDialog(domain.DialogRequest{
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
	m.api.State().RLock()
	shaderElapsed := m.api.State().ElapsedSec
	m.api.State().RUnlock()
	engine.ApplyShaders(m.shaders, buf, shaderElapsed)

	// Overlay layers: render to sub-buffers, blit, then recolor for shadow.
	shadowFg := t.ShadowFg
	shadowBg := t.ShadowBg
	if m.overlay.OpenMenu >= 0 {
		if dd := m.overlay.RenderDropdown(menus, 0, secondary); dd.Content != "" {
			sub := render.NewImageBuffer(dd.Width, dd.Height)
			sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, secondary.Fg, secondary.Bg)
			buf.Blit(dd.Col, dd.Row, sub)
			render.BlitShadow(buf, dd.Col, dd.Row, dd.Width, dd.Height, shadowFg, shadowBg)
		}
	}
	if m.overlay.HasDialog() {
		if dlg := m.overlay.RenderDialog(m.width, m.height, t.LayerAt(2)); dlg.Content != "" {
			sub := render.NewImageBuffer(dlg.Width, dlg.Height)
			dlgLayer := t.LayerAt(2)
			sub.PaintANSI(0, 0, dlg.Width, dlg.Height, dlg.Content, dlgLayer.Fg, dlgLayer.Bg)
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
