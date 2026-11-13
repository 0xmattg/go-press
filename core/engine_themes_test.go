// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/cache"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/user"
)

// themeSetEngine returns an engine that can activate and switch themes and
// rebuild its router, with active_theme and site.theme as given.
func themeSetEngine(t *testing.T, activeTheme, siteTheme string) *Engine {
	t.Helper()
	option.ClearTranslatableOptions()
	t.Cleanup(func() {
		option.DiscardThemeTranslatables()
		option.ClearTranslatableOptions()
	})
	registry := content.NewRegistry()
	options := map[string]string{}
	if activeTheme != "" {
		options["active_theme"] = activeTheme
	}
	e := &Engine{
		Config:        &config.Config{Site: config.SiteConfig{Theme: siteTheme}},
		Hooks:         hook.New(),
		Cache:         cache.NewNoopManager(),
		Registry:      registry,
		Menus:         menu.NewStore(nil),
		RBAC:          user.NewRBAC(),
		I18n:          coreI18n.NewManager("en"),
		Options:       option.NewMemoryStore(options),
		Auth:          user.NewAuth("theme-set-test-secret", 1, nil),
		Sitemap:       rewrite.NewSitemapGenerator("https://example.test", registry, content.NewRepository(nil), rewrite.NewEngine(registry)),
		PluginManager: plugin.NewManager(),
		themes:        map[string]coreTheme.Theme{},
	}
	t.Cleanup(func() {
		if e.apiLimiter != nil {
			e.apiLimiter.Stop()
		}
	})
	return e
}

// okTheme is a theme that activates cleanly and declares one content type.
func okTheme(name string, types ...string) *activationTheme {
	cfg := &coreTheme.FileConfig{}
	for _, typ := range types {
		cfg.ContentTypes = append(cfg.ContentTypes, coreTheme.ContentTypeConfig{Name: typ, HasArchive: true})
	}
	return &activationTheme{name: name, cfg: cfg}
}

// panickyTheme builds fine but panics in Setup, so activating it fails.
func panickyTheme(name string) *activationTheme {
	return &activationTheme{name: name, cfg: &coreTheme.FileConfig{}, setup: func(coreTheme.App) { panic("setup bug") }}
}

// registerTestThemes registers error-returning factories for the test and
// removes them afterwards.
func registerTestThemes(t *testing.T, factories map[string]ThemeFactoryE) {
	t.Helper()
	for slug, factory := range factories {
		RegisterThemeE(slug, ThemeSource{}, factory)
		slug := slug
		t.Cleanup(func() { unregisterTheme(slug) })
	}
}

func builds(theme coreTheme.Theme) ThemeFactoryE {
	return func(*Engine, string) (coreTheme.Theme, error) { return theme, nil }
}

func TestLoadAllThemesSkipsThemesThatFailToBuild(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{
		"aa-good": builds(okTheme("aa-good")),
		"bb-error": func(*Engine, string) (coreTheme.Theme, error) {
			return nil, errors.New("templates/home.tmpl: bad action")
		},
		"cc-nil":    func(*Engine, string) (coreTheme.Theme, error) { return nil, nil },
		"dd-legacy": nil, // replaced below with an old-style panicking factory
	})
	RegisterTheme("dd-legacy", func(*Engine, string) coreTheme.Theme { panic("template parse failed") })

	e := themeSetEngine(t, "aa-good", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	if e.ActiveThemeName() != "aa-good" || e.ThemeFallback() != nil {
		t.Fatalf("active = %q, fallback = %+v", e.ActiveThemeName(), e.ThemeFallback())
	}
	issues := e.ThemeLoadIssues()
	want := map[string]string{"bb-error": "bad action", "cc-nil": "returned no theme", "dd-legacy": "panicked: template parse failed"}
	if len(issues) != len(want) {
		t.Fatalf("issues = %+v", issues)
	}
	for _, issue := range issues {
		if !strings.Contains(issue.Reason, want[issue.Slug]) {
			t.Errorf("%s: reason %q does not mention %q", issue.Slug, issue.Reason, want[issue.Slug])
		}
	}
	if infos := e.AvailableThemes(); len(infos) != 1 || infos[0].Slug != "aa-good" {
		t.Fatalf("AvailableThemes = %+v", infos)
	}
	// The old accessor keeps its panicking contract.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("AllThemeFactories()[dd-legacy] did not panic")
			}
		}()
		AllThemeFactories()["dd-legacy"](e, "")
	}()
}

