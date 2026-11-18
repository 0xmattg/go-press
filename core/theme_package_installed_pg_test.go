// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// packageVersion is the sample package as version, with a home page that
// says which version it is.
func packageVersion(t *testing.T, version string) string {
	t.Helper()
	dir := samplePackageDir(t, map[string]string{
		"templates/pages/home.tmpl": `{{define "content"}}<p class="version">home ` + version + `</p>{{end}}`,
	})
	setThemeTOML(t, dir, `version = "1.0.0"`, `version = "`+version+`"`)
	return dir
}

// setThemeTOML replaces old with new in dir's theme.toml.
func setThemeTOML(t *testing.T, dir, old, new string) {
	t.Helper()
	path := filepath.Join(dir, "theme.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("theme.toml has no %q", old)
	}
	if err := os.WriteFile(path, bytes.Replace(data, []byte(old), []byte(new), 1), 0o644); err != nil {
		t.Fatal(err)
	}
}

// install stores the package built from dir and records it, as the install
// flow will: archive first, then the row.
func install(t *testing.T, e *Engine, dir string, makeCurrent bool) extension.Package {
	t.Helper()
	var buf bytes.Buffer
	report, err := themepkg.Build(&buf, os.DirFS(dir), themepkg.Options{})
	if err != nil || !report.OK() {
		t.Fatalf("build: %v %v", err, report.Problems)
	}
	_, size, err := e.PackageArchives.Put(report.ID.Slug, report.ID.Version, report.Digest, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	pkg := extension.Package{Kind: extension.KindTheme, Slug: report.ID.Slug, Version: report.ID.Version,
		Digest: report.Digest, Size: size, Source: extension.SourceUpload}
	if err := e.ExtStore.Add(context.Background(), &pkg, makeCurrent); err != nil {
		t.Fatal(err)
	}
	return pkg
}

func archivePath(t *testing.T, e *Engine, pkg extension.Package) string {
	t.Helper()
	path, err := e.PackageArchives.Path(pkg.Slug, pkg.Version, pkg.Digest)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func packageStatus(t *testing.T, e *Engine, id int64) string {
	t.Helper()
	pkg, err := e.ExtStore.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Status
}

func homeBody(t *testing.T, e *Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	e.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("home = %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func activate(slug string) func(e *Engine) {
	return func(e *Engine) {
		if err := e.Options.Set("active_theme", slug); err != nil {
			panic(err)
		}
	}
}

func TestPGInstalledThemePackageSurvivesRestarts(t *testing.T) {
	site := newPackageSite(t)
	var pkg extension.Package
	e := site.boot(t, func(e *Engine) {
		pkg = install(t, e, packageVersion(t, "1.0.0"), true)
		activate(samplePackageSlug)(e)
	})
	if e.ActiveThemeName() != samplePackageSlug || e.ThemeFallback() != nil {
		t.Fatalf("active = %q, fallback = %+v", e.ActiveThemeName(), e.ThemeFallback())
	}
	info, _ := coreTheme.PackageInfoOf(e.ActiveTheme())
	if info.Source != coreTheme.PackageSourceInstalled || info.Digest != pkg.Digest {
		t.Fatalf("package info = %+v", info)
	}
	if !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatal("home is not the installed package's")
	}

	restarted := site.boot(t, nil)
	if restarted.ActiveThemeName() != samplePackageSlug || restarted.ThemeFallback() != nil || len(restarted.ThemeLoadIssues()) != 0 {
		t.Fatalf("after restart: active %q, fallback %+v, issues %+v", restarted.ActiveThemeName(), restarted.ThemeFallback(), restarted.ThemeLoadIssues())
	}
	if packageStatus(t, restarted, pkg.ID) != extension.StatusReady {
		t.Fatal("a sound package changed status")
	}
}

func TestPGDamagedArchivesAreMarkedBrokenAndRecover(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	site := newPackageSite(t)
	var old, cur extension.Package
	e := site.boot(t, func(e *Engine) {
		old = install(t, e, packageVersion(t, "1.0.0"), false)
		cur = install(t, e, packageVersion(t, "1.1.0"), true)
		activate(samplePackageSlug)(e)
	})
	if e.ActiveThemeName() != samplePackageSlug {
		t.Fatalf("active = %q", e.ActiveThemeName())
	}
	oldPath, curPath := archivePath(t, e, old), archivePath(t, e, cur)
	saved := filepath.Join(t.TempDir(), "saved")
	if err := os.MkdirAll(saved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{oldPath, curPath} {
		if err := os.Rename(p, filepath.Join(saved, filepath.Base(p))); err != nil {
			t.Fatal(err)
		}
	}

	// Both archives are gone: the site starts on another theme.
	e = site.boot(t, nil)
	if fb := e.ThemeFallback(); fb == nil || fb.Requested != samplePackageSlug || fb.Used != "aa-good" {
		t.Fatalf("fallback = %+v", fb)
	}
	if packageStatus(t, e, cur.ID) != extension.StatusBroken || packageStatus(t, e, old.ID) != extension.StatusBroken {
		t.Fatal("missing archives were not marked broken")
	}
	if issues := e.ThemeLoadIssues(); len(issues) != 1 || issues[0].Slug != samplePackageSlug {
		t.Fatalf("issues = %+v", issues)
	}

	// Restored from a backup: the next start uses the package again.
	for _, p := range []string{oldPath, curPath} {
		if err := os.Rename(filepath.Join(saved, filepath.Base(p)), p); err != nil {
			t.Fatal(err)
		}
	}
	e = site.boot(t, nil)
	if e.ActiveThemeName() != samplePackageSlug || e.ThemeFallback() != nil {
		t.Fatalf("after restore: active %q, fallback %+v", e.ActiveThemeName(), e.ThemeFallback())
	}
	if packageStatus(t, e, cur.ID) != extension.StatusReady || packageStatus(t, e, old.ID) != extension.StatusReady {
		t.Fatal("restored archives were not marked ready")
	}

	// Replaced content (another package under the same name) is broken too.
	var other bytes.Buffer
	if _, err := themepkg.Build(&other, os.DirFS(packageVersion(t, "9.9.9")), themepkg.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(curPath, other.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	e = site.boot(t, nil)
	if packageStatus(t, e, cur.ID) != extension.StatusBroken || e.ThemeFallback() == nil {
		t.Fatal("a changed archive was used")
	}
}

func TestPGIncompatibleAndRevokedPackagesKeepTheirStatus(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	site := newPackageSite(t)
	future := packageVersion(t, "2.0.0")
	setThemeTOML(t, future, `core = ">=0.7.0"`, `core = ">=99.0.0"`)
	var pkg extension.Package
	e := site.boot(t, func(e *Engine) {
		// Built without a core version, as on a developer's machine; this
		// core does not satisfy it.
		pkg = install(t, e, future, true)
	})
	if _, ok := e.lookupTheme(samplePackageSlug); ok {
		t.Fatal("an incompatible package was loaded")
	}
	if packageStatus(t, e, pkg.ID) != extension.StatusReady {
		t.Fatal("an incompatible package was marked broken")
	}
	issues := e.ThemeLoadIssues()
	if len(issues) != 1 || !strings.Contains(issues[0].Reason, themepkg.RuleManifestCoreUnmet) {
		t.Fatalf("issues = %+v", issues)
	}

	other := newPackageSite(t)
	var revoked extension.Package
	e = other.boot(t, func(e *Engine) {
		revoked = install(t, e, packageVersion(t, "1.0.0"), true)
		if err := e.ExtStore.SetStatus(context.Background(), revoked.ID, extension.StatusRevoked); err != nil {
			t.Fatal(err)
		}
		activate(samplePackageSlug)(e)
	})
	if e.ActiveThemeName() != samplePackageSlug {
		t.Fatalf("a revoked current package must still start: active %q", e.ActiveThemeName())
	}
	if packageStatus(t, e, revoked.ID) != extension.StatusRevoked {
		t.Fatal("revocation was undone")
	}
}

func TestPGUnrecordedArchivesStayAndStaleWritesGo(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	site := newPackageSite(t)
	var stray, stale string
	e := site.boot(t, func(e *Engine) {
		var buf bytes.Buffer
		report, err := themepkg.Build(&buf, os.DirFS(packageVersion(t, "1.0.0")), themepkg.Options{})
		if err != nil {
			t.Fatal(err)
		}
		// An install that stopped after writing its archive.
		stray, _, err = e.PackageArchives.Put(report.ID.Slug, report.ID.Version, report.Digest, buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		stale = filepath.Join(filepath.Dir(stray), ".tmp-interrupted")
		if err := os.WriteFile(stale, []byte("partial"), 0o640); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(stale, past, past); err != nil {
			t.Fatal(err)
		}
	})
	if _, ok := e.lookupTheme(samplePackageSlug); ok {
		t.Fatal("an unrecorded archive was loaded")
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("an unrecorded archive was removed")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale temporary file was kept")
	}
}

func TestPGPreviewReplacesInstalledPackage(t *testing.T) {
	site := newPackageSite(t)
	e := site.boot(t, func(e *Engine) {
		install(t, e, packageVersion(t, "1.0.0"), true)
		activate(samplePackageSlug)(e)
	}, packageVersion(t, "1.5.0"))
	info, _ := coreTheme.PackageInfoOf(e.ActiveTheme())
	if info.Source != coreTheme.PackageSourcePreview || info.Version != "1.5.0" {
		t.Fatalf("active package = %+v", info)
	}
	// Reloading leaves the preview alone.
	if err := e.ReloadThemePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if info, _ := coreTheme.PackageInfoOf(e.ActiveTheme()); info.Source != coreTheme.PackageSourcePreview {
		t.Fatal("reload replaced the preview")
	}
}

func TestPGReloadThemePackagesFollowsInstalls(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	site := newPackageSite(t)
	e := site.boot(t, activate("aa-good"))
	ctx := context.Background()

	// Install: the theme becomes available without a restart.
	v1 := install(t, e, packageVersion(t, "1.0.0"), true)
	if err := e.ReloadThemePackages(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.lookupTheme(samplePackageSlug); !ok {
		t.Fatal("installed package not added")
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatal("switched theme does not serve")
	}

	// Upgrade the active theme: the new version serves at once.
	v2 := install(t, e, packageVersion(t, "2.0.0"), true)
	if err := e.ReloadThemePackages(ctx); err != nil {
		t.Fatal(err)
	}
	if info, _ := coreTheme.PackageInfoOf(e.ActiveTheme()); info.Digest != v2.Digest || !strings.Contains(homeBody(t, e), "home 2.0.0") {
		t.Fatalf("after upgrade: %+v", info)
	}

	// Roll back.
	if err := e.ExtStore.SetCurrent(ctx, v1.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.ReloadThemePackages(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatal("rollback did not take effect")
	}

	// Concurrent reloads with nothing to do are harmless and keep the
	// running instance.
	before := e.ActiveTheme()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.ReloadThemePackages(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if e.ActiveTheme() != before {
		t.Fatal("a reload with nothing to do replaced the running theme")
	}

	// Uninstall while active: it keeps running and the issue is reported.
	if err := e.ExtStore.Delete(ctx, v1.ID, v2.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.ReloadThemePackages(ctx); err != nil {
		t.Fatal(err)
	}
	if e.ActiveThemeName() != samplePackageSlug || !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatal("the active theme stopped serving")
	}
	if issues := e.ThemeLoadIssues(); len(issues) != 1 || !strings.Contains(issues[0].Reason, "active theme") {
		t.Fatalf("issues = %+v", issues)
	}

	// Once switched away, the next reload removes it.
	if err := e.SwitchTheme("aa-good"); err != nil {
		t.Fatal(err)
	}
	if err := e.ReloadThemePackages(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.lookupTheme(samplePackageSlug); ok || len(e.ThemeLoadIssues()) != 0 {
		t.Fatalf("uninstalled package still loaded, issues %+v", e.ThemeLoadIssues())
	}
}
