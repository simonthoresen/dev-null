package engine

import (
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"dev-null/internal/domain"
	"dev-null/internal/render"
)

// JSCallTimeout is how long a JS method can run before being interrupted.
const JSCallTimeout = 2 * time.Second

// traceCall logs entry/exit of a JS method. Returns a function to call on exit.
func traceCall(_ *goja.Runtime, method string) func() {
	start := time.Now()
	slog.Debug("JS enter", "method", method)
	return func() {
		dur := time.Since(start)
		if dur > 100*time.Millisecond {
			slog.Warn("JS slow call", "method", method, "duration", dur)
		} else {
			slog.Debug("JS exit", "method", method, "duration", dur)
		}
	}
}

// Watchdog starts a goroutine that interrupts the VM after timeout.
// Call the returned cancel func when the JS call completes.
// Used by plugin.go for per-player VMs. For the shared game VM use vmWatchdog.
func Watchdog(vm *goja.Runtime, method string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(JSCallTimeout):
			slog.Error("JS call timed out, interrupting VM", "method", method, "timeout", JSCallTimeout)
			vm.Interrupt("timeout: " + method)
		}
	}()
	return func() { close(done) }
}

// vmWatchdog is a reusable watchdog for a single JS VM. Unlike Watchdog(), it
// does not spawn a new goroutine per call — it reuses a single time.AfterFunc
// timer that is reset before each JS call and stopped after. This eliminates
// the 160+ goroutine/sec churn created when rendering for 16+ players.
type vmWatchdog struct {
	timer *time.Timer
	mu    sync.Mutex
	cur   string
}

func newVMWatchdog(vm *goja.Runtime) *vmWatchdog {
	w := &vmWatchdog{}
	// AfterFunc with a large initial duration so it never fires before the first arm().
	w.timer = time.AfterFunc(time.Duration(1<<62), func() {
		w.mu.Lock()
		m := w.cur
		w.mu.Unlock()
		slog.Error("JS call timed out, interrupting VM", "method", m, "timeout", JSCallTimeout)
		vm.Interrupt("timeout: " + m)
	})
	w.timer.Stop()
	return w
}

func (w *vmWatchdog) arm(method string) {
	w.mu.Lock()
	w.cur = method
	w.mu.Unlock()
	w.timer.Stop()
	w.timer.Reset(JSCallTimeout)
}

func (w *vmWatchdog) disarm() {
	w.timer.Stop()
}

// LOCK ORDERING INVARIANT
//
// The system has two primary mutexes:
//   1. CentralState.mu   — protects shared game state (state.go)
//   2. Runtime.mu        — protects the goja JS VM (this file)
//
// Permitted lock order: Runtime.mu → (nothing external)
//
// Runtime must NEVER hold or acquire CentralState.mu. To enforce this
// structurally, Runtime has no reference to CentralState. All data flows:
//   - Teams data: cached snapshot set by server via SetTeamsCache()
//   - Chat output: buffered channel drained by a server goroutine
//
// Callers (server.go, chrome.go) must release state.mu BEFORE calling
// any Runtime Game method (Load, Begin, View, OnInput, etc.).

