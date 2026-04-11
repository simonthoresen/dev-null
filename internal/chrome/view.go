package chrome

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"dev-null/internal/console"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
	"dev-null/internal/widget"
)

func (m *Model) View() tea.View {
	console.EnterRenderPath()
	defer console.LeaveRenderPath()

	// Set monochrome on all theme layers so widgets use text cursor glyphs
	// (►/›) instead of relying on background-color highlighting alone.
	m.theme.SetMonochrome(m.ColorProfile <= colorprofile.ASCII)

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading dev-null...")
		view.AltScreen = true
		return view
	}

	m.api.State().RLock()
	game := m.api.State().ActiveGame
	gameName := m.api.State().GameName
	phase := m.api.State().GamePhase
	m.api.State().RUnlock()

	if (!m.inActiveGame || phase == domain.PhaseNone) && m.teamEditing {
		SetInputStyle(&m.teamEditInput, m.theme.LayerAt(0).InputBg, m.theme.LayerAt(0).InputFg)
	}

	// Build menus once per frame — passed to sub-views and overlay rendering.
	menus := m.cachedMenus()

	if m.renderBuf == nil {
		m.renderBuf = render.NewImageBuffer(m.width, m.height)
	} else {
		m.renderBuf.EnsureSize(m.width, m.height)
	}
	buf := m.renderBuf

	if !m.inActiveGame || phase == domain.PhaseNone {
		m.renderLobby(buf, menus)
	} else {
		m.renderPlaying(buf, menus, game, gameName, phase)
	}

	// Overlay layers: render to sub-buffers, blit, then shadow via RecolorRect.
	shadowFg := m.theme.ShadowFg
	shadowBg := m.theme.ShadowBg
	if m.overlay.OpenMenu >= 0 {
		menuLayer := m.theme.LayerAt(1)
		if dd := m.overlay.RenderDropdown(menus, 0, menuLayer); dd.Content != "" {
			sub := render.NewImageBuffer(dd.Width, dd.Height)
			sub.PaintANSI(0, 0, dd.Width, dd.Height, dd.Content, menuLayer.Fg, menuLayer.Bg)
			buf.Blit(dd.Col, dd.Row, sub)
			render.BlitShadow(buf, dd.Col, dd.Row, dd.Width, dd.Height, shadowFg, shadowBg)
		}
	}
	if m.overlay.HasDialog() {
		dlgLayer := m.theme.LayerAt(m.overlay.DialogLayer())
		if m.overlay.DialogIsWarning() {
			dlgLayer = m.theme.WarningLayer()
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

	// Enhanced client OSC protocol: send game source, state, and viewport bounds.
	// OSC sequences are written directly to the session, bypassing Bubble Tea's cell renderer
	// which would consume them as styled string content instead of passing them through.
	isCanvasHD := m.renderMode == domain.RenderModePixels
	if m.SessionWriter != nil && m.IsEnhancedClient && m.inActiveGame && game != nil {
		var oscData string
		// Send local/remote mode OSC once on game load or when mode changes.
		if !m.oscModeSent {
			if isCanvasHD {
				oscData += render.EncodeModeOSC("local")
			} else {
				oscData += render.EncodeModeOSC("remote")
			}
			m.oscModeSent = true
		}

		// Send game source files once for Canvas HD (client-side rendering).
		if isCanvasHD && !m.gameSrcSent {
			for _, sf := range game.GameSource() {
				oscData += render.EncodeGameSourceOSC(sf.Name, sf.Content)
			}
			m.gameSrcSent = true
		}
		if !m.assetsSent {
			assets := game.GameAssets()
			if len(assets) > 0 {
				oscData += render.EncodeAssetManifestOSC(len(assets))
				for _, a := range assets {
					oscData += render.EncodeAssetOSC(a.Name, a.Data)
				}
			}
			m.assetsSent = true
		}
		// Drain pending sound OSC commands (from JS playSound/stopSound via chatCh).
		for _, osc := range m.pendingSoundOSC {
			oscData += osc
		}
		m.pendingSoundOSC = nil
		// Drain pending MIDI OSC events (from JS midiNote/midiProgram/midiCC via chatCh).
		for _, osc := range m.pendingMidiOSC {
			oscData += osc
		}
		m.pendingMidiOSC = nil
		// Send synth selection OSC once on game load or when changed.
		if !m.synthSent && m.synthName != "" {
			oscData += render.EncodeSynthOSC(m.synthName)
			m.synthSent = true
		}
		if m.viewportW > 0 && m.viewportH > 0 {
			oscData += render.EncodeViewportOSC(m.viewportX, m.viewportY, m.viewportW, m.viewportH)

			// Send Game.state if it changed since last frame (for Canvas HD).
			// Marshal to JSON first, hash it cheaply, only gzip+encode if changed.
			if isCanvasHD && phase == domain.PhasePlaying {
				var stateObj any
				if srt, ok := game.(engine.ScriptRuntime); ok {
					stateObj = srt.State()
				}
				if data, err := json.Marshal(stateObj); err == nil {
					h := fnv.New64a()
					h.Write(data)
					if hash := h.Sum64(); hash != m.lastStateHash {
						if osc := render.EncodeStateOSC(data); osc != "" {
							oscData += osc
							m.lastStateHash = hash
						}
					}
				}
			}
		}
		if oscData != "" {
			m.SessionWriter.Write([]byte(oscData))
		}
	}

	// Emit OSC 52 clipboard sequence — write directly to session for enhanced clients,
	// or embed in view content for regular terminals (which handle OSC 52 natively).
	if m.pendingClipboard != "" {
		if m.SessionWriter != nil {
			m.SessionWriter.Write([]byte(render.EncodeOSC52(m.pendingClipboard)))
		}
		m.pendingClipboard = ""
	}
	view.SetContent(buf.ToString(m.ColorProfile))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	isLobby := !m.inActiveGame || phase == domain.PhaseNone

	if isLobby && m.lobbyWindow.FocusIdx == 4 {
		if cx, cy, visible := m.lobbyWindow.CursorPosition(); visible {
			if cursor := m.lobbyInput.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	} else if !isLobby && m.playingWindow.FocusIdx == 4 {
		// Playing command input has focus — show cursor.
		if cx, cy, visible := m.playingWindow.CursorPosition(); visible {
			if cursor := m.playingInput.Model.Cursor(); cursor != nil {
				cursor.Position.X = cx
				cursor.Position.Y = cy
				view.Cursor = cursor
			}
		}
	}
	if isLobby && m.teamEditing {
		if cursor := m.teamEditInput.Cursor(); cursor != nil {
			// Position cursor on the team name row in the right panel.
			// The team panel is at col 2 in the NCWindow grid. After grid layout,
			// its X position is computed. We calculate the Y row within the panel.
			teams := m.api.State().GetTeams()
			unassigned := m.api.State().UnassignedPlayers()
			idx := m.api.State().PlayerTeamIndex(m.playerID)
			row := 1 + len(unassigned) // "Unassigned" header + player rows
			for i := 0; i < idx && i < len(teams); i++ {
				row += 1 + 1 + len(teams[i].Players) // blank + team header + members
			}
			row += 1 // blank before current team
			// NCWindow starts at y=1 (after menu bar), no top border, so content starts at y=1.
			cursor.Position.Y = 1 + row
			// Team panel X: window left border (1) + chat width + divider (1) + swatch (3) + space (1)
			// Use the grid's computed position if available.
			if len(m.lobbyWindow.Children) > 2 {
				cx, _, _, _ := m.lobbyWindow.ChildRect(2) // team panel is child index 2
				cursor.Position.X += cx + 4              // +4 for " XX " (space + swatch + space before name)
			}
			view.Cursor = cursor
		}
	}
	return view
}

// renderLobby renders the lobby view using NC controls directly into the buffer.
// Layout: row 0 = NCMenuBar, rows 1..H-2 = NCWindow (chat + teams + cmd bar), row H-1 = NCStatusBar.
func (m *Model) renderLobby(buf *render.ImageBuffer, menus []domain.MenuDef) {
	// Update menu bar.
	m.lobbyMenuBar.Menus = menus

	// Update chat view.
	m.lobbyChatView.Lines = m.chatLines
	m.lobbyChatView.ScrollOffset = m.chatScrollOffset

	// Update team panel.
	teams := m.api.State().GetTeams()
	m.lobbyTeamPanel.Teams = teams
	m.lobbyTeamPanel.Unassigned = m.api.State().UnassignedPlayers()
	m.lobbyTeamPanel.MyTeamIdx = m.api.State().PlayerTeamIndex(m.playerID)
	m.lobbyTeamPanel.PlayerID = m.playerID
	m.lobbyTeamPanel.GetPlayer = m.api.State().GetPlayer
	m.lobbyTeamPanel.Editing = m.teamEditing
	if m.teamEditing {
		m.lobbyTeamPanel.EditValue = m.teamEditInput.Value()
	}
	m.lobbyTeamPanel.ShowCreate = !m.api.State().IsSoleMemberOfTeam(m.playerID)

	// Update status bar.
	statusLeft := fmt.Sprintf(" dev-null | %d players | uptime %s", m.api.State().PlayerCount(), m.api.Uptime())
	m.lobbyStatusBar.LeftText = statusLeft
	m.lobbyStatusBar.RightText = m.api.Clock().Now().Format(domain.TimeFormatDateTime) + " "

	// Render the full screen: menu bar + window + status bar.
	m.lobbyScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back from NCTextView (it may have been changed by scroll input).
	m.chatScrollOffset = m.lobbyChatView.ScrollOffset
}


func (m *Model) renderPlaying(buf *render.ImageBuffer, menus []domain.MenuDef, game domain.Game, gameName string, phase domain.GamePhase) {
	// Compute game viewport height (16:9 aspect ratio with min chat height).
	// Window interior = total - menuBar(1) - statusBar(1) - topBorder(1) - bottomBorder(1) = height - 4
	// Interior rows: gameView + divider(1) + chat + divider(1) + cmdInput(1) = gameH + chatH + 3
	interiorH := m.height - 4 // screen chrome (menu bar, status bar) + window borders
	gameH := m.width * 9 / 16
	chatH := interiorH - 3 - gameH // 3 = two dividers + command input
	minChatH := max(5, interiorH/3)
	if chatH < minChatH {
		chatH = minChatH
		gameH = interiorH - 3 - chatH
	}
	if gameH < 1 {
		gameH = 1
	}

	// Update gameview constraint for aspect-ratio sizing.
	m.playingWindow.Children[0].Constraint.MinH = gameH

	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	// Wire gameview rendering based on phase.
	switch phase {
	case domain.PhaseStarting:
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			m.renderStartingDialog(gbuf, game, displayName, x, y, w, h)
		}
		m.playingGameView.OnKey = nil // starting screen ignores game keys
	case domain.PhaseEnding:
		m.api.State().RLock()
		results := m.api.State().GameOverResults
		m.api.State().RUnlock()
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			if !game.RenderEnding(gbuf, m.playerID, x, y, w, h, results) {
				m.defaultRenderEnding(gbuf, results, x, y, w, h)
			}
		}
		m.playingGameView.OnKey = nil // ending screen ignores game keys
	default: // PhasePlaying
		if ncTree := game.Layout(m.playerID, m.width, gameH); ncTree != nil {
			// NC-tree game: reconcile into a GameWindow and render/route through it.
			m.gameWindow = widget.ReconcileGameWindow(m.gameWindow, ncTree,
				func(gbuf *render.ImageBuffer, bx, by, bw, bh int) { game.RenderAscii(gbuf, m.playerID, bx, by, bw, bh) },
				func(action string) { game.OnInput(m.playerID, action) })
			m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
				m.gameWindow.Window.RenderToBuf(gbuf, x, y, w, h, m.theme.LayerAt(0))
			}
			// Route keys to the reconciled window's focused control.
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		} else {
			m.gameWindow = nil
			m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
				game.RenderAscii(gbuf, m.playerID, x, y, w, h)
			}
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		}
	}

	// Blocks mode: render canvas as Unicode quadrant block characters
	// (2x2 pixels per cell, doubling effective resolution).
	if m.renderMode == domain.RenderModeBlocks && phase == domain.PhasePlaying {
		inner := m.playingGameView.RenderFn
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			if inner != nil {
				inner(gbuf, x, y, w, h)
			}
			img := game.RenderCanvasImage(m.playerID, w*2, h*4)
			if img != nil {
				render.ImageToQuadrants(img, gbuf, x, y, w, h)
			}
		}
	}

	// Update chat view.
	m.playingChatView.Lines = m.chatLines
	m.playingChatView.ScrollOffset = m.chatScrollOffset

	// Update menu bar and status bar.
	m.playingMenuBar.Menus = menus
	switch phase {
	case domain.PhaseStarting:
		st := m.api.State()
		st.RLock()
		isReady := st.StartingReady != nil && st.StartingReady[m.playerID]
		st.RUnlock()
		if isReady {
			m.playingStatusBar.LeftText = " Ready! Waiting for others..."
		} else {
			m.playingStatusBar.LeftText = " [Enter] Ready up"
		}
	case domain.PhaseEnding:
		remaining := 15 - int(time.Since(m.gameOverStart).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		m.playingStatusBar.LeftText = fmt.Sprintf(" [Enter] Continue to lobby (%ds remaining)", remaining)
	default:
		m.playingStatusBar.LeftText = " " + game.StatusBar(m.playerID)
	}
	m.playingStatusBar.RightText = ""

	// Capture viewport bounds for enhanced client OSC (wraps the render function).
	if m.IsEnhancedClient && m.playingGameView.RenderFn != nil {
		inner := m.playingGameView.RenderFn
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			m.viewportX, m.viewportY, m.viewportW, m.viewportH = x, y, w, h
			if m.renderMode == domain.RenderModePixels && phase == domain.PhasePlaying {
				// Pixels mode: fill viewport with placeholder cells. The client
				// treats these as transparent, showing the locally-rendered canvas through.
				// Menus/dialogs that overlap replace these with real cells.
				gbuf.Fill(x, y, w, h, render.CanvasCell, nil, nil, render.AttrNone)
			} else {
				inner(gbuf, x, y, w, h)
			}
		}
	}

	// Render the full screen.
	m.playingScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back.
	m.chatScrollOffset = m.playingChatView.ScrollOffset
}

