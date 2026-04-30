package chrome

import (
	"fmt"
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
	viewStart := time.Now()
	defer func() { m.api.CountView(time.Since(viewStart)) }()

	// Set monochrome on all theme layers so widgets use text cursor glyphs
	// (►/›) instead of relying on background-color highlighting alone.
	m.theme.SetMonochrome(m.ColorProfile <= colorprofile.ASCII)

	var view tea.View
	if m.width == 0 || m.height == 0 {
		view.SetContent("Loading DevNull...")
		view.AltScreen = true
		return view
	}

	// Single state snapshot — one RLock for all fields used by View() and its
	// sub-renderers. This replaces 7+ individual RLock calls per frame.
	st := m.api.State()
	st.RLock()
	game := st.ActiveGame
	gameName := st.GameName
	phase := st.GamePhase
	shaderElapsed := st.ElapsedSec
	var startReady bool
	if st.StartingReady != nil {
		startReady = st.StartingReady[m.playerID]
	}
	st.RUnlock()

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
		m.renderPlaying(buf, menus, game, gameName, phase, startReady)
	}

	// Overlay layers: render to sub-buffers, blit, then shadow via RecolorRect.
	shadowFg := m.theme.ShadowFg
	shadowBg := m.theme.ShadowBg
	if m.overlay.OpenMenu >= 0 {
		menuLayer := m.theme.LayerAt(1)
		if ddBuf, ddCol, ddRow := m.overlay.RenderDropdownBuf(menus, 0, m.width, m.height, menuLayer); ddBuf != nil {
			buf.Blit(ddCol, ddRow, ddBuf)
			render.BlitShadow(buf, ddCol, ddRow, ddBuf.Width, ddBuf.Height, shadowFg, shadowBg)
			for _, sub := range m.overlay.RenderSubMenusBuf(menus, 0, ddCol, ddBuf.Width, m.width, m.height, menuLayer) {
				buf.Blit(sub.Col, sub.Row, sub.Buf)
				render.BlitShadow(buf, sub.Col, sub.Row, sub.Buf.Width, sub.Buf.Height, shadowFg, shadowBg)
			}
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
	engine.ApplyShaders(m.shaders, buf, shaderElapsed)

	// Enhanced client OSC protocol: send game source, state, and viewport bounds.
	// OSC sequences are written directly to the session, bypassing Bubble Tea's cell renderer
	// which would consume them as styled string content instead of passing them through.
	isLocal := m.renderLocal
	if m.SessionWriter != nil && m.IsEnhancedClient && m.inActiveGame && game != nil {
		var oscData string
		// Send local/remote mode OSC once on game load or when mode changes.
		if !m.oscModeSent {
			if m.renderLocal {
				oscData += render.EncodeModeOSC("local")
			} else {
				oscData += render.EncodeModeOSC("remote")
			}
			// Tell the client which ID the server keyed it as, so local
			// rendering can look up Game.state.players[pid] correctly.
			oscData += render.EncodePlayerIDOSC(m.playerID)
			m.oscModeSent = true
		}

		// Send game source files once for Canvas HD (client-side rendering).
		if isLocal && !m.gameSrcSent {
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

			// Send Game.state to local renderers. First broadcast per session
			// is the full baseline; subsequent ones are depth-1 merge patches
			// containing only the top-level keys whose JSON bytes changed.
			if isLocal && phase == domain.PhasePlaying {
				if osc := m.encodeStateBroadcast(m.api.StateSnapshot()); osc != "" {
					oscData += osc
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
	m.lobbyTeamPanel.ShowCreate = !m.api.State().IsSoleMemberOfTeam(m.playerID)

	// Update status bar.
	statusLeft := fmt.Sprintf(" DevNull | %d players | uptime %s", m.api.State().PlayerCount(), m.api.Uptime())
	m.lobbyStatusBar.LeftText = statusLeft
	m.lobbyStatusBar.RightText = m.api.Clock().Now().Format(domain.TimeFormatDateTime) + " "

	// Render the full screen: menu bar + window + status bar.
	m.lobbyScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back from NCTextView (it may have been changed by scroll input).
	m.chatScrollOffset = m.lobbyChatView.ScrollOffset
}


func (m *Model) renderPlaying(buf *render.ImageBuffer, menus []domain.MenuDef, game domain.Game, gameName string, phase domain.GamePhase, startReady bool) {
	// Chat takes m.chatSize rows (5..10, configurable via View > Chat size).
	// Window interior = total - menuBar(1) - statusBar(1) - topBorder(1) - bottomBorder(1) = height - 4
	// Interior rows: gameView + divider(1) + chat(chatSize) + divider(1) + cmdInput(1) = gameH + chatSize + 3
	interiorH := m.height - 4
	gameH := interiorH - m.chatSize - 3
	if gameH < 1 {
		gameH = 1
	}

	// Update gameview constraint for aspect-ratio sizing.
	m.playingWindow.Children[0].Constraint.MinH = gameH

	displayName := gameName
	if gn := game.GameName(); gn != "" {
		displayName = gn
	}

	// Look up any pre-rendered frame from the tick goroutine. The tick goroutine
	// renders at (m.width-2, gameH) — the game viewport interior (window minus borders).
	// This must be done before the phase switch so cachedStatus is in scope for the status bar.
	preW := m.width - 2
	cachedBuf, cachedTree, cachedStatus, releaseCache := m.api.GetPreRenderedFrame(m.playerID, preW, gameH)
	if releaseCache != nil {
		defer releaseCache()
	}

	hasCanvas := game.HasCanvasMode()
	hasAscii := game.HasAsciiMode()

	// Wire gameview rendering based on phase.
	switch phase {
	case domain.PhaseStarting:
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			m.renderStartingScreen(gbuf, displayName, x, y, w, h)
		}
		m.playingGameView.OnKey = nil // starting screen ignores game keys
	default: // PhasePlaying
		if cachedTree != nil {
			// NC-tree game with a pre-cached Layout result: reconcile using the
			// cached tree and render normally. RenderAscii calls happen inside
			// RenderToBuf (pure Go widget + renderFn closures), which still use
			// Runtime.mu — but the Layout() JS call was already done in the tick goroutine.
			m.gameWindow = widget.ReconcileGameWindow(m.gameWindow, cachedTree,
				func(gbuf *render.ImageBuffer, bx, by, bw, bh int) { game.RenderAscii(gbuf, m.playerID, bx, by, bw, bh) },
				func(action string) { game.OnInput(m.playerID, action) })
			m.playingGameView.Inner = m.gameWindow.Window
			m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
				m.gameWindow.Window.RenderToBuf(gbuf, x, y, w, h, m.theme.LayerAt(0))
			}
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		} else if cachedBuf != nil && !hasCanvas {
			// Pure ascii game with a pre-rendered buffer: just blit it.
			// No JS call needed — this is the primary hot path at 16+ players.
			m.gameWindow = nil
			m.playingGameView.Inner = nil
			m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
				gbuf.Blit(x, y, cachedBuf)
			}
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		} else if hasCanvas {
			// Canvas game (with optional ascii overlay). The actual canvas
			// quadrants and ascii overlay compose are wired below in the
			// canvas-rendering block; here we just install OnKey.
			m.gameWindow = nil
			m.playingGameView.Inner = nil
			m.playingGameView.RenderFn = nil
			m.playingGameView.OnKey = func(key string) {
				game.OnInput(m.playerID, key)
			}
		} else {
			// Fallback: no pre-rendered cache yet (first frame or dimension mismatch).
			ncTree := game.Layout(m.playerID, m.width, gameH)
			if ncTree != nil {
				m.gameWindow = widget.ReconcileGameWindow(m.gameWindow, ncTree,
					func(gbuf *render.ImageBuffer, bx, by, bw, bh int) { game.RenderAscii(gbuf, m.playerID, bx, by, bw, bh) },
					func(action string) { game.OnInput(m.playerID, action) })
				m.playingGameView.Inner = m.gameWindow.Window
				m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
					m.gameWindow.Window.RenderToBuf(gbuf, x, y, w, h, m.theme.LayerAt(0))
				}
				m.playingGameView.OnKey = func(key string) {
					game.OnInput(m.playerID, key)
				}
			} else {
				m.gameWindow = nil
				m.playingGameView.Inner = nil
				m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
					game.RenderAscii(gbuf, m.playerID, x, y, w, h)
				}
				m.playingGameView.OnKey = func(key string) {
					game.OnInput(m.playerID, key)
				}
			}
		}
	}

	// Canvas compose path: when the game has renderCanvas and the player
	// is rendering server-side, we render the canvas as Unicode quadrant
	// blocks (bottom layer), then BlitOverlay the cached ascii buf on
	// top with transparency (cells untouched after Clear() let the
	// canvas show through). When local rendering is on, the client
	// composites the same way using its own LocalRenderer.
	if hasCanvas && !m.renderLocal && phase == domain.PhasePlaying {
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			// h*2 (rather than h*4) is lossless for quadrants and ~2x cheaper
			// in JS for SSH-Blocks. Aspect correction is sacrificed but at
			// 16-player wolf3d the alternative is dropping under 10fps.
			canvasW, canvasH := w*2, h*2
			// Tell the tick goroutine what size we want pre-rendered.
			m.api.SetPlayerCanvasNeed(m.playerID, canvasW, canvasH)
			// Render canvas → quadrants (bottom layer).
			if img, release := m.api.GetPreRenderedCanvas(m.playerID, canvasW, canvasH); img != nil {
				render.ImageToQuadrants(img, gbuf, x, y, w, h)
				release()
			} else {
				t0 := time.Now()
				img := game.RenderCanvasImage(m.playerID, canvasW, canvasH)
				m.api.CountCanvasRender(time.Since(t0))
				if img != nil {
					render.ImageToQuadrants(img, gbuf, x, y, w, h)
				}
			}
			// Ascii overlay (top layer, transparency-aware).
			if hasAscii && cachedBuf != nil {
				gbuf.BlitOverlay(x, y, cachedBuf)
			}
		}
	} else if !hasCanvas || m.renderLocal {
		// Not rendering canvas server-side any more — clear any stale
		// canvas request so the tick goroutine stops rendering canvases
		// for this player.
		m.api.SetPlayerCanvasNeed(m.playerID, 0, 0)
	}

	// Update chat view.
	m.playingChatView.Lines = m.chatLines
	m.playingChatView.ScrollOffset = m.chatScrollOffset

	// Update menu bar and status bar.
	m.playingMenuBar.Menus = menus
	switch phase {
	case domain.PhaseStarting:
		if startReady {
			m.playingStatusBar.LeftText = " Ready! Waiting for others..."
		} else {
			m.playingStatusBar.LeftText = " [Enter] Ready up"
		}
	default:
		if releaseCache != nil {
			// Use the status string pre-fetched by the tick goroutine.
			m.playingStatusBar.LeftText = " " + cachedStatus
		} else {
			m.playingStatusBar.LeftText = " " + game.StatusBar(m.playerID)
		}
	}
	m.playingStatusBar.RightText = ""

	// Capture viewport bounds for enhanced client OSC (wraps the render function).
	// The wrapper is installed unconditionally for enhanced clients in PhasePlaying
	// because canvas-only games leave RenderFn nil (canvas compose lives in the
	// !renderLocal branch above) and we still need to fill placeholder cells so
	// the client's local renderer has somewhere to composite into.
	if m.IsEnhancedClient {
		inner := m.playingGameView.RenderFn
		m.playingGameView.RenderFn = func(gbuf *render.ImageBuffer, x, y, w, h int) {
			m.viewportX, m.viewportY, m.viewportW, m.viewportH = x, y, w, h
			if m.renderLocal && phase == domain.PhasePlaying {
				// Local mode (Render locally toggle): fill viewport with placeholder
				// cells. The client renders locally and composites these placeholders.
				gbuf.Fill(x, y, w, h, render.CanvasCell, nil, nil, render.AttrNone)
			} else if inner != nil {
				inner(gbuf, x, y, w, h)
			}
		}
	}

	// Render the full screen.
	m.playingScreen.RenderToBuf(buf, 0, 0, m.width, m.height, m.theme)

	// Sync chatScrollOffset back.
	m.chatScrollOffset = m.playingChatView.ScrollOffset
}

