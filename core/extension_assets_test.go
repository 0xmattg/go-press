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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

const assetTestSlug = "asset-test"

// assetTestTheme reads its files through BaseTheme.InitBaseFS.
type assetTestTheme struct {
	coreTheme.BaseTheme
}

func (t *assetTestTheme) Setup(coreTheme.App)             {}
func (t *assetTestTheme) TemplateFuncs() template.FuncMap { return template.FuncMap{} }
func (t *assetTestTheme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *assetTestTheme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

func newAssetTestTheme(files fs.FS) *assetTestTheme {
	th := &assetTestTheme{}
	// The theme directory does not exist, so nothing can come from disk.
	th.InitBaseFS(nil, filepath.Join("themes", "no-such-theme-dir"), files, "[theme]\nname = \"asset-test\"\nversion = \"1.0.0\"\n", nil)
	return th
}

func assetTestFiles() fstest.MapFS {
	return fstest.MapFS{
		"templates/layouts/base.tmpl":         {Data: []byte(`{{define "base"}}<main>{{template "content" .}}</main>{{end}}`)},
		"templates/commerce/override.tmpl":    {Data: []byte(`{{define "content"}}theme override{{end}}`)},
		"templates/admin/theme_settings.tmpl": {Data: []byte(`{{define "content"}}settings{{end}}`)},
		"demo/data/seed.toml":                 {Data: []byte("[[settings]]\nkey = \"site_name\"\nvalue = \"Demo\"\n")},
		"static/css/style.css":                {Data: []byte(`body{color:red}`)},
	}
}

func newAssetTestEngine(th coreTheme.Theme, mode string) *Engine {
	return &Engine{
		Config:          &config.Config{Server: config.ServerConfig{Mode: mode}},
		themes:          map[string]coreTheme.Theme{assetTestSlug: th},
		activeThemeName: assetTestSlug,
	}
}

func serveStaticForTest(e *Engine, target string, header http.Header) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/static/*filepath", e.serveStatic)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestServeStaticFromThemeAssetFilesystemUsesETag(t *testing.T) {
	e := newAssetTestEngine(newAssetTestTheme(assetTestFiles()), "release")

	rec := serveStaticForTest(e, "/static/css/style.css?v=3", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "body{color:red}" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("content-type = %q, want text/css", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q, want immutable for versioned assets", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Last-Modified") != "" {
		t.Fatalf("embedded file: ETag = %q, Last-Modified = %q; want content ETag only", etag, rec.Header().Get("Last-Modified"))
	}

	again := serveStaticForTest(e, "/static/css/style.css", http.Header{"If-None-Match": {etag}})
	if again.Code != http.StatusNotModified {
		t.Fatalf("conditional request status = %d, want 304", again.Code)
	}
	if cc := again.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Fatalf("cache-control = %q, want one-day cache for unversioned assets", cc)
	}
}

func TestServeStaticFromThemeAssetFilesystemRejectsEscapes(t *testing.T) {
	e := newAssetTestEngine(newAssetTestTheme(assetTestFiles()), "release")
	for _, target := range []string{
		"/static/css",
		"/static/missing.css",
		"/static/../templates/layouts/base.tmpl",
		"/static/%2e%2e/templates/layouts/base.tmpl",
	} {
		rec := serveStaticForTest(e, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "define") {
			t.Fatalf("%s leaked a template: %q", target, rec.Body.String())
		}
	}
}

func TestServeStaticFromThemeDirectoryKeepsLastModified(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "static", "app.js"), []byte("ok()"), 0o644); err != nil {
		t.Fatal(err)
	}
	th := &assetTestTheme{}
	th.InitBase(nil, dir, "[theme]\nname = \"asset-test\"\nversion = \"1.0.0\"\n", nil)
	rec := serveStaticForTest(newAssetTestEngine(th, "release"), "/static/app.js", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok()" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Last-Modified") == "" {
		t.Fatal("files on disk must keep Last-Modified")
	}
}

func TestThemeSettingsAndDemoDataUseConventionalPaths(t *testing.T) {
	e := newAssetTestEngine(newAssetTestTheme(assetTestFiles()), "release")
	ref := e.ThemeSettingsTemplate(assetTestSlug)
	if !ref.Valid() || ref.Name != coreTheme.SettingsTemplateName {
		t.Fatalf("settings template = %+v, want %s", ref, coreTheme.SettingsTemplateName)
	}
	if !e.HasThemeDemoData(assetTestSlug) {
		t.Fatal("demo seed at demo/data/seed.toml must be found")
	}
	if e.ThemeSettingsTemplatePath(assetTestSlug) != "" || e.ThemeDemoSeedPath(assetTestSlug) != "" {
		t.Fatal("deprecated path accessors only report disk paths")
	}

	files := assetTestFiles()
	delete(files, coreTheme.SettingsTemplateName)
	delete(files, coreTheme.DemoSeedName)
	bare := newAssetTestEngine(newAssetTestTheme(files), "release")
	if bare.ThemeSettingsTemplate(assetTestSlug).Valid() || bare.HasThemeDemoData(assetTestSlug) {
		t.Fatal("a theme without the conventional files has no settings page or demo data")
	}
	if e.ThemeSettingsTemplate("unknown").Valid() || e.HasThemeDemoData("unknown") {
		t.Fatal("unknown themes have no settings page or demo data")
	}
}

func TestForceSeedFromFSRejectsMissingAndMalformedSeeds(t *testing.T) {
	e := &Engine{}
	if err := e.ForceSeedFromFS(nil, "seed.toml"); err == nil {
		t.Fatal("nil filesystem must fail")
	}
	if err := e.ForceSeedFromFS(fstest.MapFS{}, "seed.toml"); err == nil || !strings.Contains(err.Error(), "failed to parse seed file") {
		t.Fatalf("missing seed: err = %v", err)
	}
	broken := fstest.MapFS{"seed.toml": {Data: []byte("[[settings]\n")}}
	if err := e.ForceSeedFromFS(broken, "seed.toml"); err == nil || !strings.Contains(err.Error(), "failed to parse seed file") {
		t.Fatalf("malformed seed: err = %v", err)
	}
}

func TestRenderNamespacedInActiveThemeFSPrefersThemeOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newAssetTestEngine(newAssetTestTheme(assetTestFiles()), "release")
	defaults := fstest.MapFS{
		"override.tmpl": {Data: []byte(`{{define "content"}}plugin default{{end}}`)},
		"fallback.tmpl": {Data: []byte(`{{define "content"}}plugin fallback{{end}}`)},
	}
	for fragment, want := range map[string]string{
		"override": "<main>theme override</main>",
		"fallback": "<main>plugin fallback</main>",
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		if err := e.RenderNamespacedInActiveThemeFS(c, "commerce", fragment, defaults, gin.H{}); err != nil {
			t.Fatalf("%s: %v", fragment, err)
		}
		if got := rec.Body.String(); got != want {
			t.Fatalf("%s rendered %q, want %q", fragment, got, want)
		}
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if err := e.RenderNamespacedInActiveThemeFS(c, "commerce", "missing", defaults, nil); err == nil {
		t.Fatal("a fragment in neither the theme nor the defaults must fail")
	}
}

// assetTestPlugin ships its files as an asset filesystem.
type assetTestPlugin struct{ files fs.FS }

func (p *assetTestPlugin) Name() string                 { return "asset-plugin" }
func (p *assetTestPlugin) Version() string              { return "1.0.0" }
func (p *assetTestPlugin) Description() string          { return "" }
func (p *assetTestPlugin) Activate(plugin.App)          {}
func (p *assetTestPlugin) Deactivate(plugin.App)        {}
func (p *assetTestPlugin) Assets() fs.FS                { return p.files }
func (p *assetTestPlugin) SettingsTemplatePath() string { return "ignored/for/asset/plugins.tmpl" }

func TestPluginFSSettingsAndCatalogComeFromAssets(t *testing.T) {
	p := &assetTestPlugin{files: fstest.MapFS{
		plugin.SettingsTemplateName: {Data: []byte(`{{define "content"}}builtin{{end}}`)},
		"locales/admin/en.json":     {Data: []byte(`{"plugin.name":"Asset Plugin"}`)},
	}}
	e := &Engine{Config: &config.Config{Server: config.ServerConfig{Mode: "release"}}}

	ref := e.pluginSettingsTemplate(p)
	if !ref.Valid() || ref.Name != plugin.SettingsTemplateName {
		t.Fatalf("settings template = %+v, want the conventional path, not SettingsTemplatePath", ref)
	}
	if data, err := fs.ReadFile(ref.FS, ref.Name); err != nil || !strings.Contains(string(data), "builtin") {
		t.Fatalf("settings template read %q, %v", data, err)
	}
	catalog := e.pluginAdminCatalog(p)
	if catalog == nil || catalog.T("en", "plugin.name") != "Asset Plugin" {
		t.Fatal("admin catalog must load from locales/admin in the asset filesystem")
	}

	without := &assetTestPlugin{files: fstest.MapFS{}}
	if e.pluginSettingsTemplate(without).Valid() {
		t.Fatal("an asset plugin without templates/admin/settings.tmpl has no settings page")
	}
}

func TestPluginFSPrefersPluginDirectoryOutsideReleaseMode(t *testing.T) {
	work := t.TempDir()
	settings := filepath.Join(work, "plugins", "asset-plugin", filepath.FromSlash(plugin.SettingsTemplateName))
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	p := &assetTestPlugin{files: fstest.MapFS{plugin.SettingsTemplateName: {Data: []byte("builtin")}}}
	for mode, want := range map[string]string{"debug": "disk", "": "disk", "release": "builtin"} {
		e := &Engine{Config: &config.Config{Server: config.ServerConfig{Mode: mode}}}
		data, err := fs.ReadFile(e.PluginFS(p), plugin.SettingsTemplateName)
		if err != nil || string(data) != want {
			t.Fatalf("mode %q read %q, %v; want %q", mode, data, err, want)
		}
	}
}

func TestPluginWithoutAssetsKeepsSettingsTemplatePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates", "admin", "settings.tmpl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := (&Engine{}).pluginSettingsTemplate(legacySettingsPlugin{path: path})
	if !ref.Valid() {
		t.Fatal("legacy plugin settings path must still resolve")
	}
	if data, err := fs.ReadFile(ref.FS, ref.Name); err != nil || string(data) != "legacy" {
		t.Fatalf("legacy settings read %q, %v", data, err)
	}
	if (&Engine{}).pluginSettingsTemplate(legacySettingsPlugin{}).Valid() {
		t.Fatal("an empty SettingsTemplatePath means no settings page")
	}
	if got := admin.TemplateRefFromPath(""); got.Valid() {
		t.Fatalf("TemplateRefFromPath(\"\") = %+v, want zero", got)
	}
}

type legacySettingsPlugin struct{ path string }

func (p legacySettingsPlugin) Name() string                 { return "legacy-plugin" }
func (p legacySettingsPlugin) Version() string              { return "1.0.0" }
func (p legacySettingsPlugin) Description() string          { return "" }
func (p legacySettingsPlugin) Activate(plugin.App)          {}
func (p legacySettingsPlugin) Deactivate(plugin.App)        {}
func (p legacySettingsPlugin) SettingsTemplatePath() string { return p.path }
