// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"html/template"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/menu"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// menuLocationTheme declares locations only in its theme.toml model and may
// add runtime-only ones from Setup.
type menuLocationTheme struct {
	cfg          *coreTheme.FileConfig
	setupExtras  []string
	setupInvoked bool
}

func (t *menuLocationTheme) Name() string                    { return "menu-location-test" }
func (t *menuLocationTheme) Version() string                 { return "1.0.0" }
func (t *menuLocationTheme) Description() string             { return "" }
func (t *menuLocationTheme) Author() string                  { return "" }
func (t *menuLocationTheme) ServeHTTP(*gin.Context)          {}
func (t *menuLocationTheme) TemplateFuncs() template.FuncMap { return nil }
func (t *menuLocationTheme) TemplateDir() string             { return "" }
func (t *menuLocationTheme) StaticDir() string               { return "" }
func (t *menuLocationTheme) FileConfig() *coreTheme.FileConfig {
	return t.cfg
}
func (t *menuLocationTheme) Setup(app coreTheme.App) {
	t.setupInvoked = true
	for _, name := range t.setupExtras {
		app.MenuStore().RegisterLocation(name, name)
	}
}

func menuLocationLabels(e *Engine) map[string]string {
	labels := map[string]string{}
	for _, loc := range e.Menus.GetLocations() {
		labels[loc.Name] = loc.Label
	}
	return labels
}

func menuLocationOrder(e *Engine) []string {
	var names []string
	for _, loc := range e.Menus.GetLocations() {
		names = append(names, loc.Name)
	}
	return names
}

// A theme that declares [[menu_locations]] only in theme.toml (and registers
// nothing in Setup) must still offer them in the menu manager, and switching
// themes must not leave the previous theme's locations behind.
func TestThemeActivationRegistersMenuLocationsFromThemeTOML(t *testing.T) {
	tomlOnly := &menuLocationTheme{cfg: &coreTheme.FileConfig{MenuLocations: []coreTheme.MenuLocationConfig{
		{Name: "header", Label: "顶部导航"},
		{Name: "footer"},
		{Name: "  "},
	}}}
	withRuntime := &menuLocationTheme{
		cfg:         &coreTheme.FileConfig{MenuLocations: []coreTheme.MenuLocationConfig{{Name: "header", Label: "Header"}}},
		setupExtras: []string{"sidebar"},
	}
	e := &Engine{
		Registry: content.NewRegistry(),
		Menus:    menu.NewStore(nil),
		Hooks:    hook.New(),
		themes:   map[string]coreTheme.Theme{"toml-only": tomlOnly, "with-runtime": withRuntime},
	}

	if err := e.activateTheme("toml-only"); err != nil {
		t.Fatal(err)
	}
	if !tomlOnly.setupInvoked {
		t.Fatal("Setup was not called")
	}
	if got, want := menuLocationOrder(e), []string{"header", "footer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("theme.toml locations = %v, want %v (declaration order, blank names skipped)", got, want)
	}
	if got := menuLocationLabels(e); got["header"] != "顶部导航" || got["footer"] != "footer" {
		t.Fatalf("labels = %v, want declared label and name fallback", got)
	}

	if err := e.activateTheme("with-runtime"); err != nil {
		t.Fatal(err)
	}
	if got, want := menuLocationOrder(e), []string{"header", "sidebar"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after switch, locations = %v, want %v (footer from the old theme dropped, Setup extra kept)", got, want)
	}

	if err := e.activateTheme("toml-only"); err != nil {
		t.Fatal(err)
	}
	if got, want := menuLocationOrder(e), []string{"header", "footer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after switching back, locations = %v, want %v", got, want)
	}
}
