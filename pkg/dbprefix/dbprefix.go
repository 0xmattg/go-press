package dbprefix

import (
	"fmt"
	"sync"
)

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
// Convention: {prefix}plgn_{storageSlug}_{tableName}, where storageSlug is the
// plugin slug with "-" replaced by "_" (plugin.StorageSlug).
// Example: PluginTable("commerce_usdt", "invoices") → "gp_plgn_commerce_usdt_invoices"
//
// Both arguments must match [a-z][a-z0-9_]*; anything else panics. A bad name
// is a programming error, and the plugin's own tests surface it.
func PluginTable(storageSlug, tableName string) string {
	mustIdent("plugin storage slug", storageSlug)
	mustIdent("table name", tableName)
	mu.RLock()
	defer mu.RUnlock()
	return prefix + PluginInfix + storageSlug + "_" + tableName
}

// ThemeTable returns the prefixed table name for a theme table.
// Convention: {prefix}thm_{storageSlug}_{tableName}, with the same storage
// slug rule and argument checks as PluginTable.
// Example: ThemeTable("financial_news", "tickers") → "gp_thm_financial_news_tickers"
func ThemeTable(storageSlug, tableName string) string {
	mustIdent("theme storage slug", storageSlug)
	mustIdent("table name", tableName)
	mu.RLock()
	defer mu.RUnlock()
	return prefix + ThemeInfix + storageSlug + "_" + tableName
}

func mustIdent(kind, s string) {
	if !validIdent(s) {
		panic(fmt.Sprintf("dbprefix: invalid %s %q: want [a-z][a-z0-9_]*", kind, s))
	}
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_'):
		default:
			return false
		}
	}
	return true
}