// Runtime wraps a goja JS runtime and implements domain.Game.
type Runtime struct {
	mu       sync.Mutex
	vm       *goja.Runtime
	watchdog *vmWatchdog
	baseDir string       // directory containing the game file (for include() resolution)
	dataDir string       // root data directory (for resolving charmaps, etc.)
	clock   domain.Clock // server clock exposed to JS as now()

	commands    []domain.Command
	cachedTeams []map[string]any   // snapshot set by server; read by JS teams()
	logFn       func(string)
	chatCh      chan domain.Message // drained by server goroutine; closed on unload

	// SourceFiles records all JS files loaded (main + includes), in order.
	// Used to send game source to enhanced clients for local rendering.
	SourceFiles []domain.GameSourceFile

	// game object methods (nil if not defined)
	initFn         goja.Callable // init(ctx, savedState) -> initial state (mandatory)
	updateFn       goja.Callable // update(state, dt, events, ctx)
	renderAsciiFn  goja.Callable // renderAscii(state, me, cells)
	renderCanvasFn goja.Callable // renderCanvas(state, me, canvas)
	layoutFn       goja.Callable // layout(state, me) -> widget tree
	statusBarFn    goja.Callable // statusBar(state, me) -> string
	commandBarFn   goja.Callable // commandBar(state, me) -> string
	resolveMeFn    goja.Callable // optional: resolveMe(state, playerID) -> me

	ctxObj        *goja.Object     // prebuilt ctx passed to server-side hooks
	pendingEvents []map[string]any // queued inputs/joins/leaves drained each Update

	// lifecycle
	gameNameProp  string
	teamRangeProp domain.TeamRange
	beginFn       goja.Callable
	endFn         goja.Callable
	unloadFn      goja.Callable
	suspendFn     goja.Callable // optional: returns session snapshot for suspend saves
	resumeFn      goja.Callable // optional: restores session snapshot; falls back to beginFn

	// gameOver() callback state — set by JS, detected by tick loop
	gameOverPending bool
	gameOverResults []domain.GameResult // results passed to gameOver()

	menus        []domain.MenuDef
	showDialogFn func(playerID string, d domain.DialogRequest) // injected by server

	isFolderGame bool // true when the game was loaded from <name>/main.js (not a flat .js file)

	elapsedTime float64 // cumulative game time in seconds, injected into Game.state._gameTime

	// Canvas reuse: keyed by playerID so each player's canvas is reused across
	// frames without resize thrashing. Runtime.mu serializes all renders, so
	// the map needs no additional locking.
	canvasCache map[string]canvasEntry
}

type canvasEntry struct {
	canvas *JSCanvas
	w, h   int
}

// LoadGame loads and executes a game script (.js), extracts the Game
// object, and returns a domain.Game. Load() is NOT called here — the server
// calls it after teams are set up, before PhaseStarting.
func LoadGame(path string, logFn func(string), chatCh chan domain.Message, clock domain.Clock, dataDir string) (domain.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	vm := goja.New()
	rt := &Runtime{
		vm:           vm,
		watchdog:     newVMWatchdog(vm),
		baseDir:      filepath.Dir(path),
		logFn:        logFn,
		chatCh:       chatCh,
		clock:        clock,
		dataDir:      dataDir,
		isFolderGame: filepath.Base(path) == "main.js",
		canvasCache:  map[string]canvasEntry{},
	}

	// Record the main source file.
	rt.SourceFiles = append(rt.SourceFiles, domain.GameSourceFile{
		Name:    filepath.Base(path),
		Content: string(src),
	})

	rt.registerGlobals()

	_, err = rt.vm.RunScript(path, string(src))
	if err != nil {
		return nil, fmt.Errorf("execute game script: %w", err)
	}

	if err := rt.extractGameObject(); err != nil {
		return nil, fmt.Errorf("extract game object: %w", err)
	}

	return rt, nil
}

// Load runs the game's init(ctx, savedState) hook and installs its return
// value as Game.state. savedState is the JSON-decoded value previously
// returned by unload() (or nil for the first ever load of a game). Suspend
// resume is a separate path — it goes through Resume(sessionState) below.
func (r *Runtime) Load(savedState any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Load")
	defer traceCall(r.vm, "Load")()
	r.watchdog.arm("Load")
	defer r.watchdog.disarm()

	// goja.ToValue(nil) yields goja.Null(), so games see `savedState === null`
	// on the first load and a real value once unload() has run at least once.
	saved := r.vm.ToValue(savedState)
	res, err := r.initFn(goja.Undefined(), r.ctxObj, saved)
	if err == nil && res != nil && !goja.IsUndefined(res) && !goja.IsNull(res) {
		if gameObj := r.vm.Get("Game").ToObject(r.vm); gameObj != nil {
			gameObj.Set("state", res)
		}
	}
}

