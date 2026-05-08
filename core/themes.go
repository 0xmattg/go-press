package core

import (
	"fmt"
	"sync"

	coreTheme "go-press/core/theme"
)

// ThemeFactory is a constructor function for creating theme instances.
// It receives the Engine and the theme directory path.
type ThemeFactory func(engine *Engine, themeDir string) coreTheme.Theme

// themeRegistry holds registered theme factories, keyed by theme slug.
var (
	themeRegistryMu sync.RWMutex
	themeRegistry   = make(map[string]ThemeFactory)
)

// RegisterTheme registers a theme factory under the given name.
// Themes call this from their init() function.
func RegisterTheme(name string, factory ThemeFactory) {
	themeRegistryMu.Lock()
	defer themeRegistryMu.Unlock()
	themeRegistry[name] = factory
}

// GetThemeFactory returns the registered factory for a theme name.
func GetThemeFactory(name string) (ThemeFactory, error) {
	themeRegistryMu.RLock()
	defer themeRegistryMu.RUnlock()
	f, ok := themeRegistry[name]
	if !ok {
		available := make([]string, 0, len(themeRegistry))
		for k := range themeRegistry {
			available = append(available, k)
		}
		return nil, fmt.Errorf("theme %q not registered; available: %v", name, available)
	}
	return f, nil
}

// AllThemeFactories returns a copy of all registered theme factories.
func AllThemeFactories() map[string]ThemeFactory {
	themeRegistryMu.RLock()
	defer themeRegistryMu.RUnlock()
	result := make(map[string]ThemeFactory, len(themeRegistry))
	for k, v := range themeRegistry {
		result[k] = v
	}
	return result
}
