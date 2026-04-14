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

	// Load init commands from ~/.dev-null/server.txt if it exists.
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".dev-null", "server.txt")); err == nil {
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
		// Hard exit on Ctrl+C / Ctrl+D — always works, bypasses everything.
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+d" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		// Dialog overlay gets the real tea.Msg for NC control handling.
		if m.overlay.HasDialog() {
			consumed, cmd := m.overlay.HandleDialogMsg(msg)
			if consumed {
				return m, cmd
			}
		}
		// Let the overlay handle F10/menu keys.
		if m.overlay.HandleKey(msg.String(), m.consoleMenus(), "") {
			return m, nil
		}
		// PgUp/PgDn always scroll the log, regardless of focus.
		switch msg.String() {
		case "pgup", "pgdown":
			m.logView.Update(msg)
			return m, nil
		case "enter":
			// Enter outside the command bar moves focus there.
			if m.window.FocusIdx != 2 {
				m.window.FocusIdx = 2
				return m, m.inputCtrl.Model.Focus()
			}
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
				{Label: "&Games...", Handler: func(_ string) { m.pushGamesDialog(0) }},
				{Label: "&Saves...", Handler: func(_ string) { m.pushSavesDialog(0) }},
				{Label: "---"},
				{Label: "&Themes...", Handler: func(_ string) { m.pushThemeDialog(0) }},
				{Label: "&Plugins...", Handler: func(_ string) { m.pushPluginDialog(0) }},
				{Label: "&Shaders...", Handler: func(_ string) { m.pushShaderDialog(0) }},
				{Label: "---"},
				{Label: "&Invite...", Handler: func(_ string) { m.pushInviteDialog() }},
				{Label: "---"},
				{Label: "E&xit", Hotkey: "ctrl+q", Handler: func(_ string) {
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

func (m *Model) pushThemeDialog(cursor int) {
	localcmd.PushThemeDialog(cursor, localcmd.ThemeDialogOptions{
		DataDir:          m.api.DataDir(),
		Overlay:          &m.overlay,
		CurrentThemeName: m.themeName,
		CanAdd:           true,
		OnSelect: func(name string, _ *theme.Theme) {
			m.submitInput("/theme-load " + name)
		},
		OnRemove: m.showThemeRemoveConfirm,
		Reload:   m.pushThemeDialog,
	})
}

func (m *Model) pushPluginDialog(cursor int) {
	localcmd.PushScriptDialog(cursor, localcmd.ScriptDialogOptions{
		Title:   "Plugins",
		SubDir:  "plugins",
		DataDir: m.api.DataDir(),
		Overlay: &m.overlay,
		Loaded:  m.pluginNames,
		CanAdd:  true,
		OnToggle: func(name string, load bool) {
			if load {
				m.submitInput("/plugin-load " + name)
			} else {
				m.submitInput("/plugin-unload " + name)
			}
		},
		OnRemove: m.showPluginRemoveConfirm,
		Reload:   m.pushPluginDialog,
	})
}

func (m *Model) pushShaderDialog(cursor int) {
	localcmd.PushScriptDialog(cursor, localcmd.ScriptDialogOptions{
		Title:   "Shaders",
		SubDir:  "shaders",
		DataDir: m.api.DataDir(),
		Overlay: &m.overlay,
		Loaded:  m.shaderNames,
		CanAdd:  true,
		OnToggle: func(name string, load bool) {
			if load {
				m.submitInput("/shader-load " + name)
			} else {
				m.submitInput("/shader-unload " + name)
			}
		},
		OnRemove: m.showShaderRemoveConfirm,
		Reload:   m.pushShaderDialog,
	})
}

func (m *Model) pushGamesDialog(cursor int) {
	m.api.State().RLock()
	currentGame := m.api.State().GameName
	teamCount := len(m.api.State().Teams)
	m.api.State().RUnlock()

	localcmd.PushGameDialog(cursor, localcmd.GameDialogOptions{
		DataDir:     m.api.DataDir(),
		Overlay:     &m.overlay,
		CurrentGame: currentGame,
		TeamCount:   teamCount,
		CanAdd:      true,
		CanRemove:   true,
		OnLoad: func(name string) {
			m.submitInput("/game-load " + name)
		},
		OnRemove: m.showGameRemoveConfirm,
		Reload:   m.pushGamesDialog,
	})
}

func (m *Model) showGameRemoveConfirm(name string, cursor int) {
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
			m.pushGamesDialog(cursor)
		},
	})
}

func (m *Model) pushSavesDialog(cursor int) {
	localcmd.PushSaveDialog(cursor, localcmd.SaveDialogOptions{
		DataDir:   m.api.DataDir(),
		Overlay:   &m.overlay,
		CanRemove: true,
		OnLoad: func(gameName, saveName string) {
			m.submitInput("/game-resume " + gameName + "/" + saveName)
		},
		OnRemove: m.showSaveRemoveConfirm,
		Reload:   m.pushSavesDialog,
	})
}

func (m *Model) showSaveRemoveConfirm(gameName, saveName string, cursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Save",
		Body:    fmt.Sprintf("Delete save?\n\n  %s/%s\n\nThis cannot be undone.", gameName, saveName),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				state.DeleteSuspend(m.api.DataDir(), gameName, saveName) //nolint:errcheck
			}
			m.pushSavesDialog(cursor)
		},
	})
}

