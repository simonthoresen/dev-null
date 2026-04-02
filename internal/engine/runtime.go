package engine

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dop251/goja"

	"null-space/internal/domain"
	"null-space/internal/render"
)

// JSCallTimeout is how long a JS method can run before being interrupted.
const JSCallTimeout = 2 * time.Second

// TraceJS logs entry/exit of a JS method. Returns a function to call on exit.
func TraceJS(_ *goja.Runtime, method string) func() {
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

// WatchdogJS starts a goroutine that interrupts the VM after timeout.
// Call the returned cancel func when the JS call completes.
func WatchdogJS(vm *goja.Runtime, method string) func() {
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

// LOCK ORDERING INVARIANT
//
// The system has two primary mutexes:
//   1. CentralState.mu   — protects shared game state (state.go)
//   2. JSRuntime.mu       — protects the goja JS VM (this file)
//
// Permitted lock order: JSRuntime.mu → (nothing external)
//
// JSRuntime must NEVER hold or acquire CentralState.mu. To enforce this
// structurally, JSRuntime has no reference to CentralState. All data flows:
//   - Teams data: cached snapshot set by server via SetTeamsCache()
//   - Chat output: buffered channel drained by a server goroutine
//
// Callers (server.go, chrome.go) must release state.mu BEFORE calling
// any JSRuntime Game method (Init, Start, View, OnInput, etc.).

// JSRuntime wraps a goja JS runtime and implements domain.Game.
type JSRuntime struct {
	mu      sync.Mutex
	vm      *goja.Runtime
	baseDir string       // directory containing the game file (for include() resolution)
	dataDir string       // root data directory (for resolving charmaps, etc.)
	clock   domain.Clock // server clock exposed to JS as now()

	commands    []domain.Command
	cachedTeams []map[string]any   // snapshot set by server; read by JS teams()
	logFn       func(string)
	chatCh      chan domain.Message // drained by server goroutine; closed on unload

	// game object methods (nil if not defined)
	updateFn        goja.Callable
	onPlayerLeave   goja.Callable
	onInput         goja.Callable
	renderFn         goja.Callable
	renderCanvasFn   goja.Callable
	renderSplashFn   goja.Callable
	renderGameOverFn goja.Callable
	layoutFn         goja.Callable
	statusBarFn     goja.Callable
	commandBarFn    goja.Callable

	// lifecycle
	gameNameProp  string
	teamRangeProp domain.TeamRange
	initFn           goja.Callable
	startFn          goja.Callable

	// suspend/resume
	canSuspendProp bool
	suspendFn      goja.Callable
	resumeFn       goja.Callable

	// gameOver() callback state — set by JS, detected by tick loop
	gameOverPending bool
	gameOverResults []domain.GameResult // results passed to gameOver()
	gameOverState   goja.Value          // state argument passed as second arg to gameOver()

	menus        []domain.MenuDef
	showDialogFn func(playerID string, d domain.DialogRequest) // injected by server

	charmapDef *render.CharMapDef // loaded from dist/charmaps/<name>/charmap.json; nil if no charmap
}

// LoadGame loads and executes a JS file from games/, extracts the Game object
// methods, and returns a domain.Game. Init() is NOT called here — the server
// calls it at the splash→playing transition when GamePlayerIDs are set.
func LoadGame(path string, logFn func(string), chatCh chan domain.Message, clock domain.Clock, dataDir string) (domain.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	rt := &JSRuntime{
		vm:      goja.New(),
		baseDir: filepath.Dir(path),
		logFn:   logFn,
		chatCh:  chatCh,
		clock:   clock,
		dataDir: dataDir,
	}

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

func (r *JSRuntime) Init(savedState any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Init")
	defer TraceJS(r.vm, "Init")()
	cancel := WatchdogJS(r.vm, "Init")
	defer cancel()
	_, _ = r.initFn(goja.Undefined(), r.vm.ToValue(savedState))

	// Re-read renderSplash — init() may have defined it dynamically.
	gameVal := r.vm.Get("Game")
	if gameVal != nil && !goja.IsUndefined(gameVal) && !goja.IsNull(gameVal) {
		if obj := gameVal.ToObject(r.vm); obj != nil {
			if r.renderSplashFn == nil {
				r.renderSplashFn = extractCallable(obj, "renderSplash")
			}
			if r.renderGameOverFn == nil {
				r.renderGameOverFn = extractCallable(obj, "renderGameOver")
			}
		}
	}
}

func (r *JSRuntime) Start() {
	if r.startFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Start")
	defer TraceJS(r.vm, "Start")()
	cancel := WatchdogJS(r.vm, "Start")
	defer cancel()
	_, _ = r.startFn(goja.Undefined())
}

func (r *JSRuntime) extractGameObject() error {
	gameVal := r.vm.Get("Game")
	if gameVal == nil || goja.IsUndefined(gameVal) || goja.IsNull(gameVal) {
		return fmt.Errorf("script must define a global 'Game' object")
	}

	gameObj := gameVal.ToObject(r.vm)
	if gameObj == nil {
		return fmt.Errorf("'Game' is not an object")
	}

	// Core game methods
	r.updateFn = extractCallable(gameObj, "update")
	r.onPlayerLeave = extractCallable(gameObj, "onPlayerLeave")
	r.onInput = extractCallable(gameObj, "onInput")
	r.renderFn = extractCallable(gameObj, "render")
	r.renderCanvasFn = extractCallable(gameObj, "renderCanvas")
	r.renderSplashFn = extractCallable(gameObj, "renderSplash")
	r.renderGameOverFn = extractCallable(gameObj, "renderGameOver")
	r.layoutFn = extractCallable(gameObj, "layout")
	if r.layoutFn == nil {
		r.layoutFn = extractCallable(gameObj, "renderNC") // backwards compat
	}
	r.statusBarFn = extractCallable(gameObj, "statusBar")
	r.commandBarFn = extractCallable(gameObj, "commandBar")

	// init and start are mandatory
	r.initFn = extractCallable(gameObj, "init")
	if r.initFn == nil {
		return fmt.Errorf("Game must define an init(savedState) function")
	}
	r.startFn = extractCallable(gameObj, "start")

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

	// Suspend/resume support (all optional)
	if v := gameObj.Get("canSuspend"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		r.canSuspendProp = v.ToBoolean()
	}
	r.suspendFn = extractCallable(gameObj, "suspend")
	r.resumeFn = extractCallable(gameObj, "resume")

	// Read charmap property (string name, e.g. "pacman").
	// Loads dist/charmaps/<name>/charmap.json if present.
	if v := gameObj.Get("charmap"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		name := v.String()
		if name != "" && r.dataDir != "" {
			jsonPath := filepath.Join(r.dataDir, "charmaps", name, "charmap.json")
			def, err := render.LoadCharMapDef(jsonPath)
			if err != nil {
				slog.Warn("failed to load charmap", "name", name, "error", err)
			} else {
				r.charmapDef = def
				slog.Info("loaded charmap", "name", name, "entries", len(def.Entries))
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

func (r *JSRuntime) OnPlayerLeave(playerID string) {
	if r.onPlayerLeave == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerLeave")
	defer TraceJS(r.vm, "OnPlayerLeave")()
	cancel := WatchdogJS(r.vm, "OnPlayerLeave")
	defer cancel()
	_, _ = r.onPlayerLeave(goja.Undefined(), r.vm.ToValue(playerID))
}

func (r *JSRuntime) OnInput(playerID, key string) {
	if r.onInput == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnInput")
	defer TraceJS(r.vm, "OnInput")()
	cancel := WatchdogJS(r.vm, "OnInput")
	defer cancel()
	_, _ = r.onInput(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(key))
}

func (r *JSRuntime) Update(dt float64) {
	if r.updateFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Update")
	defer TraceJS(r.vm, "Update")()
	cancel := WatchdogJS(r.vm, "Update")
	defer cancel()
	_, _ = r.updateFn(goja.Undefined(), r.vm.ToValue(dt))
}

func (r *JSRuntime) Render(buf *render.ImageBuffer, playerID string, x, y, width, height int) {
	if r.renderFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Render")
	defer TraceJS(r.vm, "Render")()
	cancel := WatchdogJS(r.vm, "Render")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	_, err := r.renderFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS Render error", "error", err)
	}
}

func (r *JSRuntime) Layout(playerID string, width, height int) *domain.WidgetNode {
	if r.layoutFn == nil {
		return nil // framework will fall back to wrapping Render() in a gameview node
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Layout")
	defer TraceJS(r.vm, "Layout")()
	cancel := WatchdogJS(r.vm, "Layout")
	defer cancel()
	val, err := r.layoutFn(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS Layout error", "error", err)
		return nil
	}
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	return gojaToWidgetNode(r.vm, val)
}

func (r *JSRuntime) StatusBar(playerID string) string {
	if r.statusBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("StatusBar")
	defer TraceJS(r.vm, "StatusBar")()
	cancel := WatchdogJS(r.vm, "StatusBar")
	defer cancel()
	val, err := r.statusBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS StatusBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *JSRuntime) CommandBar(playerID string) string {
	if r.commandBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("CommandBar")
	defer TraceJS(r.vm, "CommandBar")()
	cancel := WatchdogJS(r.vm, "CommandBar")
	defer cancel()
	val, err := r.commandBarFn(goja.Undefined(), r.vm.ToValue(playerID))
	if err != nil {
		slog.Error("JS CommandBar error", "error", err)
		return ""
	}
	return val.String()
}

func (r *JSRuntime) Commands() []domain.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.Command, len(r.commands))
	copy(result, r.commands)
	return result
}

func (r *JSRuntime) Unload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vm.Interrupt("game unloaded")
}

func (r *JSRuntime) Menus() []domain.MenuDef {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.menus
}

// SetTeamsCache replaces the cached teams snapshot that JS teams() returns.
// Called by the server after loading teams or when a player reconnects.
func (r *JSRuntime) SetTeamsCache(teams []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedTeams = teams
}

// --- Lifecycle methods (part of Game interface) ---

func (r *JSRuntime) GameName() string {
	return r.gameNameProp
}

func (r *JSRuntime) TeamRange() domain.TeamRange {
	return r.teamRangeProp
}

func (r *JSRuntime) CharMap() *render.CharMapDef {
	return r.charmapDef
}

func (r *JSRuntime) HasCanvasMode() bool {
	return r.renderCanvasFn != nil
}

func (r *JSRuntime) RenderCanvas(playerID string, width, height int) []byte {
	if r.renderCanvasFn == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderCanvas")
	defer TraceJS(r.vm, "RenderCanvas")()
	cancel := WatchdogJS(r.vm, "RenderCanvas")
	defer cancel()

	canvas := newJSCanvas(width, height)
	ctx := canvas.toJSObject(r.vm)
	_, err := r.renderCanvasFn(goja.Undefined(), r.vm.ToValue(ctx), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS RenderCanvas error", "error", err)
		return nil
	}

	data, err := canvas.toPNG()
	if err != nil {
		slog.Error("Canvas PNG encoding error", "error", err)
		return nil
	}
	return data
}

func (r *JSRuntime) RenderSplash(buf *render.ImageBuffer, playerID string, x, y, width, height int) bool {
	if r.renderSplashFn == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderSplash")
	defer TraceJS(r.vm, "RenderSplash")()
	cancel := WatchdogJS(r.vm, "RenderSplash")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	_, err := r.renderSplashFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS RenderSplash error", "error", err)
		return false
	}
	return true
}

func (r *JSRuntime) RenderGameOver(buf *render.ImageBuffer, playerID string, x, y, width, height int, results []domain.GameResult) bool {
	if r.renderGameOverFn == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderGameOver")
	defer TraceJS(r.vm, "RenderGameOver")()
	cancel := WatchdogJS(r.vm, "RenderGameOver")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	// Convert results to JS-friendly array of {name, result} objects.
	jsResults := make([]map[string]any, len(results))
	for i, r := range results {
		jsResults[i] = map[string]any{"name": r.Name, "result": r.Result}
	}
	_, err := r.renderGameOverFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height), r.vm.ToValue(jsResults))
	if err != nil {
		slog.Error("JS RenderGameOver error", "error", err)
		return false
	}
	return true
}

// IsGameOverPending returns true if JS called gameOver() and clears the flag.
func (r *JSRuntime) IsGameOverPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.gameOverPending {
		return false
	}
	r.gameOverPending = false
	return true
}

