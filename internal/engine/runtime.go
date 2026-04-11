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
	mu      sync.Mutex
	vm      *goja.Runtime
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
	updateFn          goja.Callable
	onPlayerLeave     goja.Callable
	onInput           goja.Callable
	renderAsciiFn     goja.Callable
	renderCanvasFn    goja.Callable
	renderStartingFn  goja.Callable
	renderEndingFn    goja.Callable
	layoutFn          goja.Callable
	statusBarFn       goja.Callable
	commandBarFn      goja.Callable

	// lifecycle
	gameNameProp  string
	teamRangeProp domain.TeamRange
	loadFn        goja.Callable
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
}

// LoadGame loads and executes a game script (.js), extracts the Game
// object, and returns a domain.Game. Load() is NOT called here — the server
// calls it after teams are set up, before PhaseStarting.
func LoadGame(path string, logFn func(string), chatCh chan domain.Message, clock domain.Clock, dataDir string) (domain.Game, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game file: %w", err)
	}

	rt := &Runtime{
		vm:           goja.New(),
		baseDir:      filepath.Dir(path),
		logFn:        logFn,
		chatCh:       chatCh,
		clock:        clock,
		dataDir:      dataDir,
		isFolderGame: filepath.Base(path) == "main.js",
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

func (r *Runtime) Load(savedState any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Load")
	defer traceCall(r.vm, "Load")()
	cancel := Watchdog(r.vm, "Load")
	defer cancel()
	_, _ = r.loadFn(goja.Undefined(), r.vm.ToValue(savedState))

	// Re-read renderGameStart/renderGameEnd — load() may have defined them dynamically.
	gameVal := r.vm.Get("Game")
	if gameVal != nil && !goja.IsUndefined(gameVal) && !goja.IsNull(gameVal) {
		if obj := gameVal.ToObject(r.vm); obj != nil {
			if r.renderStartingFn == nil {
				r.renderStartingFn = extractCallable(obj, "renderGameStart")
			}
			if r.renderEndingFn == nil {
				r.renderEndingFn = extractCallable(obj, "renderGameEnd")
			}
		}
	}
}

func (r *Runtime) Begin() {
	if r.beginFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Begin")
	defer traceCall(r.vm, "Begin")()
	cancel := Watchdog(r.vm, "Begin")
	defer cancel()
	_, _ = r.beginFn(goja.Undefined())
}

func (r *Runtime) End() {
	if r.endFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("End")
	defer traceCall(r.vm, "End")()
	cancel := Watchdog(r.vm, "End")
	defer cancel()
	_, _ = r.endFn(goja.Undefined())
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

	// Core game methods
	r.updateFn = extractCallable(gameObj, "update")
	r.onPlayerLeave = extractCallable(gameObj, "onPlayerLeave")
	r.onInput = extractCallable(gameObj, "onInput")
	r.renderAsciiFn = extractCallable(gameObj, "renderAscii")
	r.renderCanvasFn = extractCallable(gameObj, "renderCanvas")
	r.renderStartingFn = extractCallable(gameObj, "renderGameStart")
	r.renderEndingFn = extractCallable(gameObj, "renderGameEnd")
	r.layoutFn = extractCallable(gameObj, "layout")
	r.statusBarFn = extractCallable(gameObj, "statusBar")
	r.commandBarFn = extractCallable(gameObj, "commandBar")

	// load is mandatory; begin, end, unload are optional
	r.loadFn = extractCallable(gameObj, "load")
	if r.loadFn == nil {
		return fmt.Errorf("Game must define a load(savedState) function")
	}
	r.beginFn = extractCallable(gameObj, "begin")
	r.endFn = extractCallable(gameObj, "end")
	r.unloadFn = extractCallable(gameObj, "unload")
	r.suspendFn = extractCallable(gameObj, "suspend")
	r.resumeFn = extractCallable(gameObj, "resume")

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

func (r *Runtime) OnPlayerLeave(playerID string) {
	if r.onPlayerLeave == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnPlayerLeave")
	defer traceCall(r.vm, "OnPlayerLeave")()
	cancel := Watchdog(r.vm, "OnPlayerLeave")
	defer cancel()
	_, _ = r.onPlayerLeave(goja.Undefined(), r.vm.ToValue(playerID))
}

func (r *Runtime) OnInput(playerID, key string) {
	if r.onInput == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("OnInput")
	defer traceCall(r.vm, "OnInput")()
	cancel := Watchdog(r.vm, "OnInput")
	defer cancel()
	_, _ = r.onInput(goja.Undefined(), r.vm.ToValue(playerID), r.vm.ToValue(key))
}

func (r *Runtime) Update(dt float64) {
	if r.updateFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Update")
	defer traceCall(r.vm, "Update")()
	cancel := Watchdog(r.vm, "Update")
	defer cancel()
	_, _ = r.updateFn(goja.Undefined(), r.vm.ToValue(dt))
}

func (r *Runtime) RenderAscii(buf *render.ImageBuffer, playerID string, x, y, width, height int) {
	if r.renderAsciiFn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderAscii")
	defer traceCall(r.vm, "RenderAscii")()
	cancel := Watchdog(r.vm, "RenderAscii")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	_, err := r.renderAsciiFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS RenderAscii error", "error", err)
	}
}

func (r *Runtime) Layout(playerID string, width, height int) *domain.WidgetNode {
	if r.layoutFn == nil {
		return nil // framework will fall back to wrapping RenderAscii() in a gameview node
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("Layout")
	defer traceCall(r.vm, "Layout")()
	cancel := Watchdog(r.vm, "Layout")
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

func (r *Runtime) StatusBar(playerID string) string {
	if r.statusBarFn == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("StatusBar")
	defer traceCall(r.vm, "StatusBar")()
	cancel := Watchdog(r.vm, "StatusBar")
	defer cancel()
	val, err := r.statusBarFn(goja.Undefined(), r.vm.ToValue(playerID))
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
	cancel := Watchdog(r.vm, "CommandBar")
	defer cancel()
	val, err := r.commandBarFn(goja.Undefined(), r.vm.ToValue(playerID))
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
			cancel := Watchdog(r.vm, "Unload")
			defer cancel()
			val, err := r.unloadFn(goja.Undefined())
			if err == nil && val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
				result = val.Export()
			}
		}()
	}

	r.vm.Interrupt("game unloaded")
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
		cancel := Watchdog(r.vm, "Suspend")
		defer cancel()
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
		defer r.recoverJS("Resume")
		defer traceCall(r.vm, "Resume")()
		cancel := Watchdog(r.vm, "Resume")
		defer cancel()
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
	cancel := Watchdog(r.vm, "RenderCanvas")
	defer cancel()

	canvas := NewJSCanvas(width, height, 1.0)
	ctx := canvas.ToJSObject(r.vm)
	_, err := r.renderCanvasFn(goja.Undefined(), r.vm.ToValue(ctx), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(canvas.height))
	if err != nil {
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
	cancel := Watchdog(r.vm, "RenderCanvasImage")
	defer cancel()

	canvas := NewJSCanvas(width, height, 1.0)
	ctx := canvas.ToJSObject(r.vm)
	_, err := r.renderCanvasFn(goja.Undefined(), r.vm.ToValue(ctx), r.vm.ToValue(playerID), r.vm.ToValue(width), r.vm.ToValue(canvas.height))
	if err != nil {
		slog.Error("JS RenderCanvasImage error", "error", err)
		return nil
	}
	return canvas.ToRGBA()
}

func (r *Runtime) RenderStarting(buf *render.ImageBuffer, playerID string, x, y, width, height int) bool {
	if r.renderStartingFn == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderStarting")
	defer traceCall(r.vm, "RenderStarting")()
	cancel := Watchdog(r.vm, "RenderStarting")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	_, err := r.renderStartingFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height))
	if err != nil {
		slog.Error("JS RenderStarting error", "error", err)
		return false
	}
	return true
}

func (r *Runtime) RenderEnding(buf *render.ImageBuffer, playerID string, x, y, width, height int, results []domain.GameResult) bool {
	if r.renderEndingFn == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.recoverJS("RenderEnding")
	defer traceCall(r.vm, "RenderEnding")()
	cancel := Watchdog(r.vm, "RenderEnding")
	defer cancel()
	jsBuf := r.newJSImageBuffer(buf, x, y, width, height)
	// Convert results to JS-friendly array of {name, result} objects.
	jsResults := make([]map[string]any, len(results))
	for i, r := range results {
		jsResults[i] = map[string]any{"name": r.Name, "result": r.Result}
	}
	_, err := r.renderEndingFn(goja.Undefined(), r.vm.ToValue(jsBuf), r.vm.ToValue(playerID), r.vm.ToValue(x), r.vm.ToValue(y), r.vm.ToValue(width), r.vm.ToValue(height), r.vm.ToValue(jsResults))
	if err != nil {
		slog.Error("JS RenderEnding error", "error", err)
		return false
	}
	return true
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

// State returns the current value of the JS Game.state property.
// Used by the framework for OSC push to local renderers. Returns nil if
// Game.state is not set.
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
	return v.Export()
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
