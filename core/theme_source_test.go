// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/assets"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// packageTheme is how a theme whose files live in their own directory (a
// theme package, GIP-A0001b) is built: BaseTheme reads theme.toml, templates,
// static files and locales from that directory.
type packageTheme struct {
	coreTheme.BaseTheme
}

func (t *packageTheme) Setup(coreTheme.App)             {}
func (t *packageTheme) TemplateFuncs() template.FuncMap { return template.FuncMap{} }
func (t *packageTheme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *packageTheme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

func packageThemeFactory(gotDir *string) ThemeFactory {
	return func(e *Engine, themeDir string) coreTheme.Theme {
		*gotDir = themeDir
		th := &packageTheme{}
		th.InitBaseFS(e, themeDir, os.DirFS(themeDir), "", nil)
		th.LoadTemplates(th)
		return th
	}
}

func writePackageTheme(t *testing.T, name, greeting string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"theme.toml": "[theme]\nname = \"" + name + "\"\nversion = \"1.2.3\"\n\n" +
			"[[menu_locations]]\nname = \"" + name + "-menu\"\nlabel = \"Menu\"\n",
		"templates/layouts/base.tmpl":         `{{define "base"}}<main>{{template "content" .}}</main>{{end}}`,
		"templates/pages/home.tmpl":           `{{define "content"}}home{{end}}`,
		"templates/admin/theme_settings.tmpl": `{{define "content"}}settings{{end}}`,
		"static/css/site.css":                 `body{}`,
		"locales/en.json":                     `{"greeting":"` + greeting + `","only.` + name + `":"x"}`,
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newThemeSourceTestEngine(active string) *Engine {
	cfg := &config.Config{}
	cfg.Site.Theme = active
	return &Engine{
		Config:   cfg,
		Registry: content.NewRegistry(),
		Menus:    menu.NewStore(nil),
		Hooks:    hook.New(),
		I18n:     coreI18n.NewManager("en"),
		Options:  option.NewStore(nil),
		themes:   map[string]coreTheme.Theme{},
	}
}

// T21 ①: a theme directory outside the source tree, holding only theme.toml,
// templates/, static/ and locales/, is registered, activated and rendered.
// T19: the active theme's locale messages follow theme switches.
func TestThemeFromDirectoryOutsideSourceTree(t *testing.T) {
	dirA := writePackageTheme(t, "pkg-a", "Hello A")
	dirB := writePackageTheme(t, "pkg-b", "Hello B")
	var gotA, gotB string
	RegisterThemeSource("pkg-a", ThemeSource{Dir: dirA}, packageThemeFactory(&gotA))
	RegisterThemeSource("pkg-b", ThemeSource{Dir: dirB}, packageThemeFactory(&gotB))
	t.Cleanup(func() { unregisterTheme("pkg-a"); unregisterTheme("pkg-b") })

	e := newThemeSourceTestEngine("pkg-a")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatalf("LoadAllThemes: %v", err)
	}
	if gotA != dirA || ThemeDir("pkg-a") != dirA {
		t.Fatalf("factory got themeDir %q, ThemeDir = %q; want the registered %q", gotA, ThemeDir("pkg-a"), dirA)
	}
	th := e.ActiveTheme()
	if e.ActiveThemeName() != "pkg-a" || th.Name() != "pkg-a" || th.Version() != "1.2.3" {
		t.Fatalf("active = %q, name %q version %q; want metadata from the directory's theme.toml", e.ActiveThemeName(), th.Name(), th.Version())
	}
	if labels := menuLocationLabels(e); labels["pkg-a-menu"] != "Menu" {
		t.Fatalf("menu locations = %v, want the ones declared in the directory's theme.toml", labels)
	}

	// Rendering: an extension fragment inside the theme layout, and a static file.
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	defaults := fstest.MapFS{"page.tmpl": {Data: []byte(`{{define "content"}}fragment{{end}}`)}}
	if err := e.RenderNamespacedInActiveThemeFS(c, "ext", "page", defaults, nil); err != nil || rec.Body.String() != "<main>fragment</main>" {
		t.Fatalf("render = %q, %v", rec.Body.String(), err)
	}
	if static := serveStaticForTest(e, "/static/css/site.css", nil); static.Code != http.StatusOK || static.Body.String() != "body{}" {
		t.Fatalf("static = %d %q", static.Code, static.Body.String())
	}
	if !e.ThemeSettingsTemplate("pkg-a").Valid() {
		t.Fatal("settings page at the conventional path must be found")
	}
	if pages := th.(*packageTheme).PageTemplates; pages["home"] == nil {
		t.Fatalf("page bundles = %v, want home from the directory", pages)
	}

	// T19: keys and text come from the active theme's locales/ and follow a switch.
	assertMessages := func(want map[string]string) {
		t.Helper()
		if got := e.ActiveThemeLocaleMessages()["en"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("ActiveThemeLocaleMessages()[en] = %v, want %v", got, want)
		}
	}
	assertMessages(map[string]string{"greeting": "Hello A", "only.pkg-a": "x"})
	if err := e.activateTheme("pkg-b"); err != nil {
		t.Fatal(err)
	}
	assertMessages(map[string]string{"greeting": "Hello B", "only.pkg-b": "x"})

	messages := e.ActiveThemeLocaleMessages()
	messages["en"]["greeting"] = "mutated"
	assertMessages(map[string]string{"greeting": "Hello B", "only.pkg-b": "x"})

	found := false
	for _, rt := range RegisteredThemes() {
		if rt.Slug == "pkg-b" && rt.Source.Dir == dirB {
			found = true
		}
	}
	if !found {
		t.Fatalf("RegisteredThemes() = %v, want pkg-b with its directory", RegisteredThemes())
	}
}

// A theme without FileConfigProvider gets its theme.toml from the directory it
// was registered with, not from themes/<slug>.
func TestActivateThemeReadsConfigFromRegisteredDirectory(t *testing.T) {
	dir := writePackageTheme(t, "plain", "Hi")
	RegisterThemeSource("plain-dir", ThemeSource{Dir: dir}, func(*Engine, string) coreTheme.Theme {
		return shellTestTheme{templateDir: filepath.Join(dir, "templates")}
	})
	t.Cleanup(func() { unregisterTheme("plain-dir") })

	e := newThemeSourceTestEngine("plain-dir")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatalf("LoadAllThemes: %v", err)
	}
	if labels := menuLocationLabels(e); labels["plain-menu"] != "Menu" {
		t.Fatalf("menu locations = %v, want theme.toml from the registered directory", labels)
	}
	if got := e.ActiveThemeLocaleMessages()["en"]["greeting"]; got != "Hi" {
		t.Fatalf("greeting = %q, want locales from the registered directory", got)
	}
}

