// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// ThemeFactory is a constructor function for creating theme instances.
// It receives the Engine and the theme directory path.
type ThemeFactory func(engine *Engine, themeDir string) coreTheme.Theme

// ThemeSource says where a registered theme's files live.
type ThemeSource struct {
	// Dir is the theme's directory on disk, handed to the factory as themeDir
	// and used to read the theme's files when it has no asset filesystem.
	// Empty means themes/<slug> in the working directory, the source tree.
	Dir string
	// TOML is the theme's theme.toml content when it is known at registration,
	// usually embedded with go:embed. Core reads metadata from it (for example
	// for the installer's theme list) without touching disk. Empty means read
	// <Dir>/theme.toml when needed.
	TOML string
}

// RegisteredTheme is one entry of the theme registry.
type RegisteredTheme struct {
	Slug   string
	Source ThemeSource
}

type themeEntry struct {
	factory ThemeFactory
	source  ThemeSource
}

// themeRegistry holds registered theme factories, keyed by theme slug.
var (
	themeRegistryMu sync.RWMutex
	themeRegistry   = make(map[string]themeEntry)
)

// RegisterTheme registers a theme factory under the given name. The theme's
// files are in themes/<name>. Themes call this from their init() function.
func RegisterTheme(name string, factory ThemeFactory) {
	RegisterThemeSource(name, ThemeSource{}, factory)
}

// RegisterThemeWithMeta is RegisterTheme plus the theme's embedded theme.toml,
// so core can describe the theme (installer theme list) without reading its
// directory.
func RegisterThemeWithMeta(name, themeTOML string, factory ThemeFactory) {
	RegisterThemeSource(name, ThemeSource{TOML: themeTOML}, factory)
}

// RegisterThemeSource registers a theme whose files may live outside the
// source tree, for example a theme package unpacked into a site directory.
func RegisterThemeSource(name string, source ThemeSource, factory ThemeFactory) {
	themeRegistryMu.Lock()
	defer themeRegistryMu.Unlock()
	themeRegistry[name] = themeEntry{factory: factory, source: source}
}

// unregisterTheme removes a registration; tests use it to clean up.
func unregisterTheme(name string) {
	themeRegistryMu.Lock()
	defer themeRegistryMu.Unlock()
	delete(themeRegistry, name)
}

// GetThemeFactory returns the registered factory for a theme name.
func GetThemeFactory(name string) (ThemeFactory, error) {
	themeRegistryMu.RLock()
	defer themeRegistryMu.RUnlock()
	entry, ok := themeRegistry[name]
	if !ok {
		available := make([]string, 0, len(themeRegistry))
		for k := range themeRegistry {
			available = append(available, k)
		}
		sort.Strings(available)
		return nil, fmt.Errorf("theme %q not registered; available: %v", name, available)
	}
	return entry.factory, nil
}

// AllThemeFactories returns a copy of all registered theme factories.
func AllThemeFactories() map[string]ThemeFactory {
	themeRegistryMu.RLock()
	defer themeRegistryMu.RUnlock()
	result := make(map[string]ThemeFactory, len(themeRegistry))
	for k, v := range themeRegistry {
		result[k] = v.factory
	}
	return result
}

// RegisteredThemes lists every registered theme sorted by slug, with its
// source directory resolved (see ThemeDir).
func RegisteredThemes() []RegisteredTheme {
	themeRegistryMu.RLock()
	defer themeRegistryMu.RUnlock()
	out := make([]RegisteredTheme, 0, len(themeRegistry))
	for slug, entry := range themeRegistry {
		source := entry.source
		source.Dir = themeDirFor(slug, source)
		out = append(out, RegisteredTheme{Slug: slug, Source: source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// ThemeDir returns the directory that holds the named theme's files: the
// directory it was registered with, or themes/<name>.
func ThemeDir(name string) string {
	themeRegistryMu.RLock()
	entry, ok := themeRegistry[name]
	themeRegistryMu.RUnlock()
	if !ok {
		return filepath.Join("themes", name)
	}
	return themeDirFor(name, entry.source)
}

func themeDirFor(slug string, source ThemeSource) string {
	if source.Dir != "" {
		return source.Dir
	}
	return filepath.Join("themes", slug)
}