// renderStartingDialog renders the starting screen as a centered dialog in the
// game viewport. The splash area uses the game's RenderStarting (or a default
// figlet title), and the status row shows a countdown + per-player ready state.
func (m *Model) renderStartingDialog(buf *render.ImageBuffer, game domain.Game, name string, x, y, w, h int) {
	// Read starting state.
	st := m.api.State()
	st.RLock()
	startingStart := st.StartingStart
	readyMap := st.StartingReady
	gameTeams := st.GameTeams
	players := make(map[string]*domain.Player, len(st.Players))
	for k, v := range st.Players {
		players[k] = v
	}
	st.RUnlock()

	// Countdown.
	elapsed := m.api.Clock().Now().Sub(startingStart).Seconds()
	remaining := 10 - int(elapsed)
	if remaining < 0 {
		remaining = 0
	}

	// Build status line: "Starting in Xs  [ ] alice  [x] bob"
	var parts []string
	parts = append(parts, fmt.Sprintf("Starting in %ds", remaining))
	for _, team := range gameTeams {
		for _, pid := range team.Players {
			p := players[pid]
			if p == nil {
				continue
			}
			check := " "
			if readyMap != nil && readyMap[pid] {
				check = "x"
			}
			parts = append(parts, fmt.Sprintf("[%s] %s", check, p.Name))
		}
	}
	m.startingStatus.Text = strings.Join(parts, "  ")

	// Wire splash to game's custom starting screen or default figlet.
	m.startingSplash.RenderFn = func(gbuf *render.ImageBuffer, sx, sy, sw, sh int) {
		if !game.RenderStarting(gbuf, m.playerID, sx, sy, sw, sh) {
			renderFigletSplash(gbuf, name, sx, sy, sw, sh)
		}
	}
	m.startingWindow.Title = name

	// Size the dialog: nearly fill the viewport with some margin.
	dlgW := w
	dlgH := h
	if dlgW > 4 {
		dlgW -= 4
	}
	if dlgH > 2 {
		dlgH -= 2
	}

	// Render into a sub-buffer, blit centered.
	layer := m.theme.LayerAt(1)
	sub := render.NewImageBuffer(dlgW, dlgH)
	m.startingWindow.RenderToBuf(sub, 0, 0, dlgW, dlgH, layer)

	dlgX := x + (w-dlgW)/2
	dlgY := y + (h-dlgH)/2
	buf.Blit(dlgX, dlgY, sub)
	render.BlitShadow(buf, dlgX, dlgY, dlgW, dlgH, m.theme.ShadowFg, m.theme.ShadowBg)
}

