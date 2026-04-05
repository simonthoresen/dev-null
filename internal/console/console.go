package console

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"dev-null/internal/domain"
	"dev-null/internal/render"
	"dev-null/internal/engine"
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
		profile: profile,
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
				{Label: "&Themes...", Handler: func(_ string) { m.pushConsoleThemeDialog(0) }},
				{Label: "&Plugins...", Handler: func(_ string) { m.pushConsolePluginDialog(0) }},
				{Label: "&Shaders...", Handler: func(_ string) { m.pushConsoleShaderDialog(0) }},
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

// pushConsoleThemeDialog opens an interactive Themes dialog for the server console.
// Enter activates the highlighted theme. Remove deletes the theme file from disk.
func (m *Model) pushConsoleThemeDialog(cursor int) {
	available := theme.ListThemes(m.api.DataDir())
	if len(available) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Themes",
			Body:    "No themes found in themes/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showConsoleThemeAddDialog(0)
				}
			},
		})
		return
	}
	tags := make([]string, len(available))
	for i, name := range available {
		if strings.EqualFold(name, m.theme.Name) {
			tags[i] = "(●)"
		} else {
			tags[i] = "(○)"
		}
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:      "Themes",
		ListItems:  available,
		ListTags:   tags,
		ListCursor: cursor,
		Buttons:    []string{"Add", "Remove", "Close"},
		OnListEnter: func(idx int) {
			name := available[idx]
			path := filepath.Join(m.api.DataDir(), "themes", name+".json")
			t, err := theme.Load(path)
			if err != nil {
				return
			}
			m.theme = t
			m.themeName = name
			m.persistServerConfig()
			m.overlay.PopDialog()
			m.pushConsoleThemeDialog(idx)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Add":
				m.showConsoleThemeAddDialog(idx)
			case "Remove":
				m.showConsoleThemeRemoveConfirm(available[idx], idx)
			}
		},
	})
}

func (m *Model) showConsoleThemeAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Theme",
		Body:         "Enter a theme name to activate:",
		InputPrompt:  "Theme",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handleThemeCommand("/theme " + strings.TrimSpace(value))
			}
			m.pushConsoleThemeDialog(returnCursor)
		},
	})
}

func (m *Model) showConsoleThemeRemoveConfirm(name string, returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Theme File",
		Body:    fmt.Sprintf("Delete '%s.json' from the themes folder?\nThis cannot be undone.", name),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "themes", name+".json"))
			}
			m.pushConsoleThemeDialog(returnCursor)
		},
	})
}

