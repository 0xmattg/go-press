// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/pkg/dbprefix"
)

// packageSite is a throwaway site on PostgreSQL: a schema, a site directory
// and an upload directory that outlive one boot, so a test can restart it.
type packageSite struct {
	dsn       string
	schema    string
	siteDir   string
	uploadDir string
}

// newPackageSite creates the site, or skips without GOPRESS_TEST_PG_DSN.
func newPackageSite(t *testing.T) *packageSite {
	t.Helper()
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	site := &packageSite{
		dsn:       dsn,
		schema:    fmt.Sprintf("gopress_theme_package_itest_%d", time.Now().UnixNano()),
		siteDir:   t.TempDir(),
		uploadDir: t.TempDir(),
	}
	if err := admin.Exec(`CREATE SCHEMA "` + site.schema + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	// Registered first, so it runs after every engine's shutdown.
	t.Cleanup(func() {
		_ = admin.Exec(`DROP SCHEMA "` + site.schema + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return site
}

// boot starts an engine on the site the way BuildAndBootstrap does, with the
// theme packages at previews. setup, when not nil, runs after Bootstrap and
// before the themes load, as an earlier process would have left the site.
func (s *packageSite) boot(t *testing.T, setup func(e *Engine), previews ...string) *Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbprefix.Set(dbprefix.DefaultPrefix)
	db, err := gorm.Open(postgres.Open(s.dsn+" search_path="+s.schema), &gorm.Config{
		Logger:         gormlogger.Discard,
		NamingStrategy: schema.NamingStrategy{TablePrefix: dbprefix.DefaultPrefix},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Site:   config.SiteConfig{Name: "Example", URL: "https://example.test", Language: "en"},
		Server: config.ServerConfig{Mode: "debug"},
		CMS:    config.CMSConfig{JWTSecret: "theme-package-itest-secret-0123456789", JWTExpireHours: 1, UploadDir: s.uploadDir},
	}
	e := New(cfg, db)
	e.SiteDir = s.siteDir
	e.ExtStore = extension.NewStore(db)
	e.PackageArchives = extension.NewArchives(filepath.Join(s.siteDir, "packages", "archives"))
	e.previewThemePackages = previews
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := e.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := e.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(e)
	}
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	e.SetupRouter()
	return e
}

// packageSiteEngine boots a fresh site with the theme packages at paths as
// previews.
func packageSiteEngine(t *testing.T, paths ...string) *Engine {
	t.Helper()
	return newPackageSite(t).boot(t, nil, paths...)
}

func publishPost(t *testing.T, e *Engine, slug, title, body string) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	if err := e.Content.Create(&content.Content{Type: "post", Status: content.StatusPublished, Title: title, Slug: slug, Content: body, PublishedAt: &past}); err != nil {
		t.Fatal(err)
	}
}

func TestPGThemePackageServesASite(t *testing.T) {
	e := packageSiteEngine(t, samplePackageDir(t, nil))
	if e.ActiveThemeName() != samplePackageSlug {
		t.Fatalf("active theme = %q", e.ActiveThemeName())
	}
	publishPost(t, e, "hello", "Hello package", `<p>Package body</p><img src="x" onerror="alert(1)">`)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		e.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	home := get("/")
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "<h1>Welcome</h1>") ||
		!strings.Contains(home.Body.String(), `<a href="/blog/hello">Hello package</a>`) {
		t.Fatalf("home = %d %s", home.Code, home.Body.String())
	}
	single := get("/blog/hello")
	if single.Code != http.StatusOK || !strings.Contains(single.Body.String(), "<h1>Hello package</h1>") ||
		!strings.Contains(single.Body.String(), "<p>Package body</p>") || strings.Contains(single.Body.String(), "onerror") {
		t.Fatalf("single = %d %s", single.Code, single.Body.String())
	}
	if !strings.Contains(single.Body.String(), "<title>Hello package") || !strings.Contains(single.Body.String(), `rel="canonical" href="https://example.test/blog/hello"`) {
		t.Fatalf("single page has no SEO head: %s", single.Body.String())
	}
	// No archive page in the package: core's built-in archive answers.
	if archive := get("/blog"); archive.Code != http.StatusOK || !strings.Contains(archive.Body.String(), "Hello package") {
		t.Fatalf("archive = %d %s", archive.Code, archive.Body.String())
	}
	missing := get("/blog/no-such-post")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "Page not found") ||
		!strings.Contains(missing.Body.String(), `/static/css/style.css`) {
		t.Fatalf("404 = %d %s", missing.Code, missing.Body.String())
	}
	if css := get("/static/css/style.css"); css.Code != http.StatusOK || !strings.Contains(css.Body.String(), "--accent") {
		t.Fatalf("css = %d", css.Code)
	}
}

