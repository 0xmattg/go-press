// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"html/template"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/user"
)

// activationTheme is a test theme whose theme.toml model, Setup behavior, and
// files are configurable.
type activationTheme struct {
	name  string
	cfg   *coreTheme.FileConfig
	setup func(app coreTheme.App)
	files fstest.MapFS
}

func (t *activationTheme) Name() string                      { return t.name }
func (t *activationTheme) Version() string                   { return "1.0.0" }
func (t *activationTheme) Description() string               { return "" }
func (t *activationTheme) Author() string                    { return "" }
func (t *activationTheme) ServeHTTP(*gin.Context)            {}
func (t *activationTheme) TemplateFuncs() template.FuncMap   { return nil }
func (t *activationTheme) TemplateDir() string               { return "" }
func (t *activationTheme) StaticDir() string                 { return "" }
func (t *activationTheme) FileConfig() *coreTheme.FileConfig { return t.cfg }
func (t *activationTheme) AssetFS() fs.FS                    { return t.files }
func (t *activationTheme) Setup(app coreTheme.App) {
	if t.setup != nil {
		t.setup(app)
	}
}

func submissionType(name string) coreTheme.ContentTypeConfig {
	return coreTheme.ContentTypeConfig{
		Name:             name,
		HasArchive:       true,
		PublicSubmission: coreTheme.PublicSubmissionConfig{Enabled: true, Roles: []string{user.RoleSubscriber}},
	}
}

// twoThemes returns an engine with themes "alpha" and "beta". Both declare a
// "question" type open to subscribers, so its capabilities must never drop
// during a switch; each also has a type, a menu location, translatable keys,
// and a locale message of its own.
func twoThemes(t *testing.T) *Engine {
	t.Helper()
	option.ClearTranslatableOptions()
	t.Cleanup(func() {
		option.DiscardThemeTranslatables()
		option.ClearTranslatableOptions()
	})
	alpha := &activationTheme{
		name: "alpha",
		cfg: &coreTheme.FileConfig{
			ContentTypes:  []coreTheme.ContentTypeConfig{submissionType("question"), {Name: "showcase", HasArchive: true}},
			MenuLocations: []coreTheme.MenuLocationConfig{{Name: "header", Label: "Header"}},
		},
		setup: func(app coreTheme.App) {
			app.MenuStore().RegisterLocation("alpha-sidebar", "Sidebar")
			option.RegisterTranslatable("alpha_title", "home", "Alpha title")
		},
		files: fstest.MapFS{"locales/en.json": {Data: []byte(`{"greeting":"Hello from alpha"}`)}},
	}
	beta := &activationTheme{
		name: "beta",
		cfg: &coreTheme.FileConfig{
			ContentTypes:  []coreTheme.ContentTypeConfig{submissionType("question"), {Name: "listing", HasArchive: true}},
			MenuLocations: []coreTheme.MenuLocationConfig{{Name: "top-nav", Label: "Top"}},
		},
		setup: func(app coreTheme.App) {
			option.RegisterTranslatable("beta_title", "home", "Beta title")
		},
		files: fstest.MapFS{"locales/en.json": {Data: []byte(`{"greeting":"Hello from beta"}`)}},
	}
	return &Engine{
		Registry: content.NewRegistry(),
		Menus:    menu.NewStore(nil),
		Hooks:    hook.New(),
		RBAC:     user.NewRBAC(),
		I18n:     coreI18n.NewManager("en"),
		themes:   map[string]coreTheme.Theme{"alpha": alpha, "beta": beta},
	}
}

// runtimeState captures everything a theme activation publishes.
type runtimeState struct {
	Theme        string
	Types        []string
	Locations    []string
	Translatable []string
	Greeting     string
	Caps         map[string]bool
}

func captureRuntime(e *Engine) runtimeState {
	var types []string
	for _, def := range e.Registry.AllTypes() {
		types = append(types, def.Name)
	}
	sort.Strings(types)
	var locations []string
	for _, loc := range e.Menus.GetLocations() {
		locations = append(locations, loc.Name)
	}
	var keys []string
	for _, key := range option.AllTranslatableOptions() {
		keys = append(keys, key.Key)
	}
	sort.Strings(keys)
	caps := map[string]bool{}
	for _, resource := range []string{"question", "showcase", "listing"} {
		caps[resource+".create"] = e.RBAC.Can(user.RoleSubscriber, resource, "create")
	}
	return runtimeState{
		Theme:        e.ActiveThemeName(),
		Types:        types,
		Locations:    locations,
		Translatable: keys,
		Greeting:     e.I18n.ThemeMessages()["en"]["greeting"],
		Caps:         caps,
	}
}