// pushConsolePluginDialog opens an interactive Plugins dialog for the server console.
// Loaded plugins are listed first (with order numbers), then unloaded ones.
// Enter toggles load/unload. Remove deletes the plugin file from disk.
func (m *Model) pushConsolePluginDialog(cursor int) {
	available := engine.ListScripts(filepath.Join(m.api.DataDir(), "plugins"))
	loadedSet := make(map[string]bool)
	for _, n := range m.pluginNames {
		loadedSet[n] = true
	}
	if len(available) == 0 && len(m.pluginNames) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Plugins",
			Body:    "No plugins found in plugins/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showConsolePluginAddDialog(0)
				}
			},
		})
		return
	}

	activeCount := len(m.pluginNames)
	numWidth := len(fmt.Sprintf("%d", max(activeCount, 1)))
	inactivePad := strings.Repeat(" ", numWidth+2) // matches "N. " width

	var items []string
	var tags []string
	for i, name := range m.pluginNames {
		items = append(items, fmt.Sprintf("%*d. %s", numWidth, i+1, name))
		tags = append(tags, "[✓]")
	}
	var inactive []string
	for _, name := range available {
		if !loadedSet[name] {
			items = append(items, inactivePad+name)
			tags = append(tags, "[ ]")
			inactive = append(inactive, name)
		}
	}

	m.overlay.PushDialog(domain.DialogRequest{
		Title:                 "Plugins",
		ListItems:             items,
		ListTags:              tags,
		ListCursor:            cursor,
		Buttons:               []string{"Add", "Remove", "Close"},
		RequireListNavigation: []string{"Remove"},
		OnListEnter: func(idx int) {
			var newCursor int
			if idx < activeCount {
				toggledName := m.pluginNames[idx]
				m.handlePluginCommand("/plugin unload " + toggledName)
				newActiveSet := make(map[string]bool)
				for _, n := range m.pluginNames {
					newActiveSet[n] = true
				}
				pos := 0
				for _, n := range available {
					if !newActiveSet[n] {
						if strings.EqualFold(n, toggledName) {
							break
						}
						pos++
					}
				}
				newCursor = len(m.pluginNames) + pos
			} else {
				inactiveIdx := idx - activeCount
				if inactiveIdx < len(inactive) {
					m.handlePluginCommand("/plugin load " + inactive[inactiveIdx])
					newCursor = len(m.pluginNames) - 1
				}
			}
			m.overlay.PopDialog()
			m.pushConsolePluginDialog(newCursor)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Add":
				m.showConsolePluginAddDialog(idx)
			case "Remove":
				var name string
				if idx < activeCount {
					name = m.pluginNames[idx]
				} else {
					inactiveIdx := idx - activeCount
					if inactiveIdx < len(inactive) {
						name = inactive[inactiveIdx]
					}
				}
				if name != "" {
					m.showConsolePluginRemoveConfirm(name, idx)
				}
			}
		},
	})
}

func (m *Model) showConsolePluginAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Plugin",
		Body:         "Enter a plugin name or URL:",
		InputPrompt:  "Plugin",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handlePluginCommand("/plugin load " + strings.TrimSpace(value))
			}
			m.pushConsolePluginDialog(returnCursor)
		},
	})
}

func (m *Model) showConsolePluginRemoveConfirm(name string, returnCursor int) {
	// Determine extension: check for .js first, then .lua.
	ext := ".js"
	if _, err := os.Stat(filepath.Join(m.api.DataDir(), "plugins", name+".lua")); err == nil {
		ext = ".lua"
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Plugin File",
		Body:    fmt.Sprintf("Delete '%s%s' from the plugins folder?\nThis cannot be undone.", name, ext),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				os.Remove(filepath.Join(m.api.DataDir(), "plugins", name+ext))
			}
			m.pushConsolePluginDialog(returnCursor)
		},
	})
}