func TestLoadAllThemesFallsBack(t *testing.T) {
	cases := []struct {
		name               string
		active, site       string
		wantUsed           string
		wantFallback       bool
		wantReasonContains string
	}{
		{name: "configured theme works", active: "beta", site: "gamma", wantUsed: "beta"},
		{name: "site.theme when active_theme is set but missing", active: "missing", site: "gamma", wantUsed: "gamma", wantFallback: true, wantReasonContains: "not found"},
		{name: "first theme that activates", active: "missing", site: "alpha", wantUsed: "beta", wantFallback: true, wantReasonContains: "not found"},
		{name: "active theme failed to build", active: "broken", site: "", wantUsed: "beta", wantFallback: true, wantReasonContains: "factory failed"},
		{name: "active theme fails to activate", active: "alpha", site: "", wantUsed: "beta", wantFallback: true, wantReasonContains: "setup bug"},
		{name: "nothing configured", active: "", site: "", wantUsed: "beta", wantFallback: true, wantReasonContains: "no theme is configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registerTestThemes(t, map[string]ThemeFactoryE{
				"alpha":  builds(panickyTheme("alpha")),
				"beta":   builds(okTheme("beta")),
				"gamma":  builds(okTheme("gamma")),
				"broken": func(*Engine, string) (coreTheme.Theme, error) { return nil, errors.New("boom") },
			})
			e := themeSetEngine(t, tc.active, tc.site)
			if err := e.LoadAllThemes(); err != nil {
				t.Fatal(err)
			}
			if e.ActiveThemeName() != tc.wantUsed || e.ActiveTheme() == nil {
				t.Fatalf("active = %q, want %q", e.ActiveThemeName(), tc.wantUsed)
			}
			fallback := e.ThemeFallback()
			if (fallback != nil) != tc.wantFallback {
				t.Fatalf("fallback = %+v, want %v", fallback, tc.wantFallback)
			}
			if fallback != nil {
				requested := tc.active
				if requested == "" {
					requested = tc.site
				}
				if fallback.Requested != requested || fallback.Used != tc.wantUsed || !strings.Contains(fallback.Reason, tc.wantReasonContains) {
					t.Fatalf("fallback = %+v", fallback)
				}
			}
			if got := e.Options.Get("active_theme"); got != tc.active {
				t.Fatalf("active_theme rewritten to %q", got)
			}
		})
	}
}

func TestLoadAllThemesFailsWhenNoThemeActivates(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{
		"alpha":  builds(panickyTheme("alpha")),
		"broken": func(*Engine, string) (coreTheme.Theme, error) { return nil, errors.New("boom") },
	})
	e := themeSetEngine(t, "broken", "")
	err := e.LoadAllThemes()
	if err == nil || !strings.Contains(err.Error(), "broken: ") || !strings.Contains(err.Error(), "alpha: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestReconcileLeavesPluginsOffDuringFallback(t *testing.T) {
	e := themeSetEngine(t, "", "")
	e.PluginManager = newManagerWith(nil, map[string]string{"needed-plugin": "1.0.0"})
	e.themes = map[string]coreTheme.Theme{
		"th": fakeTheme{name: "th", req: coreTheme.RequiresConfig{Plugins: []coreTheme.PluginRequirement{{Slug: "needed-plugin"}}}},
	}
	e.activeThemeName = "th"

	e.themeFallback = &ThemeFallback{Requested: "saved", Used: "th"}
	e.ReconcileActiveThemeDependencies()
	if e.PluginManager.IsActive("needed-plugin") || e.Options.Get("plugin_active_needed-plugin") != "" {
		t.Fatal("a fallback theme enabled and persisted a plugin")
	}

	e.themeFallback = nil
	e.ReconcileActiveThemeDependencies()
	if !e.PluginManager.IsActive("needed-plugin") || e.Options.Get("plugin_active_needed-plugin") != "true" {
		t.Fatal("the configured theme's dependency was not enabled")
	}
}

func TestSwitchThemeEndsFallback(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"beta": builds(okTheme("beta")), "gamma": builds(okTheme("gamma"))})
	e := themeSetEngine(t, "missing", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	if e.ThemeFallback() == nil {
		t.Fatal("expected a fallback")
	}
	if err := e.SwitchTheme("gamma"); err != nil {
		t.Fatal(err)
	}
	if e.ThemeFallback() != nil || e.Options.Get("active_theme") != "gamma" {
		t.Fatalf("fallback = %+v, active_theme = %q", e.ThemeFallback(), e.Options.Get("active_theme"))
	}
}

