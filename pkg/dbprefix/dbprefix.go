package dbprefix

import "sync"

// DefaultPrefix is the default table prefix for GoPress core tables.
const DefaultPrefix = "gp_"

// Infixes to distinguish plugin / theme tables from core tables.
const (
	PluginInfix = "plgn_"
	ThemeInfix  = "thm_"
)

var (
	mu     sync.RWMutex
	prefix = DefaultPrefix
)

// Set sets the global table prefix. Must be called before any DB operation.
func Set(p string) {
	mu.Lock()
	defer mu.Unlock()
	prefix = p
}

// Get returns the current table prefix.
func Get() string {
	mu.RLock()
	defer mu.RUnlock()
	return prefix
}

// Table returns the prefixed table name for a GoPress core table.
// Example: Table("contents") → "gp_contents"
func Table(name string) string {
	mu.RLock()
	defer mu.RUnlock()
	return prefix + name
}

// PluginTable returns the prefixed table name for a plugin table.
// Convention: {prefix}plgn_{pluginSlug}_{tableName}
// Example: PluginTable("multilang", "translations") → "gp_plgn_multilang_translations"
func PluginTable(pluginSlug, tableName string) string {
	mu.RLock()
	defer mu.RUnlock()
	return prefix + PluginInfix + pluginSlug + "_" + tableName
}

// ThemeTable returns the prefixed table name for a theme table.
// Convention: {prefix}thm_{themeSlug}_{tableName}
// Example: ThemeTable("financial-news", "tickers") → "gp_thm_financial-news_tickers"
func ThemeTable(themeSlug, tableName string) string {
	mu.RLock()
	defer mu.RUnlock()
	return prefix + ThemeInfix + themeSlug + "_" + tableName
}
