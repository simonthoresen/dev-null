package chrome

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"dev-null/internal/console"
	"dev-null/internal/domain"
	"dev-null/internal/engine"
	"dev-null/internal/render"
	"dev-null/internal/widget"
)

func (m Model) View() tea.View {
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

	// Enhanced client OSC protocol: send charmap data, game source, state, and viewport bounds.
	var oscPrefix string
	if m.IsEnhancedClient && m.inActiveGame && game != nil {
		// Send local/remote mode OSC once on game load or when toggled.
		if !m.localModeSent {
			if m.localRendering {
				oscPrefix += render.EncodeModeOSC("local")
			} else {
				oscPrefix += render.EncodeModeOSC("remote")
			}
			m.localModeSent = true
		}

		// Send game source files once on game load (for client-side local rendering).
		if m.localRendering && !m.gameSrcSent {
			for _, sf := range game.GameSource() {
				oscPrefix += render.EncodeGameSourceOSC(sf.Name, sf.Content)
			}
			m.gameSrcSent = true
		}
		if !m.charmapSent && !m.IsTerminalClient {
			if cm := game.CharMap(); cm != nil {
				oscPrefix += render.EncodeCharmapOSC(cm)
				if cm.Atlas != "" {
					atlasPath := filepath.Join(m.api.DataDir(), "charmaps", cm.Name, cm.Atlas)
					oscPrefix += render.EncodeAtlasOSC(atlasPath)
				}
			}
			m.charmapSent = true
		}
		if !m.assetsSent && !m.IsTerminalClient {
			assets := game.GameAssets()
			if len(assets) > 0 {
				oscPrefix += render.EncodeAssetManifestOSC(len(assets))
				for _, a := range assets {
					oscPrefix += render.EncodeAssetOSC(a.Name, a.Data)
				}
			}
			m.assetsSent = true
		}
		// Drain pending sound OSC commands (from JS playSound/stopSound via chatCh).
		for _, osc := range m.pendingSoundOSC {
			oscPrefix += osc
		}
		m.pendingSoundOSC = nil
		if m.viewportW > 0 && m.viewportH > 0 {
			oscPrefix += render.EncodeViewportOSC(m.viewportX, m.viewportY, m.viewportW, m.viewportH)

			// Send Game.state if it changed since last frame (for local rendering).
			if m.localRendering && phase == domain.PhasePlaying {
				var gameState any
				if srt, ok := game.(engine.ScriptRuntime); ok {
					gameState = srt.State()
				}
				if stateJSON := render.EncodeStateOSC(gameState); stateJSON != "" && stateJSON != m.lastStateJSON {
					oscPrefix += stateJSON
					m.lastStateJSON = stateJSON
				}
			}

			// Canvas frame: send PNG via OSC when render mode is Canvas (not for terminal clients).
			if m.renderMode == domain.RenderModeCanvas && phase == domain.PhasePlaying && !m.IsTerminalClient {
				m.api.State().RLock()
				canvasScale := m.api.State().CanvasScale
				m.api.State().RUnlock()
				pixelW := m.viewportW * canvasScale
				pixelH := m.viewportH * canvasScale
				pngData := game.RenderCanvas(m.playerID, pixelW, pixelH)
				oscPrefix += render.EncodeFrameOSC(pngData)
			}
		}
	}

	view.SetContent(oscPrefix + buf.ToString(m.ColorProfile))
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
func (m Model) renderLobby(buf *render.ImageBuffer, menus []domain.MenuDef) {
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


func (m Model) renderPlaying(buf *render.ImageBuffer, menus []domain.MenuDef, game domain.Game, gameName string, phase domain.GamePhase) {
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
			if !game.RenderStarting(gbuf, m.playerID, x, y, w, h) {
				m.defaultRenderStarting(gbuf, displayName, x, y, w, h)
			}
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
				func(gbuf *render.ImageBuffer, bx, by, bw, bh int) { game.Render(gbuf, m.playerID, bx, by, bw, bh) },
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
				game.Render(gbuf, m.playerID, x, y, w, h)
			}
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		}
	}

	// Quadrant mode: render canvas as Unicode quadrant block characters
	// (2x2 pixels per cell, doubling effective resolution).
	if m.renderMode == domain.RenderModeQuadrant && phase == domain.PhasePlaying {
		inner := m.playingGameView.RenderFn
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			if inner != nil {
				inner(gbuf, x, y, w, h)
			}
			img := game.RenderCanvasImage(m.playerID, w*2, h*2)
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
		player := m.api.State().GetPlayer(m.playerID)
		isAdmin := player != nil && player.IsAdmin
		if isAdmin {
			m.playingStatusBar.LeftText = " [Enter] Start game"
		} else {
			m.playingStatusBar.LeftText = " Waiting for host to start..."
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

	// Capture viewport bounds for enhanced/terminal client OSC (wraps the render function).
	if m.IsEnhancedClient && m.playingGameView.RenderFn != nil {
		inner := m.playingGameView.RenderFn
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			m.viewportX, m.viewportY, m.viewportW, m.viewportH = x, y, w, h
			inner(gbuf, x, y, w, h)
		}
	}

	// Render the full screen.
	m.playingScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back.
	m.chatScrollOffset = m.playingChatView.ScrollOffset
}

// defaultRenderStarting renders a figlet game name centered in the viewport.
func (m Model) defaultRenderStarting(buf *render.ImageBuffer, name string, x, y, w, h int) {
	figletTitle := strings.TrimRight(engine.Figlet(name, ""), "\n")
	var lines []string
	if figletTitle != "" {
		lines = strings.Split(figletTitle, "\n")
		// Check if figlet fits; fall back to plain text if too wide.
		maxW := 0
		for _, l := range lines {
			if len(l) > maxW {
				maxW = len(l)
			}
		}
		if maxW > w {
			lines = []string{name}
		}
	} else {
		lines = []string{name}
	}

	// Center vertically and horizontally.
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

// defaultRenderEnding renders a figlet "GAME OVER" title with ranked results.
func (m Model) defaultRenderEnding(buf *render.ImageBuffer, results []domain.GameResult, x, y, w, h int) {
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

func headerWithSpinner(text string, width int, spinner string) string {
	if width <= 0 {
		return ""
	}
	spinnerWidth := ansi.StringWidth(spinner)
	if width <= spinnerWidth {
		return truncateStyled(spinner, width)
	}
	left := truncateStyled(text, width-spinnerWidth-1)
	spaces := width - ansi.StringWidth(left) - spinnerWidth
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + spinner
}

func truncateStyled(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "")
}
