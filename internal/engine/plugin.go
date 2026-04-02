package engine

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dop251/goja"

	"null-space/internal/domain"
	"null-space/internal/network"
)

// JSPlugin wraps a goja JS runtime for a per-player or per-console plugin.
// The plugin exports a Plugin object with an onMessage(author, text, isSystem) hook.
// If onMessage returns a non-empty string, it is treated as if the owner typed it.
type JSPlugin struct {
	mu          sync.Mutex
	vm          *goja.Runtime
	name        string
	onMessageFn goja.Callable // Plugin.onMessage(author, text, isSystem) → string|null
}

// LoadPlugin reads and executes a JS plugin file, extracting the Plugin.onMessage hook.
func LoadPlugin(path string, clock domain.Clock) (*JSPlugin, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin file: %w", err)
	}

	p := &JSPlugin{
		vm:   goja.New(),
		name: strings.TrimSuffix(filepath.Base(path), ".js"),
	}

	// Register a minimal log() global so plugins can debug-print.
	p.vm.Set("log", func(msg string) {
		slog.Info("plugin log", "plugin", p.name, "msg", msg)
	})

	// now() — returns server time in epoch milliseconds (same clock as games).
	p.vm.Set("now", func() int64 {
		return clock.Now().UnixMilli()
	})

	if _, err := p.vm.RunScript(path, string(src)); err != nil {
		return nil, fmt.Errorf("execute plugin script: %w", err)
	}

	pluginVal := p.vm.Get("Plugin")
	if pluginVal == nil || goja.IsUndefined(pluginVal) || goja.IsNull(pluginVal) {
		return nil, fmt.Errorf("plugin script must export a global Plugin object")
	}
	obj := pluginVal.ToObject(p.vm)
	if obj == nil {
		return nil, fmt.Errorf("Plugin is not an object")
	}

	if fn, ok := goja.AssertFunction(obj.Get("onMessage")); ok {
		p.onMessageFn = fn
	} else {
		return nil, fmt.Errorf("Plugin.onMessage is required and must be a function")
	}

	return p, nil
}

// OnMessage calls the JS onMessage hook with the given message fields.
// Returns a non-empty string if the plugin wants to inject input, empty string otherwise.
func (p *JSPlugin) OnMessage(author, text string, isSystem bool) string {
	if p.onMessageFn == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// Recover from JS panics.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("plugin panic", "plugin", p.name, "panic", r)
		}
	}()

	cancel := WatchdogJS(p.vm, "Plugin.onMessage")
	defer cancel()

	val, err := p.onMessageFn(goja.Undefined(),
		p.vm.ToValue(author),
		p.vm.ToValue(text),
		p.vm.ToValue(isSystem),
	)
	if err != nil {
		slog.Error("plugin onMessage error", "plugin", p.name, "error", err)
		return ""
	}

	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return ""
	}
	if s := val.String(); s != "" {
		return s
	}
	return ""
}

// Name returns the plugin's display name (filename stem).
func (p *JSPlugin) Name() string { return p.name }

// Unload interrupts the JS runtime.
func (p *JSPlugin) Unload() {
	p.vm.Interrupt("unloaded")
}

// ResolvePluginPath resolves a plugin name or URL to a local file path,
// downloading and caching if it's a URL.
func ResolvePluginPath(nameOrURL, dataDir string) (name, path string, err error) {
	if network.IsURL(nameOrURL) {
		cacheDir := filepath.Join(dataDir, "plugins", ".cache")
		local, err := network.DownloadToCache(nameOrURL, cacheDir)
		if err != nil {
			return "", "", fmt.Errorf("download plugin: %w", err)
		}
		return strings.TrimSuffix(filepath.Base(local), ".js"), local, nil
	}
	return nameOrURL, filepath.Join(dataDir, "plugins", nameOrURL+".js"), nil
}
