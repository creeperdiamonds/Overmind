package plugin

import (
	"log/slog"
	"sync"
)

var (
	mu       sync.RWMutex
	registry []Plugin
	rootLog  = slog.Default()
)

// SetLogger sets the root logger used to scope per-plugin loggers.
// Call this before EnableAll if you want a custom logger.
func SetLogger(l *slog.Logger) {
	mu.Lock()
	rootLog = l
	mu.Unlock()
}

// Register adds a plugin to the global registry.
// Call this from your plugin package's init() function.
func Register(p Plugin) {
	mu.Lock()
	registry = append(registry, p)
	mu.Unlock()
}

// LoadAll calls OnLoad on every registered plugin and assigns loggers.
func LoadAll() {
	mu.RLock()
	plugins := append([]Plugin(nil), registry...)
	log := rootLog
	mu.RUnlock()

	for _, p := range plugins {
		p.setLogger(log.With("plugin", p.Name()))
		p.OnLoad()
		log.Info("Plugin loaded", "name", p.Name(), "version", p.Version())
	}
}

// EnableAll calls OnEnable on every registered plugin in registration order.
func EnableAll() {
	mu.RLock()
	plugins := append([]Plugin(nil), registry...)
	log := rootLog
	mu.RUnlock()

	for _, p := range plugins {
		p.OnEnable()
		log.Info("Plugin enabled", "name", p.Name())
	}
}

// DisableAll calls OnDisable on every registered plugin in reverse order.
func DisableAll() {
	mu.RLock()
	plugins := append([]Plugin(nil), registry...)
	log := rootLog
	mu.RUnlock()

	for i := len(plugins) - 1; i >= 0; i-- {
		plugins[i].OnDisable()
		log.Info("Plugin disabled", "name", plugins[i].Name())
	}
}

// Get returns the plugin with the given name, or nil if not found.
func Get(name string) Plugin {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range registry {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// All returns a snapshot of all registered plugins.
func All() []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	return append([]Plugin(nil), registry...)
}
