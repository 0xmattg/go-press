package contracts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/plugin"
	_ "github.com/0xmattg/go-press/internal/autoload"

	"github.com/BurntSushi/toml"
)

// Extension identity rules (GIP-M0002 §3.3): a theme or plugin directory name
// is its slug, slugs follow plugin.ValidSlug, a slug is never shared by a theme
// and a plugin, and table names are derived from the slug with no override.
// Modules are discovered by glob, so no module name is written here.

func extensionDirs(t *testing.T, kind, marker string) []string {
	t.Helper()
	manifests, err := filepath.Glob(filepath.Join(testRepositoryRoot(t), kind, "*", marker))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, m := range manifests {
		dir := filepath.Dir(m)
		goFiles, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		if len(goFiles) > 0 {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		t.Fatalf("no %s discovered", kind)
	}
	return dirs
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestExtensionDirectoriesAreTheirSlugs(t *testing.T) {
	themes := map[string]bool{}
	for _, dir := range extensionDirs(t, "themes", "theme.toml") {
		themes[filepath.Base(dir)] = true
	}
	plugins := map[string]bool{}
	for _, dir := range extensionDirs(t, "plugins", "plugin.toml") {
		name := filepath.Base(dir)
		plugins[name] = true
		var manifest struct {
			Plugin map[string]any `toml:"plugin"`
		}
		if _, err := toml.DecodeFile(filepath.Join(dir, "plugin.toml"), &manifest); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if slug, _ := manifest.Plugin["slug"].(string); slug != name {
			t.Errorf("plugin dir %q declares slug %q; the directory name must equal the slug", name, slug)
		}
		for _, key := range []string{"storage_slug", "table_prefix"} {
			if _, ok := manifest.Plugin[key]; ok {
				t.Errorf("plugin %q sets %s; table names are always derived from the slug", name, key)
			}
		}
	}

	for slug := range themes {
		if !plugin.ValidSlug(slug) {
			t.Errorf("theme slug %q is invalid", slug)
		}
		if plugins[slug] {
			t.Errorf("slug %q is used by both a theme and a plugin", slug)
		}
	}
	for slug := range plugins {
		if !plugin.ValidSlug(slug) {
			t.Errorf("plugin slug %q is invalid", slug)
		}
	}

	// Registration names (the first argument of RegisterTheme/RegisterPlugin)
	// must be exactly the directory names.
	if got, want := sortedKeys(core.AllThemeFactories()), sortedKeys(themes); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("registered themes %v differ from theme directories %v", got, want)
	}
	if got, want := sortedKeys(core.AllPluginFactories()), sortedKeys(plugins); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("registered plugins %v differ from plugin directories %v", got, want)
	}
}

var (
	stringConstRE = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
	pluginTableRE = regexp.MustCompile(`(?:dbprefix\.PluginTable|core\.RegisterPluginTable)\(\s*(\w+|"[^"]*")\s*,`)
)

// Every table a plugin creates already uses the derived storage slug, so the
// derivation rule changes no existing table name. Plugins still calling
// dbprefix.PluginTable directly instead of Meta.Table are listed in -v output.
func TestPluginTablesUseDerivedStorageSlug(t *testing.T) {
	for _, dir := range extensionDirs(t, "plugins", "plugin.toml") {
		slug := filepath.Base(dir)
		want := plugin.StorageSlug(slug)
		consts := map[string]string{}
		var refs []string
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range stringConstRE.FindAllStringSubmatch(string(data), -1) {
				consts[m[1]] = m[2]
			}
			for _, m := range pluginTableRE.FindAllStringSubmatch(string(data), -1) {
				refs = append(refs, m[1])
			}
		}
		if len(refs) == 0 {
			continue
		}
		t.Logf("plugin %q builds table names with dbprefix.PluginTable; prefer pluginMeta.Table", slug)
		for _, ref := range refs {
			got, ok := strings.CutPrefix(ref, `"`)
			if ok {
				got = strings.TrimSuffix(got, `"`)
			} else if got, ok = consts[ref]; !ok {
				t.Errorf("plugin %q: cannot resolve table slug %s", slug, ref)
				continue
			}
			if got != want {
				t.Errorf("plugin %q stores tables under %q, want the derived %q", slug, got, want)
			}
		}
	}
}
