package core

import (
	"fmt"
	"sync"

	"go-press/pkg/dbprefix"
)

// TableOwner identifies the subsystem responsible for a registered table.
//
// The ownership metadata is informational today, but it is useful for installer
// output, diagnostics, plugin cleanup, and future migration tooling.
type TableOwner string

const (
	// OwnerCore marks framework-owned tables that ship with GoPress.
	OwnerCore TableOwner = "core"
	// OwnerPlugin marks tables created by a plugin.
	OwnerPlugin TableOwner = "plugin"
	// OwnerTheme marks tables created by a theme.
	OwnerTheme TableOwner = "theme"
)

// TableEntry records metadata about a database table known to the runtime.
//
// BaseName is the logical name without any site prefix. FullName is the actual
// table name after applying dbprefix rules, including plugin/theme namespaces.
type TableEntry struct {
	Owner     TableOwner // core / plugin / theme
	OwnerSlug string     // e.g. "multilang", "financial-news" (empty for core)
	BaseName  string     // unprefixed logical name, e.g. "translations"
	FullName  string     // final DB table name, e.g. "gp_plgn_multilang_translations"
}

var (
	tableRegistryMu sync.RWMutex
	tableRegistry   []TableEntry
)

// RegisterCoreTable registers a framework-owned table.
func RegisterCoreTable(baseName string) {
	tableRegistryMu.Lock()
	defer tableRegistryMu.Unlock()
	tableRegistry = append(tableRegistry, TableEntry{
		Owner:    OwnerCore,
		BaseName: baseName,
		FullName: dbprefix.Table(baseName),
	})
}

// RegisterPluginTable registers a plugin-owned table.
//
// Plugins should call this during Activate after they have migrated or verified
// their schema. The registry does not create tables; it records ownership and
// the dbprefix-resolved name for diagnostics and tooling.
func RegisterPluginTable(pluginSlug, baseName string) {
	tableRegistryMu.Lock()
	defer tableRegistryMu.Unlock()
	tableRegistry = append(tableRegistry, TableEntry{
		Owner:     OwnerPlugin,
		OwnerSlug: pluginSlug,
		BaseName:  baseName,
		FullName:  dbprefix.PluginTable(pluginSlug, baseName),
	})
}

// RegisterThemeTable registers a theme-owned table.
//
// Most themes should use the shared Content model instead of custom tables.
// This hook exists for advanced themes that need private relational data while
// still making that ownership visible to the engine.
func RegisterThemeTable(themeSlug, baseName string) {
	tableRegistryMu.Lock()
	defer tableRegistryMu.Unlock()
	tableRegistry = append(tableRegistry, TableEntry{
		Owner:     OwnerTheme,
		OwnerSlug: themeSlug,
		BaseName:  baseName,
		FullName:  dbprefix.ThemeTable(themeSlug, baseName),
	})
}

// AllTables returns a copy of all registered table entries.
func AllTables() []TableEntry {
	tableRegistryMu.RLock()
	defer tableRegistryMu.RUnlock()
	out := make([]TableEntry, len(tableRegistry))
	copy(out, tableRegistry)
	return out
}

// TablesByOwner returns registered tables filtered by owner type.
func TablesByOwner(owner TableOwner) []TableEntry {
	tableRegistryMu.RLock()
	defer tableRegistryMu.RUnlock()
	var out []TableEntry
	for _, e := range tableRegistry {
		if e.Owner == owner {
			out = append(out, e)
		}
	}
	return out
}

// TablesForPlugin returns tables registered by a specific plugin.
func TablesForPlugin(pluginSlug string) []TableEntry {
	tableRegistryMu.RLock()
	defer tableRegistryMu.RUnlock()
	var out []TableEntry
	for _, e := range tableRegistry {
		if e.Owner == OwnerPlugin && e.OwnerSlug == pluginSlug {
			out = append(out, e)
		}
	}
	return out
}

// TablesForTheme returns tables registered by a specific theme.
func TablesForTheme(themeSlug string) []TableEntry {
	tableRegistryMu.RLock()
	defer tableRegistryMu.RUnlock()
	var out []TableEntry
	for _, e := range tableRegistry {
		if e.Owner == OwnerTheme && e.OwnerSlug == themeSlug {
			out = append(out, e)
		}
	}
	return out
}

// ClearTableRegistry removes all registered table entries.
func ClearTableRegistry() {
	tableRegistryMu.Lock()
	defer tableRegistryMu.Unlock()
	tableRegistry = nil
}

// TableRegistrySummary returns a human-readable summary of all registered tables.
func TableRegistrySummary() string {
	tableRegistryMu.RLock()
	defer tableRegistryMu.RUnlock()
	core, plugins, themes := 0, 0, 0
	for _, e := range tableRegistry {
		switch e.Owner {
		case OwnerCore:
			core++
		case OwnerPlugin:
			plugins++
		case OwnerTheme:
			themes++
		}
	}
	return fmt.Sprintf("Tables: %d core, %d plugin, %d theme (%d total)",
		core, plugins, themes, len(tableRegistry))
}