func TestThemeRegistryDefaultsToSourceTree(t *testing.T) {
	RegisterThemeWithMeta("meta-test", "[theme]\nname = \"Meta\"\n", func(*Engine, string) coreTheme.Theme { return nil })
	t.Cleanup(func() { unregisterTheme("meta-test") })
	if got, want := ThemeDir("meta-test"), filepath.Join("themes", "meta-test"); got != want {
		t.Fatalf("ThemeDir = %q, want %q", got, want)
	}
	if got, want := ThemeDir("not-registered"), filepath.Join("themes", "not-registered"); got != want {
		t.Fatalf("ThemeDir(unregistered) = %q, want %q", got, want)
	}
	for _, rt := range RegisteredThemes() {
		if rt.Slug == "meta-test" && (rt.Source.TOML == "" || rt.Source.Dir != filepath.Join("themes", "meta-test")) {
			t.Fatalf("registered = %+v, want embedded TOML and the default directory", rt)
		}
	}
	if _, err := GetThemeFactory("meta-test"); err != nil {
		t.Fatal(err)
	}
}

// T21 ②: a plugin whose files exist only in Assets(), with no plugins/<slug>
// directory, renders its settings page, admin catalog and fragments, also
// outside release mode where core would prefer the directory on disk.
func TestPluginWithOnlyBuiltinFiles(t *testing.T) {
	t.Chdir(t.TempDir()) // no plugins/ directory here
	p := &assetTestPlugin{files: fstest.MapFS{
		plugin.SettingsTemplateName:    {Data: []byte(`{{define "content"}}settings{{end}}`)},
		"locales/admin/en.json":        {Data: []byte(`{"plugin.name":"Built-in Only"}`)},
		"templates/widgets/badge.tmpl": {Data: []byte(`{{define "content"}}badge{{end}}`)},
	}}
	e := newAssetTestEngine(newAssetTestTheme(assetTestFiles()), "debug")
	if !e.PreferDiskAssets() {
		t.Fatal("debug mode must prefer disk")
	}

	ref := e.pluginSettingsTemplate(p)
	if data, err := fs.ReadFile(ref.FS, ref.Name); err != nil || string(data) != `{{define "content"}}settings{{end}}` {
		t.Fatalf("settings template = %q, %v", data, err)
	}
	if catalog := e.pluginAdminCatalog(p); catalog == nil || catalog.T("en", "plugin.name") != "Built-in Only" {
		t.Fatal("admin catalog must come from the built-in files")
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if err := e.RenderNamespacedInActiveThemeFS(c, "widgets", "badge", assets.Sub(e.PluginFS(p), "templates/widgets"), nil); err != nil || rec.Body.String() != "<main>badge</main>" {
		t.Fatalf("fragment render = %q, %v", rec.Body.String(), err)
	}
}

// T9: modules still reading their directories on disk are listed once.
func TestDiskFallbackModulesAreListed(t *testing.T) {
	legacy := &assetTestTheme{}
	legacy.InitBase(nil, t.TempDir(), "[theme]\nname = \"legacy\"\n", nil)
	themes := map[string]coreTheme.Theme{
		"zeta-legacy":  legacy,
		"embedded":     newAssetTestTheme(assetTestFiles()),
		"alpha-legacy": shellTestTheme{},
	}
	if got, want := diskFallbackThemes(themes), []string{"alpha-legacy", "zeta-legacy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("diskFallbackThemes = %v, want %v", got, want)
	}
	plugins := []plugin.Plugin{legacySettingsPlugin{}, &assetTestPlugin{files: fstest.MapFS{}}}
	got := diskFallbackPlugins(plugins)
	if !sort.StringsAreSorted(got) || !reflect.DeepEqual(got, []string{"legacy-plugin"}) {
		t.Fatalf("diskFallbackPlugins = %v, want [legacy-plugin]", got)
	}
}