func (r *Runtime) Begin() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elapsedTime = 0
	if r.beginFn == nil {
		return
	}
	r.injectStateTeams()
	defer r.recoverJS("Begin")
	defer traceCall(r.vm, "Begin")()
	r.watchdog.arm("Begin")
	defer r.watchdog.disarm()
	_, _ = r.beginFn(goja.Undefined(), r.currentState(), r.ctxObj)
}

func (r *Runtime) End() {
	if r.endFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("End")
	defer traceCall(r.vm, "End")()
	r.watchdog.arm("End")
	defer r.watchdog.disarm()
	r.injectStateTeams()
	_, _ = r.endFn(goja.Undefined(), r.currentState(), r.ctxObj)
}

// currentState reads Game.state from the VM without locking. Callers must hold r.mu.
func (r *Runtime) currentState() goja.Value {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return goja.Undefined()
	}
	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return goja.Undefined()
	}
	v := gameObj.Get("state")
	if v == nil {
		return goja.Undefined()
	}
	return v
}

func (r *Runtime) extractGameObject() error {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return fmt.Errorf("script must define a global 'Game' object")
	}

	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return fmt.Errorf("'Game' is not an object")
	}

	r.updateFn       = extractCallable(gameObj, "update")
	r.renderAsciiFn  = extractCallable(gameObj, "renderAscii")
	r.renderCanvasFn = extractCallable(gameObj, "renderCanvas")
	r.layoutFn       = extractCallable(gameObj, "layout")
	r.statusBarFn    = extractCallable(gameObj, "statusBar")
	r.commandBarFn   = extractCallable(gameObj, "commandBar")
	r.initFn         = extractCallable(gameObj, "init")
	r.resolveMeFn    = extractCallable(gameObj, "resolveMe")
	if r.initFn == nil {
		return fmt.Errorf("Game must define an init(ctx) function")
	}
	r.beginFn   = extractCallable(gameObj, "begin")
	r.endFn     = extractCallable(gameObj, "end")
	r.unloadFn  = extractCallable(gameObj, "unload")
	r.suspendFn = extractCallable(gameObj, "suspend")
	r.resumeFn  = extractCallable(gameObj, "resume")
	r.ctxObj    = r.buildCtxObject()

	// Read gameName property (string, not callable)
	if v := gameObj.Get("gameName"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		r.gameNameProp = v.String()
	}

	// Read teamRange property: {min, max}
	if v := gameObj.Get("teamRange"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		obj := v.ToObject(r.vm)
		if obj != nil {
			if mv := obj.Get("min"); mv != nil && !goja.IsUndefined(mv) {
				r.teamRangeProp.Min = int(mv.ToInteger())
			}
			if mv := obj.Get("max"); mv != nil && !goja.IsUndefined(mv) {
				r.teamRangeProp.Max = int(mv.ToInteger())
			}
		}
	}


	return nil
}

func extractCallable(obj *goja.Object, name string) goja.Callable {
	val := obj.Get(name)
	if val == nil || goja.IsUndefined(val) {
		return nil
	}
	fn, ok := goja.AssertFunction(val)
	if !ok {
		return nil
	}
	return fn
}

// Implement domain.Game

func (r *Runtime) OnPlayerJoin(playerID, playerName string) {
	r.mu.Lock()
	r.pendingEvents = append(r.pendingEvents, map[string]any{
		"type":       "join",
		"playerID":   playerID,
		"playerName": playerName,
	})
	r.mu.Unlock()
}

func (r *Runtime) OnPlayerLeave(playerID string) {
	r.mu.Lock()
	r.pendingEvents = append(r.pendingEvents, map[string]any{
		"type":     "leave",
		"playerID": playerID,
	})
	r.mu.Unlock()
}

func (r *Runtime) OnInput(playerID, key string) {
	r.mu.Lock()
	r.pendingEvents = append(r.pendingEvents, map[string]any{
		"type":     "input",
		"playerID": playerID,
		"key":      key,
	})
	r.mu.Unlock()
}