// renderFigletSplash renders a figlet title centered in the given area.
// Tries larry3d first, falls back to standard, then plain text.
func renderFigletSplash(buf *render.ImageBuffer, name string, x, y, w, h int) {
	lines := figletLines(name, "larry3d", w)
	if lines == nil {
		lines = figletLines(name, "", w)
	}
	if lines == nil {
		lines = []string{name}
	}

	topPad := (h - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i, line := range lines {
		row := y + topPad + i
		if row >= y+h {
			break
		}
		col := x + (w-len(line))/2
		if col < x {
			col = x
		}
		// Inherit the theme colors already painted by the Window fill.
		buf.WriteStringInherit(col, row, line)
	}
}

// figletLines renders text with the given font and returns split lines,
// or nil if the result is empty or too wide for the viewport.
func figletLines(text, font string, maxW int) []string {
	raw := strings.TrimRight(engine.Figlet(text, font), "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		if len(l) > maxW {
			return nil
		}
	}
	return lines
}

// defaultRenderEnding renders a figlet "GAME OVER" title with ranked results.
func (m *Model) defaultRenderEnding(buf *render.ImageBuffer, results []domain.GameResult, x, y, w, h int) {
	var lines []string

	// Figlet title.
	figletTitle := strings.TrimRight(engine.Figlet("GAME OVER", "slant"), "\n")
	figletLines := strings.Split(figletTitle, "\n")
	maxW := 0
	for _, l := range figletLines {
		if len(l) > maxW {
			maxW = len(l)
		}
	}
	if figletTitle != "" && maxW <= w {
		lines = append(lines, figletLines...)
	} else {
		lines = append(lines, "G A M E   O V E R")
	}
	lines = append(lines, "")

	// Results table.
	if len(results) > 0 {
		lines = append(lines, "")
		maxNameLen := 0
		for _, r := range results {
			if len(r.Name) > maxNameLen {
				maxNameLen = len(r.Name)
			}
		}
		for i, r := range results {
			pos := fmt.Sprintf("%d.", i+1)
			lines = append(lines, fmt.Sprintf("  %-3s %-*s  %s", pos, maxNameLen, r.Name, r.Result))
		}
	}

	// Center vertically.
	topPad := (h - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i, line := range lines {
		row := y + topPad + i
		if row >= y+h {
			break
		}
		col := x + (w-len(line))/2
		if col < x {
			col = x
		}
		buf.WriteString(col, row, line, nil, nil, render.AttrNone)
	}
}