// demoPackage is the sample package with demo data and an image host.
func demoPackage(t *testing.T, seed string) string {
	t.Helper()
	dir := samplePackageDir(t, map[string]string{
		"demo/data/seed.toml":  seed,
		"static/img/cover.png": "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16),
	})
	toml, err := os.ReadFile(filepath.Join(dir, "theme.toml"))
	if err != nil {
		t.Fatal(err)
	}
	toml = append(toml, []byte("\n[[external]]\nhost = \"images.example.com\"\nkinds = [\"image\"]\npurpose = \"Demo images\"\n")...)
	if err := os.WriteFile(filepath.Join(dir, "theme.toml"), toml, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const allowedDemoSeed = `
[[settings]]
key = "hero_title"
value = "Demo title"

[[contents]]
type = "post"
title = "Demo post"
slug = "demo-post"
content = "<p>Demo body</p><script>alert(1)</script>"
image_url = "/static/img/cover.png"

[[contents]]
type = "post"
title = "Remote image"
slug = "remote-image"
content = "<p>x</p>"
image_url = "https://images.example.com/a.jpg"
`

func TestPGThemePackageDemoDataIsRestricted(t *testing.T) {
	bad := `
[[settings]]
key = "active_theme"
value = "x"

[[settings]]
key = "update_check_enabled"
value = "1"

[[contents]]
type = "post"
title = "SSRF"
slug = "ssrf"
image_url = "http://169.254.169.254/latest/meta-data"

[[contents]]
type = "post"
title = "Undeclared host"
slug = "undeclared"
image_url = "https://tracker.example.net/a.jpg"

[[contents]]
type = "post"
title = "Not in the package"
slug = "missing"
image_url = "/static/uploads/2026/01/a.png"
`
	e := packageSiteEngine(t, demoPackage(t, bad))
	publishPost(t, e, "existing", "Existing post", "<p>keep me</p>")
	if !e.HasThemeDemoData(samplePackageSlug) {
		t.Fatal("demo data not found")
	}
	err := e.ImportThemeDemoData(samplePackageSlug)
	if err == nil {
		t.Fatal("restricted demo data was imported")
	}
	for _, want := range []string{`"active_theme" is not declared`, `"update_check_enabled" is not declared`, "169.254.169.254", "tracker.example.net", "/static/uploads/2026/01/a.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
	// Nothing was deleted.
	if item, err := e.Content.FindBySlug("post", "existing"); err != nil || item == nil {
		t.Fatalf("existing content was removed: %v", err)
	}
	if e.IsThemeDemoImported(samplePackageSlug) {
		t.Fatal("a rejected import was marked as imported")
	}
}

func TestPGThemePackageDemoDataImports(t *testing.T) {
	e := packageSiteEngine(t, demoPackage(t, allowedDemoSeed))
	if err := e.ImportThemeDemoData(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	if got := e.Options.Get("hero_title"); got != "Demo title" {
		t.Fatalf("hero_title = %q", got)
	}
	post, err := e.Content.FindBySlug("post", "demo-post")
	if err != nil || post == nil {
		t.Fatalf("demo post: %v", err)
	}
	if strings.Contains(post.Content, "<script") || !strings.Contains(post.Content, "<p>Demo body</p>") {
		t.Fatalf("body was not sanitized: %q", post.Content)
	}
	if post.ImageURL != "/static/img/cover.png" {
		t.Fatalf("image_url = %q", post.ImageURL)
	}
	remote, err := e.Content.FindBySlug("post", "remote-image")
	if err != nil || remote == nil || remote.ImageURL != "https://images.example.com/a.jpg" {
		t.Fatalf("remote image = %+v, %v", remote, err)
	}
	if !e.IsThemeDemoImported(samplePackageSlug) {
		t.Fatal("import not recorded")
	}
	if entries, _ := os.ReadDir(filepath.Join(e.Config.CMS.UploadDir, "demo")); len(entries) != 0 {
		t.Fatalf("the server stored downloaded images: %v", entries)
	}
}

func TestPGUntrustedSeedNeverDownloadsImages(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer server.Close()
	e := packageSiteEngine(t, samplePackageDir(t, nil))
	prepared, err := prepareSeedData(fmt.Sprintf("[[contents]]\ntype = \"post\"\ntitle = \"x\"\nslug = \"x\"\nimage_url = %q\n", server.URL+"/a.png"))
	if err != nil {
		t.Fatal(err)
	}
	prepared.untrusted = true
	if err := e.forceSeed(prepared); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("the server made %d image requests for an untrusted seed", n)
	}

	// The same seed from compiled-in code still downloads, as before.
	trusted, err := prepareSeedData(fmt.Sprintf("[[contents]]\ntype = \"post\"\ntitle = \"y\"\nslug = \"y\"\nimage_url = %q\n", server.URL+"/b.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.forceSeed(trusted); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("trusted seed made %d image requests, want 1", n)
	}
}

func TestPGThemePackageResponsesCarryAContentSecurityPolicy(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	site := newPackageSite(t)
	e := site.boot(t, func(e *Engine) {
		activate(samplePackageSlug)(e)
		e.Hooks.AddFilter(hook.FrontendCSP, func(value interface{}, args ...interface{}) interface{} {
			sources := value.(CSPSources)
			sources.Connect = append(sources.Connect, "https://api.example.com", "'unsafe-eval'")
			return sources
		}, 10)
	}, samplePackageDir(t, nil))
	if e.ActiveThemeName() != samplePackageSlug {
		t.Fatalf("active theme = %q", e.ActiveThemeName())
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		e.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	first, second := get("/"), get("/")
	for name, w := range map[string]*httptest.ResponseRecorder{"miss": first, "hit": second, "404": get("/no-such-page")} {
		policy := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;") ||
			!strings.Contains(policy, "font-src 'self' data: https://fonts.gstatic.com;") ||
			!strings.Contains(policy, "script-src 'self';") ||
			!strings.Contains(policy, "connect-src 'self' https://api.example.com;") || strings.Contains(policy, "unsafe-eval") {
			t.Errorf("%s: policy = %q", name, policy)
		}
	}
	if !e.Cache.IsNoop() && second.Header().Get("X-Cache") != "HIT" {
		t.Errorf("second request X-Cache = %q, want a cache hit", second.Header().Get("X-Cache"))
	}
	if policy := get("/api/v1/types").Header().Get("Content-Security-Policy"); policy != "" {
		t.Errorf("API response got a policy: %s", policy)
	}

	if err := e.SwitchTheme("aa-good"); err != nil {
		t.Fatal(err)
	}
	if policy := get("/").Header().Get("Content-Security-Policy"); policy != "" {
		t.Fatalf("a built-in theme got a policy: %s", policy)
	}
}

// A package without single and 404 pages: core's generic blocks render inside
// the package's own layout instead of the standalone fallback pages.
func TestPGPagesWithoutATemplateUseThePackageLayout(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join("extension", "themepkg", "testdata", "minimal", "theme.toml"))
	if err != nil {
		t.Fatal(err)
	}
	selectable := "[[page_templates]]\nname = \"Single column\"\ntemplate = \"single\"\n"
	if !strings.Contains(string(manifest), selectable) {
		t.Fatal("the sample package no longer declares the single page template")
	}
	e := packageSiteEngine(t, samplePackageDir(t, map[string]string{
		"theme.toml":                  strings.Replace(string(manifest), selectable, "", 1),
		"templates/pages/single.tmpl": "",
		"templates/pages/404.tmpl":    "",
	}))
	if e.ActiveThemeName() != samplePackageSlug {
		t.Fatalf("active theme = %q", e.ActiveThemeName())
	}
	past := time.Now().Add(-time.Hour)
	if err := e.Content.Create(&content.Content{Type: "page", Status: content.StatusPublished, Title: "Terms of use", Slug: "terms",
		Content: `<h2>Accounts</h2><ul><li>Apply</li></ul><img src="x" onerror="alert(1)">`, PublishedAt: &past}); err != nil {
		t.Fatal(err)
	}
	publishPost(t, e, "hello", "Hello package", "<p>Body</p>")
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		e.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	inLayout := func(name string, w *httptest.ResponseRecorder, status int, wants ...string) {
		t.Helper()
		html := w.Body.String()
		if w.Code != status || !strings.Contains(html, `<main class="site-main">`) || !strings.Contains(html, `<footer class="site-footer">`) ||
			!strings.Contains(html, `/static/css/style.css`) {
			t.Fatalf("%s = %d, not in the package layout: %s", name, w.Code, html)
		}
		if strings.Contains(html, `<html lang="zh-CN">`) {
			t.Fatalf("%s used the standalone fallback", name)
		}
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}

	page := get("/terms")
	inLayout("page", page, http.StatusOK, "gp-page--single", `<h1 class="gp-page__title">Terms of use</h1>`, "<h2>Accounts</h2><ul><li>Apply</li></ul>",
		"<title>Terms of use", `rel="canonical" href="https://example.test/terms"`)
	if strings.Contains(page.Body.String(), "onerror") {
		t.Fatal("the page body was not sanitized")
	}
	inLayout("post", get("/blog/hello"), http.StatusOK, "gp-page--single", "Hello package", "<p>Body</p>", "<time datetime=")
	inLayout("archive", get("/blog"), http.StatusOK, "gp-page--archive", `<a href="/blog/hello">Hello package</a>`)
	inLayout("404", get("/blog/no-such-post"), http.StatusNotFound, "gp-page--not-found", "Page not found", `<a href="/">`)
}
