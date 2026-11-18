// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/option"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// settingsTheme declares settings in its theme.toml.
func settingsTheme(t *testing.T, name, toml string) *activationTheme {
	t.Helper()
	cfg, err := coreTheme.ParseFileConfig(toml)
	if err != nil {
		t.Fatal(err)
	}
	return &activationTheme{name: name, cfg: cfg}
}

const declaredSettingsTOML = `
[[settings]]
key = "home_title"
type = "string"
translatable = true

[[settings]]
key = "home_subtitle"
type = "text"
translatable = true

[[settings]]
key = "home_count"
type = "int"
`

func TestActivationRegistersDeclaredTranslatableSettings(t *testing.T) {
	declared := settingsTheme(t, "declared", declaredSettingsTOML)
	// The theme also registers one key in Go and one the declaration has.
	declared.setup = func(coreTheme.App) {
		option.RegisterTranslatable("footer_note", "footer", "Footer note")
		option.RegisterTranslatable("home_title", "hero", "Title")
	}
	registerTestThemes(t, map[string]ThemeFactoryE{
		"declared": builds(declared),
		"plain":    builds(okTheme("plain")),
	})
	e := themeSetEngine(t, "declared", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tk := range option.AllTranslatableOptions() {
		if tk.Key == "home_title" {
			count++
		}
	}
	// home_subtitle is only declared, so core registered it.
	if count != 1 || !option.IsTranslatable("home_subtitle") || !option.IsTranslatable("footer_note") || option.IsTranslatable("home_count") {
		t.Fatalf("translatable keys = %+v", option.AllTranslatableOptions())
	}

	if err := e.SwitchTheme("plain"); err != nil {
		t.Fatal(err)
	}
	if option.IsTranslatable("home_title") || option.IsTranslatable("footer_note") {
		t.Fatal("the previous theme's translatable settings lingered after switching")
	}
}

func TestActivationRejectsAnInvalidSettingsDeclaration(t *testing.T) {
	registerTestThemes(t, map[string]ThemeFactoryE{
		"good":   builds(settingsTheme(t, "good", declaredSettingsTOML)),
		"broken": builds(settingsTheme(t, "broken", "[[settings]]\nkey = \"plugin_shop_currency\"\ntype = \"string\"\n")),
	})
	e := themeSetEngine(t, "good", "")
	if err := e.LoadAllThemes(); err != nil {
		t.Fatal(err)
	}
	err := e.SwitchTheme("broken")
	if err == nil || !strings.Contains(err.Error(), "invalid settings declaration") || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("switch error = %v", err)
	}
	if e.ActiveThemeName() != "good" || !option.IsTranslatable("home_title") {
		t.Fatalf("active = %q; the current theme and its settings must stay in place", e.ActiveThemeName())
	}

	// The admin sees the error and still gets a settings page for it.
	if schema, err := e.ThemeSettingsSchema("broken"); schema != nil || err == nil {
		t.Fatalf("broken schema = %v, %v", schema, err)
	}
	if !e.themeDeclaresSettings("broken") || !e.themeDeclaresSettings("good") || e.themeDeclaresSettings("missing") {
		t.Fatal("themeDeclaresSettings is wrong")
	}
	if schema, err := e.ThemeSettingsSchema("good"); err != nil || schema == nil || len(schema.Fields) != 3 {
		t.Fatalf("good schema = %v, %v", schema, err)
	}
}