// pushConsoleShaderDialog opens an interactive Shaders dialog for the server console.
// Active shaders are listed first (in order), then inactive ones.
// Enter toggles load/unload. Up/Down reorders active shaders. Remove deletes file from disk.
func (m *Model) pushConsoleShaderDialog(cursor int) {
	available := engine.ListScripts(filepath.Join(m.api.DataDir(), "shaders"))
	loadedSet := make(map[string]bool)
	for _, n := range m.shaderNames {
		loadedSet[n] = true
	}

	activeCount := len(m.shaderNames)
	numWidth := len(fmt.Sprintf("%d", max(activeCount, 1)))
	inactivePad := strings.Repeat(" ", numWidth+2) // matches "N. " width

	var items []string
	var tags []string
	for i, name := range m.shaderNames {
		items = append(items, fmt.Sprintf("%*d. %s", numWidth, i+1, name))
		tags = append(tags, "[✓]")
	}
	var inactive []string
	for _, name := range available {
		if !loadedSet[name] {
			items = append(items, inactivePad+name)
			tags = append(tags, "[ ]")
			inactive = append(inactive, name)
		}
	}

	if len(items) == 0 {
		m.overlay.PushDialog(domain.DialogRequest{
			Title:   "Shaders",
			Body:    "No shaders found in shaders/",
			Buttons: []string{"Add", "Close"},
			OnClose: func(btn string) {
				if btn == "Add" {
					m.showConsoleShaderAddDialog(0)
				}
			},
		})
		return
	}

	m.overlay.PushDialog(domain.DialogRequest{
		Title:                 "Shaders",
		ListItems:             items,
		ListTags:              tags,
		ListCursor:            cursor,
		Buttons:               []string{"Add", "Remove", "Close"},
		RequireListNavigation: []string{"Remove"},
		OnListEnter: func(idx int) {
			var newCursor int
			if idx < activeCount {
				toggledName := m.shaderNames[idx]
				m.handleShaderCommand("/shader unload " + toggledName)
				// Find where the unloaded shader landed in the new inactive list.
				newActiveSet := make(map[string]bool)
				for _, n := range m.shaderNames {
					newActiveSet[n] = true
				}
				pos := 0
				for _, n := range available {
					if !newActiveSet[n] {
						if strings.EqualFold(n, toggledName) {
							break
						}
						pos++
					}
				}
				newCursor = len(m.shaderNames) + pos
			} else {
				inactiveIdx := idx - activeCount
				if inactiveIdx < len(inactive) {
					m.handleShaderCommand("/shader load " + inactive[inactiveIdx])
					newCursor = len(m.shaderNames) - 1
				}
			}
			m.overlay.PopDialog()
			m.pushConsoleShaderDialog(newCursor)
		},
		OnListAction: func(btn string, idx int) {
			switch btn {
			case "Add":
				m.showConsoleShaderAddDialog(idx)
			case "Remove":
				var name string
				if idx < activeCount {
					name = m.shaderNames[idx]
				} else {
					inactiveIdx := idx - activeCount
					if inactiveIdx < len(inactive) {
						name = inactive[inactiveIdx]
					}
				}
				if name != "" {
					m.showConsoleShaderRemoveConfirm(name, idx)
				} else {
					m.pushConsoleShaderDialog(idx)
				}
			}
		},
	})
}

func (m *Model) showConsoleShaderAddDialog(returnCursor int) {
	m.overlay.PushDialog(domain.DialogRequest{
		Title:        "Add Shader",
		Body:         "Enter a shader name or URL:",
		InputPrompt:  "Shader",
		Buttons:      []string{"Load", "Cancel"},
		OnInputClose: func(btn, value string) {
			if btn == "Load" && strings.TrimSpace(value) != "" {
				m.handleShaderCommand("/shader load " + strings.TrimSpace(value))
			}
			m.pushConsoleShaderDialog(returnCursor)
		},
	})
}

func (m *Model) showConsoleShaderRemoveConfirm(name string, returnCursor int) {
	ext := ".js"
	if _, err := os.Stat(filepath.Join(m.api.DataDir(), "shaders", name+".lua")); err == nil {
		ext = ".lua"
	}
	m.overlay.PushDialog(domain.DialogRequest{
		Title:   "Delete Shader File",
		Body:    fmt.Sprintf("Delete '%s%s' from the shaders folder?\nThis cannot be undone.", name, ext),
		Buttons: []string{"Delete", "Cancel"},
		Warning: true,
		OnClose: func(btn string) {
			if btn == "Delete" {
				// Unload if active before deleting.
				for _, n := range m.shaderNames {
					if strings.EqualFold(n, name) {
						m.handleShaderCommand("/shader unload " + name)
						break
					}
				}
				os.Remove(filepath.Join(m.api.DataDir(), "shaders", name+ext))
			}
			m.pushConsoleShaderDialog(returnCursor)
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
		if dd := m.overlay.RenderDropdown(menus, 0, secondary); dd.Content != "" {
			sub := render.NewImageBuffer(dd.Width, dd.Height)
			sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, secondary.Fg, secondary.Bg)
			buf.Blit(dd.Col, dd.Row, sub)
			render.BlitShadow(buf, dd.Col, dd.Row, dd.Width, dd.Height, shadowFg, shadowBg)
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

func (m *Model) rebuildVisibleLines() {
	var visible []string
	for _, tl := range m.allLines {
		if m.filter[tl.cat] {
			visible = append(visible, tl.text)
		}
	}
	m.logView.Lines = visible
}