// GameOverResults returns the results array passed to gameOver().
func (r *JSRuntime) GameOverResults() []domain.GameResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameOverResults
}

// GameOverStateExport returns the state object passed as the second arg to gameOver().
func (r *JSRuntime) GameOverStateExport() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gameOverState == nil || goja.IsUndefined(r.gameOverState) || goja.IsNull(r.gameOverState) {
		return nil
	}
	return r.gameOverState.Export()
}

// --- Suspend/resume ---

func (r *JSRuntime) CanSuspend() bool {
	return r.canSuspendProp
}

func (r *JSRuntime) Suspend() any {
	if r.suspendFn == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Suspend")
	defer TraceJS(r.vm, "Suspend")()
	cancel := WatchdogJS(r.vm, "Suspend")
	defer cancel()
	val, err := r.suspendFn(goja.Undefined())
	if err != nil {
		slog.Error("JS Suspend error", "error", err)
		return nil
	}
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	return val.Export()
}

func (r *JSRuntime) Resume(sessionState any) {
	if r.resumeFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Resume")
	defer TraceJS(r.vm, "Resume")()
	cancel := WatchdogJS(r.vm, "Resume")
	defer cancel()
	_, _ = r.resumeFn(goja.Undefined(), r.vm.ToValue(sessionState))
}

// ChatCh returns the channel used by JS chat()/chatPlayer() to send messages.
func (r *JSRuntime) ChatCh() chan domain.Message {
	return r.chatCh
}

// CloseChatCh closes the chat channel. Called during game unload.
func (r *JSRuntime) CloseChatCh() {
	if r.chatCh != nil {
		close(r.chatCh)
	}
}

// SetShowDialogFn sets the callback used by JS messageBox() to show dialogs.
func (r *JSRuntime) SetShowDialogFn(fn func(playerID string, d domain.DialogRequest)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.showDialogFn = fn
}

func (r *JSRuntime) recoverJS(method string) {
	if rec := recover(); rec != nil {
		slog.Error("JS panic in game method", "method", method, "panic", rec)
	}
}