// scriptExt returns the file extension (".js" or ".lua") for a script in dir.
func scriptExt(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, name+".lua")); err == nil {
		return ".lua"
	}
	return ".js"
}

func (m *Model) showPluginRemoveConfirm(names []string, returnCursor int) {
	var lines []string
	for _, name := range names {
		ext := scriptExt(filepath.Join(m.api.DataDir(), "plugins"), name)
		lines = append(lines, "  "+name+ext)
	}
	noun := "plugin"
	if len(names) > 1 {
		noun = fmt.Sprintf("%d plugins", len(names))
	}
	body := fmt.Sprintf("Delete active %s from the plugins folder?\n\n%s\n\nThis cannot be undone.", noun, strings.Join(lines, "\n"))
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Plugin Files",
		Body:    body,
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				for _, name := range names {
					m.submitInput("/plugin-unload " + name)
					ext := scriptExt(filepath.Join(m.api.DataDir(), "plugins"), name)
					os.Remove(filepath.Join(m.api.DataDir(), "plugins", name+ext))
				}
			}
			m.pushPluginDialog(returnCursor)
		},
	})
}

func (m *Model) showThemeRemoveConfirm(name string, returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Theme File",
		Body:    fmt.Sprintf("Delete '%s.json' from the themes folder?\nThis cannot be undone.", name),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "themes", name+".json"))
			}
			m.pushThemeDialog(returnCursor)
		},
	})
}

func (m *Model) showShaderRemoveConfirm(names []string, returnCursor int) {
	var lines []string
	for _, name := range names {
		ext := scriptExt(filepath.Join(m.api.DataDir(), "shaders"), name)
		lines = append(lines, "  "+name+ext)
	}
	noun := "shader"
	if len(names) > 1 {
		noun = fmt.Sprintf("%d shaders", len(names))
	}
	body := fmt.Sprintf("Delete active %s from the shaders folder?\n\n%s\n\nThis cannot be undone.", noun, strings.Join(lines, "\n"))
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Shader Files",
		Body:    body,
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				for _, name := range names {
					m.submitInput("/shader-unload " + name)
					ext := scriptExt(filepath.Join(m.api.DataDir(), "shaders"), name)
					os.Remove(filepath.Join(m.api.DataDir(), "shaders", name+ext))
				}
			}
			m.pushShaderDialog(returnCursor)
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
		case domain.PhaseEnding:
			gameLabel += " [ending]"
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

func (m *Model) pushInviteDialog() {
	winLink, sshLink := m.api.InviteLinks()
	win := widget.BuildInviteWindow(winLink, sshLink,
		func(v string) { m.pendingClipboard = v },
		m.overlay.PopDialog,
	)
	m.overlay.PushWindowDialog(win)
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
