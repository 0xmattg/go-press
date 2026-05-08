package core

import "sync"

// PluginFactory is a constructor function for creating and loading a plugin.
// It receives the Engine so it can call engine.LoadPlugin().
type PluginFactory func(engine *Engine)

var (
	pluginRegistryMu sync.RWMutex
	pluginRegistry   = make(map[string]PluginFactory)
)

// RegisterPlugin registers a plugin factory under the given name.
// Plugins call this from their init() function for auto-registration.
func RegisterPlugin(name string, factory PluginFactory) {
	pluginRegistryMu.Lock()
	defer pluginRegistryMu.Unlock()
	pluginRegistry[name] = factory
}

// AllPluginFactories returns a copy of all registered plugin factories.
func AllPluginFactories() map[string]PluginFactory {
	pluginRegistryMu.RLock()
	defer pluginRegistryMu.RUnlock()
	out := make(map[string]PluginFactory, len(pluginRegistry))
	for k, v := range pluginRegistry {
		out[k] = v
	}
	return out
}