func (r *Runtime) Update(dt float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elapsedTime += dt
	defer r.recoverJS("Update")
	defer traceCall(r.vm, "Update")()
	r.watchdog.arm("Update")
	defer r.watchdog.disarm()

	if r.updateFn != nil {
		// Refresh live state.teams and drain pending events.
		r.injectStateTeams()
		events := r.pendingEvents
		r.pendingEvents = nil
		events = append(events, map[string]any{"type": "tick"})
		_, _ = r.updateFn(goja.Undefined(),
			r.currentState(),
			r.vm.ToValue(dt),
			r.vm.ToValue(events),
			r.ctxObj,
		)
	}

	// Auto-inject _gameTime (elapsed game time, seconds since begin) into
	// Game.state so canvas/local renderers always have the current time, even
	// if the game itself never writes to state during update().
	r.injectStateTime()
}

// injectStateTime sets Game.state._gameTime to the cumulative elapsed time
// in seconds. If Game.state is nil/undefined it creates { _gameTime: ... };
// otherwise it merges _gameTime into the existing object. Must be called
// with r.mu held.
func (r *Runtime) injectStateTime() {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return
	}
	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return
	}
	stateVal := gameObj.Get("state")
	if stateVal == nil || goja.IsUndefined(stateVal) || goja.IsNull(stateVal) {
		// Game.state not set — create it with just _gameTime
		obj := r.vm.NewObject()
		obj.Set("_gameTime", r.elapsedTime)
		gameObj.Set("state", obj)
		return
	}
	stateObj := stateVal.ToObject(r.vm)
	if stateObj != nil {
		stateObj.Set("_gameTime", r.elapsedTime)
	}
}

// injectStateTeams writes the cached teams onto the live Game.state so
// game code on the server sees state.teams during begin/update without
// having to call teams(). Export-time overlay in State() handles the
// client-synced snapshot; this handles the server-live object.
// Must be called with r.mu held. No-op when no teams are cached yet.
func (r *Runtime) injectStateTeams() {
	if r.cachedTeams == nil {
		return
	}
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return
	}
	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return
	}
	stateVal := gameObj.Get("state")
	if stateVal == nil || goja.IsUndefined(stateVal) || goja.IsNull(stateVal) {
		return
	}
	stateObj := stateVal.ToObject(r.vm)
	if stateObj == nil {
		return
	}
	stateObj.Set("teams", r.vm.ToValue(r.cachedTeams))
}

func (r *Runtime) RenderAscii(buf *render.ImageBuffer, playerID string, x, y, width, height int) {
	if r.renderAsciiFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderAscii")
	defer traceCall(r.vm, "RenderAscii")()
	r.watchdog.arm("RenderAscii")
	defer r.watchdog.disarm()

	// renderAscii(state, me, cells). Framework resolves me; if the game's
	// resolveMe returns null we skip render so chrome/client can draw the
	// not-ready splash.
	me := r.resolveMe(playerID)
	if goja.IsUndefined(me) || goja.IsNull(me) {
		return
	}
	cells := r.newJSImageBuffer(buf, x, y, width, height)
	cells["ATTR_NONE"] = int(render.AttrNone)
	cells["ATTR_BOLD"] = int(render.AttrBold)
	cells["ATTR_FAINT"] = int(render.AttrFaint)
	cells["ATTR_ITALIC"] = int(render.AttrItalic)
	cells["ATTR_UNDERLINE"] = int(render.AttrUnderline)
	cells["ATTR_REVERSE"] = int(render.AttrReverse)
	cells["log"] = func(msg string) {
		slog.Debug("cells.log", "game", r.gameNameProp, "msg", msg)
	}
	if _, err := r.renderAsciiFn(goja.Undefined(), r.currentState(), me, r.vm.ToValue(cells)); err != nil {
		slog.Error("JS renderAscii error", "error", err)
	}
}

