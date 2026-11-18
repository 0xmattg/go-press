// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// samplePackageSlug is the slug of themepkg's sample package.
const samplePackageSlug = "minimal-sample"

// samplePackageDir copies themepkg's sample package to a temporary
// directory with changes applied; an empty value deletes the file.
func samplePackageDir(t *testing.T, changes map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("extension", "themepkg", "testdata", "minimal"))); err != nil {
		t.Fatal(err)
	}
	for name, content := range changes {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if content == "" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// buildPackage packs the source directory dir into a .zip and returns its
// path.
func buildPackage(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "package.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	report, err := themepkg.Build(f, os.DirFS(dir), themepkg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("package fails the check: %v", report.Problems)
	}
	return path
}

// packageEngine is themeSetEngine with the services a theme package's
// templates need.
func packageEngine(t *testing.T, activeTheme string, packages ...string) *Engine {
	t.Helper()
	e := themeSetEngine(t, activeTheme, "")
	e.Config.CMS.UploadDir = t.TempDir()
	e.Rewrite = rewrite.NewEngine(e.Registry)
	e.SEO = rewrite.NewSEOBuilder("https://example.test", "Example", e.Rewrite)
	e.Media = media.NewRepository(nil)
	e.previewThemePackages = packages
	return e
}

func TestReadThemePackageReadsDirectoriesAndArchives(t *testing.T) {
	dir := samplePackageDir(t, nil)
	for _, path := range []string{dir, buildPackage(t, dir)} {
		pkg, report, err := ReadThemePackage(path)
		if err != nil {
			t.Fatal(err)
		}
		if pkg == nil || pkg.ID().Slug != samplePackageSlug {
			t.Fatalf("%s: package %v, problems %v", path, pkg, report.Problems)
		}
	}
	if _, _, err := ReadThemePackage(filepath.Join(dir, "missing.zip")); err == nil {
		t.Fatal("a missing path was read")
	}
}