func TestThemeSwitchPublishesCompleteStateWithoutResidue(t *testing.T) {
	e := twoThemes(t)
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}
	alphaState := captureRuntime(e)
	want := runtimeState{
		Theme:        "alpha",
		Types:        []string{"contact_message", "page", "post", "question", "showcase"},
		Locations:    []string{"header", "alpha-sidebar"},
		Translatable: []string{"alpha_title"},
		Greeting:     "Hello from alpha",
		Caps:         map[string]bool{"question.create": true, "showcase.create": false, "listing.create": false},
	}
	if !reflect.DeepEqual(alphaState, want) {
		t.Fatalf("after alpha:\n got %+v\nwant %+v", alphaState, want)
	}

	if err := e.activateTheme("beta"); err != nil {
		t.Fatal(err)
	}
	betaState := captureRuntime(e)
	if betaState.Theme != "beta" || !reflect.DeepEqual(betaState.Locations, []string{"top-nav"}) ||
		!reflect.DeepEqual(betaState.Translatable, []string{"beta_title"}) || betaState.Greeting != "Hello from beta" ||
		strings.Contains(strings.Join(betaState.Types, ","), "showcase") || !betaState.Caps["question.create"] {
		t.Fatalf("after beta, alpha state lingered or beta state is missing: %+v", betaState)
	}

	// A → B → A ends exactly where A started.
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}
	if again := captureRuntime(e); !reflect.DeepEqual(again, alphaState) {
		t.Fatalf("A→B→A is not idempotent:\n got %+v\nwant %+v", again, alphaState)
	}
}

func TestFailedActivationLeavesRuntimeUntouched(t *testing.T) {
	e := twoThemes(t)
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}
	before := captureRuntime(e)
	registry := e.Registry

	e.themes["panics"] = &activationTheme{
		name: "panics",
		cfg:  &coreTheme.FileConfig{MenuLocations: []coreTheme.MenuLocationConfig{{Name: "broken"}}},
		setup: func(app coreTheme.App) {
			app.MenuStore().RegisterLocation("half-done", "Half")
			option.RegisterTranslatable("half_done", "x", "Half")
			panic("theme bug")
		},
	}
	// No FileConfig and no theme.toml on disk: the config cannot be loaded.
	e.themes["no-config"] = &activationTheme{name: "no-config", files: fstest.MapFS{}}

	for _, name := range []string{"panics", "no-config", "missing"} {
		err := e.activateTheme(name)
		if err == nil {
			t.Fatalf("activating %q succeeded", name)
		}
		if after := captureRuntime(e); !reflect.DeepEqual(after, before) {
			t.Fatalf("activating %q (%v) changed the runtime:\n got %+v\nwant %+v", name, err, after, before)
		}
	}
	if e.Registry != registry {
		t.Fatal("the registry pointer must never change")
	}
	// The site still switches normally afterwards.
	if err := e.activateTheme("beta"); err != nil || e.ActiveThemeName() != "beta" {
		t.Fatalf("switch after failures: %v", err)
	}
}

func TestPluginContentTypesFollowTheRegisterTypesHook(t *testing.T) {
	e := twoThemes(t)
	handle := e.Hooks.AddAction(hook.ContentRegisterTypes, func(_ context.Context, args ...interface{}) {
		args[0].(*content.Registry).RegisterType(content.ContentTypeDef{Name: "product"})
	}, 10)
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}
	if e.Registry.GetType("product") == nil {
		t.Fatal("type registered through the hook argument is missing")
	}
	// Deactivating the plugin removes its hook; the next refresh drops the type.
	e.Hooks.RemoveAction(handle)
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}
	if e.Registry.GetType("product") != nil {
		t.Fatal("type of a removed plugin survived the refresh")
	}
}

// TestConcurrentReadersNeverSeeHalfBuiltState switches themes repeatedly while
// readers check every published piece of state. Run with -race.
func TestConcurrentReadersNeverSeeHalfBuiltState(t *testing.T) {
	e := twoThemes(t)
	if err := e.activateTheme("alpha"); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var failures atomic.Int64
	var firstFailure atomic.Value
	fail := func(msg string) {
		if failures.Add(1) == 1 {
			firstFailure.Store(msg)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if e.Registry.GetType("post") == nil || e.Registry.GetType("question") == nil {
					fail("core or shared type missing")
				}
				if len(e.Registry.AllTypes()) < 5 {
					fail("registry partially built")
				}
				if len(e.Menus.GetLocations()) == 0 {
					fail("menu locations empty")
				}
				if !e.RBAC.Can(user.RoleSubscriber, "question", "create") {
					fail("capability shared by both themes dropped")
				}
				if len(option.AllTranslatableOptions()) == 0 {
					fail("translatable keys empty")
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		name := "alpha"
		if i%2 == 0 {
			name = "beta"
		}
		if err := e.activateTheme(name); err != nil {
			t.Fatal(err)
		}
	}
	stop.Store(true)
	wg.Wait()
	if n := failures.Load(); n > 0 {
		t.Fatalf("%d inconsistent reads during switches; first: %v", n, firstFailure.Load())
	}
}