// resolveMe produces the "me" value passed to render hooks. Prefers the
// game-defined resolveMe hook; otherwise falls back to state.players[pid].
// Returns goja undefined when me can't be resolved, signalling the framework
// to draw the not-ready splash instead of invoking render.
// Must be called with r.mu held.
func (r *Runtime) resolveMe(playerID string) goja.Value {
	state := r.currentState()
	if r.resolveMeFn != nil {
		me, err := r.resolveMeFn(goja.Undefined(), state, r.vm.ToValue(playerID))
		if err != nil {
			slog.Error("JS resolveMe error", "error", err)
			return goja.Undefined()
		}
		if me == nil || goja.IsNull(me) {
			return goja.Undefined()
		}
		return me
	}
	// Default: return state.players[pid] if it exists, else a minimal
	// {id: pid}. The default ALWAYS returns something — games that want
	// the "not ready" splash override resolveMe to return null when
	// their player isn't spawned yet.
	stateObj := state.ToObject(r.vm)
	if stateObj == nil {
		return r.minimalMe(playerID)
	}
	players := stateObj.Get("players")
	if players == nil || goja.IsUndefined(players) || goja.IsNull(players) {
		return r.minimalMe(playerID)
	}
	playersObj := players.ToObject(r.vm)
	if playersObj == nil {
		return r.minimalMe(playerID)
	}
	me := playersObj.Get(playerID)
	if me == nil || goja.IsUndefined(me) || goja.IsNull(me) {
		return r.minimalMe(playerID)
	}
	return me
}

// minimalMe returns {id: playerID} — the default me for games that don't
// track per-player state in state.players.
func (r *Runtime) minimalMe(playerID string) goja.Value {
	obj := r.vm.NewObject()
	obj.Set("id", playerID)
	return obj
}