func TestReadThemePackageRejectsBuiltInSlugsAndUnmetCore(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{samplePackageSlug: builds(okTheme(samplePackageSlug))})
	pkg, report, err := ReadThemePackage(samplePackageDir(t, nil))
	if err != nil || pkg != nil || !hasPackageProblem(report, themepkg.RuleManifestReserved) {
		t.Fatalf("built-in slug: package %v, err %v, report %+v", pkg, err, report)
	}

	unregisterTheme(samplePackageSlug)
	dir := samplePackageDir(t, nil)
	toml, err := os.ReadFile(filepath.Join(dir, "theme.toml"))
	if err != nil {
		t.Fatal(err)
	}
	future := strings.Replace(string(toml), `core = ">=0.7.0"`, `core = ">=99.0.0"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "theme.toml"), []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, report, err = ReadThemePackage(dir)
	if err != nil || pkg != nil || !hasPackageProblem(report, themepkg.RuleManifestCoreUnmet) {
		t.Fatalf("unmet core: package %v, err %v, report %+v", pkg, err, report)
	}
}

func hasPackageProblem(report *themepkg.Report, rule string) bool {
	if report == nil {
		return false
	}
	for _, p := range report.Problems {
		if p.Rule == rule {
			return true
		}
	}
	return false
}

func TestLoadThemePackageChecksTheDigest(t *testing.T) {
	e := packageEngine(t, "")
	dir := samplePackageDir(t, nil)
	pkg, _, err := ReadThemePackage(dir)
	if err != nil || pkg == nil {
		t.Fatal(err)
	}
	th, err := e.LoadThemePackage(ThemePackageSource{Path: dir, Source: coreTheme.PackageSourceInstalled, Digest: pkg.Digest()})
	if err != nil {
		t.Fatal(err)
	}
	if info := th.PackageInfo(); info.Slug != samplePackageSlug || info.Source != coreTheme.PackageSourceInstalled || info.Digest != pkg.Digest() {
		t.Fatalf("info = %+v", info)
	}
	_, err = e.LoadThemePackage(ThemePackageSource{Path: dir, Source: coreTheme.PackageSourceInstalled, Digest: "h1:AAAA"})
	if !errors.Is(err, ErrPackageArchive) {
		t.Fatalf("wrong digest: err = %v", err)
	}
	_, err = e.LoadThemePackage(ThemePackageSource{Path: filepath.Join(dir, "missing.zip"), Digest: pkg.Digest()})
	if !errors.Is(err, ErrPackageArchive) {
		t.Fatalf("missing archive: err = %v", err)
	}
	// A package that fails the check is incompatible, not broken.
	_, err = e.LoadThemePackage(ThemePackageSource{Path: samplePackageDir(t, map[string]string{"static/js/main.js": "alert(1)"})})
	if err == nil || errors.Is(err, ErrPackageArchive) || !strings.Contains(err.Error(), themepkg.RuleScriptFile) {
		t.Fatalf("failing package: err = %v", err)
	}
}

func TestLoadAllThemesLoadsThemePackages(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	good := samplePackageDir(t, nil)
	broken := samplePackageDir(t, map[string]string{"templates/pages/home.tmpl": `{{define "content"}}<a onclick="x()">x</a>{{end}}`})
	duplicate := buildPackage(t, good)
	e := packageEngine(t, samplePackageSlug, good, broken, duplicate)
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	if e.ActiveThemeName() != samplePackageSlug || e.ThemeFallback() != nil {
		t.Fatalf("active = %q, fallback = %+v", e.ActiveThemeName(), e.ThemeFallback())
	}
	info, ok := coreTheme.PackageInfoOf(e.ActiveTheme())
	if !ok || info.Source != coreTheme.PackageSourcePreview {
		t.Fatalf("active theme package info = %+v, %v", info, ok)
	}
	issues := map[string]string{}
	for _, issue := range e.ThemeLoadIssues() {
		issues[issue.Slug] = issue.Reason
	}
	if !strings.Contains(issues[broken], themepkg.RuleScriptHandler) || !strings.Contains(issues[duplicate], "already provides") || len(issues) != 2 {
		t.Fatalf("issues = %v", issues)
	}
	if got := len(e.AvailableThemes()); got != 2 {
		t.Fatalf("available themes = %d, want the compiled theme and one package", got)
	}
	// Activation registered what theme.toml declares.
	if locs := e.Menus.GetLocations(); len(locs) != 1 || locs[0].Name != "primary" {
		t.Fatalf("menu locations from theme.toml = %+v", locs)
	}
	if ref := e.ThemeSettingsTemplate(samplePackageSlug); ref.Valid() {
		t.Fatal("a theme package must not get an admin settings template")
	}
	schema, err := e.ThemeSettingsSchema(samplePackageSlug)
	if err != nil || schema == nil {
		t.Fatalf("settings schema = %v, %v", schema, err)
	}
}

func TestLoadAllThemesFallsBackWhenThePackageIsGone(t *testing.T) {
	// active_theme names a package that is not loaded this time, as after
	// a restart without -theme-package.
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	e := packageEngine(t, samplePackageSlug)
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	if fb := e.ThemeFallback(); fb == nil || fb.Requested != samplePackageSlug || fb.Used != "aa-good" {
		t.Fatalf("fallback = %+v", fb)
	}
}

func TestThemePackageRendersHomeAndNotFound(t *testing.T) {
	e := packageEngine(t, samplePackageSlug, samplePackageDir(t, nil))
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		e.ActiveTheme().ServeHTTP(c)
		return w
	}
	home := get("/")
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "<h1>Welcome</h1>") || !strings.Contains(home.Body.String(), "No posts yet.") {
		t.Fatalf("home = %d %s", home.Code, home.Body.String())
	}
	if !strings.Contains(home.Body.String(), `/static/css/style.css`) {
		t.Fatal("home does not use the package layout")
	}
	// The rewrite engine resolves nothing without content types for the
	// path, so the theme's 404 page answers.
	missing := get("/no-such-page/at/all")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "Page not found") {
		t.Fatalf("404 = %d %s", missing.Code, missing.Body.String())
	}
}

func TestThemePackageStaticFilesGetUploadGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`
	e := packageEngine(t, samplePackageSlug, samplePackageDir(t, map[string]string{"static/img/logo.svg": svg}))
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/static/*filepath", e.serveStatic)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	css := get("/static/css/style.css")
	if css.Code != http.StatusOK || !strings.Contains(css.Header().Get("Content-Type"), "text/css") || css.Header().Get("Content-Disposition") != "" {
		t.Fatalf("css = %d %v", css.Code, css.Header())
	}
	if css.Header().Get("Last-Modified") == "" {
		t.Fatal("package files should support conditional requests")
	}
	logo := get("/static/img/logo.svg")
	if logo.Code != http.StatusOK || logo.Header().Get("Content-Disposition") != "attachment" ||
		logo.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" || logo.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("svg = %d %v", logo.Code, logo.Header())
	}

	// Go themes are compiled-in code: their files keep being served inline.
	goTheme := &activationTheme{name: "go-theme", cfg: &coreTheme.FileConfig{}, files: fstest.MapFS{"static/logo.svg": {Data: []byte(svg)}}}
	e.themes = withTheme(e.themes, "go-theme", goTheme)
	if err := e.activateTheme("go-theme"); err != nil {
		t.Fatal(err)
	}
	if w := get("/static/logo.svg"); w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("go theme svg = %d %v", w.Code, w.Header())
	}
}

// packageLikeTheme is a theme that reports being a theme package, with files
// the package check would have refused.
type packageLikeTheme struct{ *activationTheme }

func (packageLikeTheme) PackageInfo() coreTheme.PackageInfo {
	return coreTheme.PackageInfo{Slug: "pkg-like", Source: coreTheme.PackageSourceInstalled}
}

func TestThemePackagesNeverGetAdminSettingsTemplates(t *testing.T) {
	files := fstest.MapFS{coreTheme.SettingsTemplateName: {Data: []byte(`<script>alert(1)</script>`)}}
	e := packageEngine(t, "")
	e.themes = withTheme(e.themes, "go-theme", &activationTheme{name: "go-theme", cfg: &coreTheme.FileConfig{}, files: files})
	e.themes = withTheme(e.themes, "pkg-like", packageLikeTheme{&activationTheme{name: "pkg-like", cfg: &coreTheme.FileConfig{}, files: files}})
	if !e.ThemeSettingsTemplate("go-theme").Valid() {
		t.Fatal("a Go theme lost its settings template")
	}
	if e.ThemeSettingsTemplate("pkg-like").Valid() {
		t.Fatal("a theme package got an admin settings template")
	}
}

func TestThemePackagePreviewsAreRefusedInReleaseMode(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Mode: "release"}}
	_, err := BuildAndBootstrapWithOptions(cfg, "site/config.toml", BootstrapOptions{ThemePackages: []string{"./aurora"}})
	if err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("err = %v", err)
	}
}
