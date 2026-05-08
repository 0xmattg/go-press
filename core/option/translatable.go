package option

import "sync"

// TranslatableKey describes a theme setting that can be translated.
//
// Themes register human-facing option keys, such as headings or button labels,
// so multilingual plugins can provide admin UI for per-language values without
// hard-coding knowledge of each theme's settings schema.
type TranslatableKey struct {
	Key     string // option key, e.g. "home_about_title"
	Section string // UI grouping, e.g. "hero", "about", "cta"
	Label   string // human-readable label, e.g. "Title"
}

var (
	trMu       sync.RWMutex
	trRegistry []TranslatableKey
	trSet      = make(map[string]bool)
)

// RegisterTranslatable registers an option key as translatable.
//
// Themes call this during Setup to declare text-like settings. Duplicate keys
// are ignored so repeated setup during theme switching does not create multiple
// rows in the in-memory registry.
func RegisterTranslatable(key, section, label string) {
	trMu.Lock()
	defer trMu.Unlock()
	if trSet[key] {
		return
	}
	trSet[key] = true
	trRegistry = append(trRegistry, TranslatableKey{Key: key, Section: section, Label: label})
}

// AllTranslatableOptions returns all registered translatable option keys.
func AllTranslatableOptions() []TranslatableKey {
	trMu.RLock()
	defer trMu.RUnlock()
	out := make([]TranslatableKey, len(trRegistry))
	copy(out, trRegistry)
	return out
}

// IsTranslatable checks if an option key is registered as translatable.
func IsTranslatable(key string) bool {
	trMu.RLock()
	defer trMu.RUnlock()
	return trSet[key]
}

// ClearTranslatableOptions resets the in-memory registry.
//
// Engine should call this when switching themes so settings from the previous
// theme do not appear in the active theme's translation UI.
func ClearTranslatableOptions() {
	trMu.Lock()
	defer trMu.Unlock()
	trRegistry = nil
	trSet = make(map[string]bool)
}

// AllTranslatableKeys returns just the key strings of all registered translatable options.
// Useful for passing to i18n.TranslateSettings without exposing the full struct.
func AllTranslatableKeys() []string {
	trMu.RLock()
	defer trMu.RUnlock()
	keys := make([]string, len(trRegistry))
	for i, tk := range trRegistry {
		keys[i] = tk.Key
	}
	return keys
}