func (r *Runtime) Layout(playerID string, width, height int) *domain.WidgetNode {
	if r.layoutFn == nil {
		return nil // framework will fall back to wrapping RenderAscii() in a gameview node
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Layout")
	defer traceCall(r.vm, "Layout")()
	r.watchdog.arm("Layout")
	defer r.watchdog.disarm()
	// layout(state, me) — same shape as renderAscii/statusBar/commandBar.
	// Width/height are not passed: the widget tree itself encodes sizing
	// via weight/width/height node properties, and layouts that need to
	// branch on viewport size should read me/state instead.
	me := r.resolveMe(playerID)
	if goja.IsUndefined(me) || goja.IsNull(me) {
		return nil
	}
	val, err := r.layoutFn(goja.Undefined(), r.currentState(), me)
	if err != nil {
		slog.Error("JS Layout error", "error", err)
		return nil
	}
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	return gojaToWidgetNode(r.vm, val)
}

func (r *Runtime) StatusBar(playerID string) string {
	if r.statusBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("StatusBar")
	defer traceCall(r.vm, "StatusBar")()
	r.watchdog.arm("StatusBar")
	defer r.watchdog.disarm()
	val, err := r.statusBarFn(goja.Undefined(), r.currentState(), r.resolveMe(playerID))
	if err != nil {
		slog.Error("JS StatusBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *Runtime) CommandBar(playerID string) string {
	if r.commandBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("CommandBar")
	defer traceCall(r.vm, "CommandBar")()
	r.watchdog.arm("CommandBar")
	defer r.watchdog.disarm()
	val, err := r.commandBarFn(goja.Undefined(), r.currentState(), r.resolveMe(playerID))
	if err != nil {
		slog.Error("JS CommandBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *Runtime) Commands() []domain.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.Command, len(r.commands))
	copy(result, r.commands)
	return result
}

// Unload calls the optional JS unload() hook to collect persistent state
// (high scores, unlocks, etc.), then interrupts the VM. Returns the exported
// state value, or nil.
func (r *Runtime) Unload() any {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result any
	if r.unloadFn != nil {
		func() {
			defer r.recoverJS("Unload")
			defer traceCall(r.vm, "Unload")()
			r.watchdog.arm("Unload")
			defer r.watchdog.disarm()
			val, err := r.unloadFn(goja.Undefined())
			if err == nil && val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
				result = val.Export()
			}
		}()
	}

	r.vm.Interrupt("game unloaded")
	r.canvasCache = map[string]canvasEntry{}
	return result
}

// Suspend calls the optional JS suspend() hook to collect the mid-session
// snapshot for a suspend save. Does NOT interrupt the VM — Unload() is called
// immediately after by the server to collect persistent state.
// Returns the exported snapshot, or nil if the hook is not defined.
func (r *Runtime) Suspend() any {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.suspendFn == nil {
		return nil
	}
	var result any
	func() {
		defer r.recoverJS("Suspend")
		defer traceCall(r.vm, "Suspend")()
		r.watchdog.arm("Suspend")
		defer r.watchdog.disarm()
		val, err := r.suspendFn(goja.Undefined())
		if err == nil && val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
			result = val.Export()
		}
	}()
	return result
}

// Resume calls the optional JS resume(sessionState) hook with the session
// snapshot from a suspend save. If the hook is not defined, falls back to
// calling Begin() so existing games without a resume hook still work.
func (r *Runtime) Resume(sessionState any) {
	if r.resumeFn != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.elapsedTime = 0
		defer r.recoverJS("Resume")
		defer traceCall(r.vm, "Resume")()
		r.watchdog.arm("Resume")
		defer r.watchdog.disarm()
		_, _ = r.resumeFn(goja.Undefined(), r.vm.ToValue(sessionState))
		return
	}
	// No resume hook — fall back to begin().
	r.Begin()
}

func (r *Runtime) Menus() []domain.MenuDef {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menus
}

// SetTeamsCache replaces the cached teams snapshot that JS teams() returns.
// Called by the server after loading teams or when a player reconnects.
func (r *Runtime) SetTeamsCache(teams []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedTeams = teams
}

// --- Lifecycle methods (part of Game interface) ---

func (r *Runtime) GameName() string {
	return r.gameNameProp
}

func (r *Runtime) TeamRange() domain.TeamRange {
	return r.teamRangeProp
}

func (r *Runtime) HasCanvasMode() bool {
	return r.renderCanvasFn != nil
}

func (r *Runtime) RenderCanvas(playerID string, width, height int) []byte {
	if r.renderCanvasFn == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderCanvas")
	defer traceCall(r.vm, "RenderCanvas")()
	r.watchdog.arm("RenderCanvas")
	defer r.watchdog.disarm()

	canvas := r.getCanvas(playerID, width, height)
	canvasObj := canvas.ToJSObject(r.vm)
	if err := r.invokeRenderCanvasFn(playerID, canvasObj, width, canvas.height); err != nil {
		slog.Error("JS RenderCanvas error", "error", err)
		return nil
	}

	data, err := canvas.ToPNG()
	if err != nil {
		slog.Error("Canvas PNG encoding error", "error", err)
		return nil
	}
	return data
}

func (r *Runtime) RenderCanvasImage(playerID string, width, height int) *image.RGBA {
	if r.renderCanvasFn == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderCanvasImage")
	defer traceCall(r.vm, "RenderCanvasImage")()
	r.watchdog.arm("RenderCanvasImage")
	defer r.watchdog.disarm()

	canvas := r.getCanvas(playerID, width, height)
	canvasObj := canvas.ToJSObject(r.vm)
	if err := r.invokeRenderCanvasFn(playerID, canvasObj, width, canvas.height); err != nil {
		slog.Error("JS RenderCanvasImage error", "error", err)
		return nil
	}
	return canvas.ToRGBA()
}

// getCanvas returns the cached canvas for the given player and dimensions.
// If dimensions match the cached entry the canvas is renewed in-place; otherwise
// a new canvas is created and stored. Must be called with r.mu held.
func (r *Runtime) getCanvas(playerID string, w, h int) *JSCanvas {
	if e, ok := r.canvasCache[playerID]; ok && e.w == w && e.h == h {
		e.canvas.Renew()
		return e.canvas
	}
	c := NewJSCanvas(w, h, 1.0)
	r.canvasCache[playerID] = canvasEntry{canvas: c, w: w, h: h}
	return c
}

// invokeRenderCanvasFn calls the game's renderCanvas(state, me, canvas) with
// framework-resolved me. Installs canvas.log as a narrow debug escape hatch.
// Must be called with r.mu held. No-op when resolveMe returns null (the
// framework-drawn splash is expected instead).
func (r *Runtime) invokeRenderCanvasFn(playerID string, canvasObj map[string]any, width, height int) error {
	_ = width
	_ = height
	me := r.resolveMe(playerID)
	if goja.IsUndefined(me) || goja.IsNull(me) {
		return nil
	}
	canvasObj["log"] = func(msg string) {
		slog.Debug("canvas.log", "game", r.gameNameProp, "msg", msg)
	}
	_, err := r.renderCanvasFn(goja.Undefined(), r.currentState(), me, r.vm.ToValue(canvasObj))
	return err
}

// IsGameOverPending returns true if JS called gameOver() and clears the flag.
func (r *Runtime) IsGameOverPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.gameOverPending {
		return false
	}
	r.gameOverPending = false
	return true
}

// GameOverResults returns the results array passed to gameOver().
func (r *Runtime) GameOverResults() []domain.GameResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameOverResults
}