func TestRuntimeThemeAPI(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"built-in": builds(okTheme("built-in", "page_extra"))})
	RegisterPlugin("plugin-name", func(*Engine) {})
	t.Cleanup(func() {
		pluginRegistryMu.Lock()
		delete(pluginRegistry, "plugin-name")
		pluginRegistryMu.Unlock()
	})
	e := themeSetEngine(t, "built-in", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}

	for slug, theme := range map[string]coreTheme.Theme{
		"Bad_Slug":    okTheme("x"),
		"built-in":    okTheme("x"),
		"plugin-name": okTheme("x"),
		"no-theme":    nil,
	} {
		if err := e.AddTheme(slug, theme); err == nil {
			t.Errorf("AddTheme(%q) accepted", slug)
		}
	}
	pkgA := okTheme("pkg-a", "listing")
	if err := e.AddTheme("pkg-a", pkgA); err != nil {
		t.Fatal(err)
	}
	if err := e.AddTheme("pkg-a", okTheme("pkg-a")); err == nil {
		t.Fatal("AddTheme added the same slug twice")
	}
	if err := e.SwitchTheme("pkg-a"); err != nil || e.ActiveTheme() != coreTheme.Theme(pkgA) {
		t.Fatalf("switch to an added theme: %v", err)
	}

	// Remove: only themes added at runtime, never the active one.
	for _, slug := range []string{"pkg-a", "built-in", "never-added"} {
		if err := e.RemoveTheme(slug); err == nil {
			t.Errorf("RemoveTheme(%q) succeeded", slug)
		}
	}

	// Replace an inactive theme.
	if err := e.AddTheme("pkg-b", okTheme("pkg-b")); err != nil {
		t.Fatal(err)
	}
	pkgB2 := okTheme("pkg-b")
	if err := e.ReplaceTheme("pkg-b", pkgB2); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.lookupTheme("pkg-b"); got != coreTheme.Theme(pkgB2) {
		t.Fatal("ReplaceTheme did not swap the inactive theme")
	}
	if err := e.ReplaceTheme("built-in", okTheme("x")); err == nil {
		t.Fatal("ReplaceTheme replaced a built-in theme")
	}
	if err := e.ReplaceTheme("never-added", okTheme("x")); err == nil {
		t.Fatal("ReplaceTheme accepted an unknown theme")
	}

	// Replacing the active theme with a broken build keeps the old one.
	registry := e.Registry
	if err := e.ReplaceTheme("pkg-a", panickyTheme("pkg-a")); err == nil {
		t.Fatal("ReplaceTheme activated a broken theme")
	}
	if e.ActiveTheme() != coreTheme.Theme(pkgA) || e.Registry != registry || e.Registry.GetType("listing") == nil {
		t.Fatal("a failed replacement changed the active theme")
	}
	// A good build replaces it and its types.
	pkgA2 := okTheme("pkg-a", "catalog")
	if err := e.ReplaceTheme("pkg-a", pkgA2); err != nil {
		t.Fatal(err)
	}
	if e.ActiveTheme() != coreTheme.Theme(pkgA2) || e.Registry.GetType("catalog") == nil || e.Registry.GetType("listing") != nil {
		t.Fatal("the replacement is not active")
	}

	// After switching away, the theme can be removed.
	if err := e.SwitchTheme("built-in"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveTheme("pkg-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.lookupTheme("pkg-a"); ok {
		t.Fatal("pkg-a still loaded")
	}
}

// Readers see a consistent theme set while it changes: the active theme is
// never nil and always matches the active name.
func TestThemeSetConcurrentReadsAndWrites(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{"built-in": builds(okTheme("built-in"))})
	e := themeSetEngine(t, "built-in", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	if err := e.AddTheme("pkg-live", okTheme("pkg-live")); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var bad atomic.Int32
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				if e.ActiveTheme() == nil {
					bad.Add(1)
				}
				_ = e.AvailableThemes()
				_ = e.ThemeDependencies(e.ActiveThemeName())
				_ = e.themeFS("pkg-live")
				_ = e.ThemeLoadIssues()
			}
		}()
	}
	for i := 0; i < 100; i++ {
		target := "built-in"
		if i%2 == 1 {
			target = "pkg-live"
		}
		e.lifecycleMu.Lock()
		err := e.activateTheme(target)
		e.lifecycleMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if err := e.AddTheme("pkg-temp", okTheme("pkg-temp")); err != nil {
			t.Fatal(err)
		}
		if err := e.ReplaceTheme("pkg-temp", okTheme("pkg-temp")); err != nil {
			t.Fatal(err)
		}
		if err := e.RemoveTheme("pkg-temp"); err != nil {
			t.Fatal(err)
		}
	}
	stop.Store(true)
	readers.Wait()
	if bad.Load() != 0 {
		t.Fatalf("ActiveTheme returned nil %d times", bad.Load())
	}
}