// renderStartingScreen fills the game viewport with the starting splash:
// figlet title, per-player ready checkboxes with countdown, and the
// focused Ready button. No bordered sub-window — the splash is part of
// the game viewport just like the gameplay render and the game-over
// screen, so transitions between phases do not jump in layout.
func (m *Model) renderStartingScreen(buf *render.ImageBuffer, name string, x, y, w, h int) {
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

	remaining := 10 - int(m.api.Clock().Now().Sub(startingStart).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	// "Starting in Xs  [ ] alice  [x] bob"
	parts := []string{fmt.Sprintf("Starting in %ds", remaining)}
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
	status := strings.Join(parts, "  ")

	// Lay out vertically in the viewport:
	//   figlet title (variable height), blank, status row, blank, button row.
	// The content block is centered vertically in the viewport.
	titleLines := figletLines(name, "larry3d", w)
	if titleLines == nil {
		titleLines = figletLines(name, "", w)
	}
	if titleLines == nil {
		titleLines = []string{name}
	}
	btnLabel := "[ " + m.phaseReadyButton.Label + " ]"
	contentH := len(titleLines) + 1 + 1 + 1 + 1 // title + gap + status + gap + button
	topPad := (h - contentH) / 2
	if topPad < 0 {
		topPad = 0
	}

	row := y + topPad
	for _, line := range titleLines {
		if row >= y+h {
			break
		}
		col := x + (w-len(line))/2
		if col < x {
			col = x
		}
		// Inherit the theme bg/fg that the Window's fill just painted —
		// writing with explicit nil colors produces black-on-black.
		buf.WriteStringInherit(col, row, line)
		row++
	}
	row++ // blank after title
	if row < y+h {
		col := x + (w-len(status))/2
		if col < x {
			col = x
		}
		buf.WriteStringInherit(col, row, status)
		row++
	}
	row++ // blank before button
	if row < y+h {
		btnX := x + (w-len(btnLabel))/2
		if btnX < x {
			btnX = x
		}
		m.phaseReadyButton.Render(buf, btnX, row, len(btnLabel), 1, true, m.theme.LayerAt(0))
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

// Game-over rendering was removed along with PhaseEnding — results go
// straight to chat history (see server's checkGameOver) where they
// persist for post-game discussion and can be scrolled via PgUp/PgDn.