// State returns the current value of the JS Game.state property, with
// framework-injected read-only keys (currently: teams) overlaid on top so
// clients receiving this state through the OSC sync get them for free.
// Used by the framework for OSC push to local renderers. Returns nil if
// Game.state is not set.
//
// The overlay is done on the exported Go value, not on the live JS object,
// so the game's own reads of Game.state (during update/render on the
// server) are unaffected. Games that author a key named "teams" in their
// own state will lose it here — that's the contract for framework-reserved
// keys and documented in game-contract.md.
func (r *Runtime) State() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return nil
	}
	obj := gameVal.ToObject(r.vm)
	if obj == nil {
		return nil
	}
	v := obj.Get("state")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	exported := v.Export()
	if m, ok := exported.(map[string]any); ok && r.cachedTeams != nil {
		// Copy, then overlay. We don't mutate the exported map in place
		// because goja's Export may return a live-ish view in some versions,
		// and we'd rather be defensive than debug a spooky aliasing bug
		// later.
		out := make(map[string]any, len(m)+1)
		for k, val := range m {
			out[k] = val
		}
		out["teams"] = r.cachedTeams
		return out
	}
	return exported
}

// GameSource returns all JS source files for client-side replication.
func (r *Runtime) GameSource() []domain.GameSourceFile {
	return r.SourceFiles
}

// GameAssets returns the binary asset files (audio, images) bundled alongside
// a folder-based game. Returns nil for single-file games.
func (r *Runtime) GameAssets() []domain.GameAsset {
	if !r.isFolderGame {
		return nil
	}
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return nil
	}
	allowed := map[string]bool{
		".ogg": true, ".mp3": true, ".wav": true,
		".png": true, ".jpg": true, ".jpeg": true,
	}
	var assets []domain.GameAsset
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !allowed[ext] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.baseDir, e.Name()))
		if err != nil {
			slog.Warn("game asset read failed", "file", e.Name(), "error", err)
			continue
		}
		assets = append(assets, domain.GameAsset{Name: e.Name(), Data: data})
	}
	return assets
}

// ChatCh returns the channel used by JS chat()/chatPlayer() to send messages.
func (r *Runtime) ChatCh() chan domain.Message {
	return r.chatCh
}

// CloseChatCh closes the chat channel. Called during game unload.
func (r *Runtime) CloseChatCh() {
	if r.chatCh != nil {
		close(r.chatCh)
	}
}

// SetShowDialogFn sets the callback used by JS messageBox() to show dialogs.
func (r *Runtime) SetShowDialogFn(fn func(playerID string, d domain.DialogRequest)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.showDialogFn = fn
}

func (r *Runtime) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in game method", "method", method, "panic", rec)
	}
}
