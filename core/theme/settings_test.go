// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/option"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

const sampleSettingsTOML = `
[theme]
name = "Sample"

[[setting_groups]]
key = "hero"
label = "Hero"
label_key = "settings.group.hero"

[[setting_groups]]
key = "layout"
label = "Layout"

[[settings]]
key = "home_hero_title"
group = "hero"
type = "string"
label = "Hero title"
default = "Build faster"
translatable = true
max_length = 20

[[settings]]
key = "home_hero_{n}_image"
repeat = 3
group = "hero"
type = "image"
label = "Slide {n} image"

[[settings]]
key = "home_interval_ms"
group = "layout"
type = "int"
default = 5000
min = 1000
max = 20000

[[settings]]
key = "home_show_partners"
group = "layout"
type = "bool"
default = true

[[settings]]
key = "showcase_layout"
group = "layout"
type = "select"
default = "grid"
options = [{ value = "grid", label = "Grid" }, { value = "list", label = "List" }]

[[settings]]
key = "company_email"
type = "email"
required = true

[[settings]]
key = "site_name"
type = "string"
translatable = true
`

func sampleSettingsSchema(t *testing.T) *SettingsSchema {
	t.Helper()
	cfg, err := ParseFileConfig(sampleSettingsTOML)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := cfg.SettingsSchema()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestSettingsSchemaExpandsAndKeepsDeclarationOrder(t *testing.T) {
	schema := sampleSettingsSchema(t)
	var keys []string
	for _, field := range schema.Fields {
		keys = append(keys, field.Key)
	}
	want := []string{"home_hero_title", "home_hero_1_image", "home_hero_2_image", "home_hero_3_image",
		"home_interval_ms", "home_show_partners", "showcase_layout", "company_email", "site_name"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v", keys)
	}
	slide, _ := schema.Field("home_hero_2_image")
	if slide.Label != "Slide 2 image" || slide.N != 2 || slide.Group != "hero" || slide.MaxLength != defaultLineMaxLength {
		t.Fatalf("repeated field = %+v", slide)
	}
	interval, _ := schema.Field("home_interval_ms")
	if interval.Default != "5000" || *interval.Min != 1000 || interval.MaxLength != 0 {
		t.Fatalf("int field = %+v", interval)
	}
	if partners, _ := schema.Field("home_show_partners"); partners.Default != "1" {
		t.Fatalf("bool default = %q, want 1", partners.Default)
	}
	if title, _ := schema.Field("home_hero_title"); title.MaxLength != 20 || !title.Translatable {
		t.Fatalf("title = %+v", title)
	}
	if email, _ := schema.Field("company_email"); email.Label != "company_email" {
		t.Fatalf("a missing label falls back to the key, got %q", email.Label)
	}
	if _, ok := schema.Field("home_hero_{n}_image"); ok {
		t.Fatal("the unexpanded key must not be declared")
	}
	if len(schema.Groups) != 2 || schema.Groups[0].LabelKey != "settings.group.hero" {
		t.Fatalf("groups = %+v", schema.Groups)
	}
}

func TestNoSettingsDeclarationIsNotAnError(t *testing.T) {
	cfg, err := ParseFileConfig("[theme]\nname = \"Plain\"\n")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := cfg.SettingsSchema()
	if schema != nil || err != nil || cfg.ValidateSettings() != nil {
		t.Fatalf("schema = %v, err = %v", schema, err)
	}
	var nilConfig *FileConfig
	if schema, err := nilConfig.SettingsSchema(); schema != nil || err != nil {
		t.Fatal("a nil FileConfig declares nothing")
	}
}

func TestInvalidSettingsDeclarations(t *testing.T) {
	field := func(body string) string { return "[[settings]]\n" + body + "\n" }
	many := strings.Repeat(field("key = \"k{n}_x\"\ntype = \"string\"\nrepeat = 24"), 1)
	for i := 1; i <= 21; i++ {
		many += field(fmt.Sprintf("key = \"g%d_{n}\"\ntype = \"string\"\nrepeat = 24", i))
	}
	for _, tc := range []struct {
		name, toml, want string
	}{
		{"uppercase key", field(`key = "HomeTitle"` + "\ntype = \"string\""), "lowercase"},
		{"hyphen", field(`key = "home-title"` + "\ntype = \"string\""), "lowercase"},
		{"digit first", field(`key = "1home"` + "\ntype = \"string\""), "lowercase"},
		{"too long", field(`key = "` + strings.Repeat("a", 65) + `"` + "\ntype = \"string\""), "at most 64"},
		{"empty key", field(`type = "string"`), "lowercase"},
		{"duplicate", field("key = \"a\"\ntype = \"string\"") + field("key = \"a\"\ntype = \"text\""), "declared twice"},
		{"duplicate after repeat", field("key = \"a_{n}\"\ntype = \"string\"\nrepeat = 2") + field("key = \"a_2\"\ntype = \"string\""), "declared twice"},
		{"repeat without placeholder", field("key = \"a\"\ntype = \"string\"\nrepeat = 3"), `needs "{n}"`},
		{"placeholder without repeat", field("key = \"a_{n}\"\ntype = \"string\""), "repeat between 2 and 24"},
		{"repeat too large", field("key = \"a_{n}\"\ntype = \"string\"\nrepeat = 25"), "between 2 and 24"},
		{"missing type", field(`key = "a"`), "type is required"},
		{"unknown type", field("key = \"a\"\ntype = \"color\""), `unknown type "color"`},
		{"select without options", field("key = \"a\"\ntype = \"select\""), "need options"},
		{"repeated option", field("key = \"a\"\ntype = \"select\"\noptions = [{ value = \"x\" }, { value = \"x\" }]"), "empty or repeated"},
		{"options on string", field("key = \"a\"\ntype = \"string\"\noptions = [{ value = \"x\" }]"), "select settings only"},
		{"bad int default", field("key = \"a\"\ntype = \"int\"\ndefault = \"many\""), "default"},
		{"default outside options", field("key = \"a\"\ntype = \"select\"\ndefault = \"z\"\noptions = [{ value = \"x\" }]"), "default"},
		{"script url default", field("key = \"a\"\ntype = \"url\"\ndefault = \"javascript:alert(1)\""), "default"},
		{"default over range", field("key = \"a\"\ntype = \"int\"\ndefault = 9\nmax = 5"), "default"},
		{"min above max", field("key = \"a\"\ntype = \"int\"\nmin = 5\nmax = 1"), "min is greater than max"},
		{"range on string", field("key = \"a\"\ntype = \"string\"\nmin = 1"), "int settings only"},
		{"length on bool", field("key = \"a\"\ntype = \"bool\"\nmax_length = 4"), "does not apply"},
		{"negative length", field("key = \"a\"\ntype = \"string\"\nmax_length = -1"), "max_length"},
		{"translatable int", field("key = \"a\"\ntype = \"int\"\ntranslatable = true"), "cannot be translatable"},
		{"undeclared group", field("key = \"a\"\ntype = \"string\"\ngroup = \"hero\""), "not declared"},
		{"duplicate group", "[[setting_groups]]\nkey = \"g\"\n[[setting_groups]]\nkey = \"g\"\n" + field("key = \"a\"\ntype = \"string\""), "declared twice"},
		{"groups without settings", "[[setting_groups]]\nkey = \"g\"\n", "without [[settings]]"},
		{"too many", many, "more than 500"},
		{"plugin namespace", field("key = \"plugin_commerce_currency\"\ntype = \"string\""), "reserved"},
		{"market namespace", field("key = \"market_developer_mode\"\ntype = \"bool\""), "reserved"},
		{"demo marker", field("key = \"demo_imported_x\"\ntype = \"string\""), "reserved"},
		{"active theme", field("key = \"active_theme\"\ntype = \"string\""), "core setting"},
		{"registration role", field("key = \"new_user_default_role\"\ntype = \"string\""), "core setting"},
		{"repeated reserved", field("key = \"plugin_x_{n}\"\ntype = \"string\"\nrepeat = 2"), "reserved"},
	} {
		cfg, err := ParseFileConfig(tc.toml)
		if err != nil {
			t.Errorf("%s: parse: %v", tc.name, err)
			continue
		}
		err = cfg.ValidateSettings()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
}

func TestSettingFieldValidate(t *testing.T) {
	schema := sampleSettingsSchema(t)
	get := func(key string) SettingField {
		field, ok := schema.Field(key)
		if !ok {
			t.Fatalf("no field %s", key)
		}
		return field
	}
	for _, tc := range []struct {
		key, in, want, reason string
	}{
		{"home_hero_title", "  Hello  ", "  Hello  ", ""},
		{"home_hero_title", "", "", ""},
		{"home_hero_title", strings.Repeat("字", 20), strings.Repeat("字", 20), ""},
		{"home_hero_title", strings.Repeat("字", 21), "", SettingErrTooLong},
		{"home_interval_ms", " 2000 ", "2000", ""},
		{"home_interval_ms", "", "", ""},
		{"home_interval_ms", "500", "", SettingErrOutOfRange},
		{"home_interval_ms", "20001", "", SettingErrOutOfRange},
		{"home_interval_ms", "2.5", "", SettingErrNotInt},
		{"home_interval_ms", "99999999999", "", SettingErrNotInt},
		{"home_show_partners", "1", "1", ""},
		{"home_show_partners", "on", "1", ""},
		{"home_show_partners", "", "0", ""},
		{"home_show_partners", "false", "0", ""},
		{"home_show_partners", "maybe", "", SettingErrNotBool},
		{"showcase_layout", "list", "list", ""},
		{"showcase_layout", "", "", ""},
		{"showcase_layout", "masonry", "", SettingErrNotOption},
		{"company_email", "ops@example.com", "ops@example.com", ""},
		{"company_email", "", "", SettingErrRequired},
		{"company_email", "Ops <ops@example.com>", "", SettingErrBadEmail},
		{"company_email", "not-an-email", "", SettingErrBadEmail},
		{"home_hero_1_image", "/uploads/a.png", "/uploads/a.png", ""},
		{"home_hero_1_image", "https://cdn.example.com/a.png", "https://cdn.example.com/a.png", ""},
		{"home_hero_1_image", "//evil.example/a.png", "", SettingErrBadImage},
		{"home_hero_1_image", "javascript:alert(1)", "", SettingErrBadImage},
		{"home_hero_1_image", "data:image/png;base64,AAAA", "", SettingErrBadImage},
		{"home_hero_1_image", "#top", "", SettingErrBadImage},
		{"home_hero_1_image", "/a b.png", "", SettingErrBadImage},
	} {
		got, err := get(tc.key).Validate(tc.in)
		var valueErr *SettingValueError
		switch {
		case tc.reason == "" && (err != nil || got != tc.want):
			t.Errorf("%s(%q) = %q, %v; want %q", tc.key, tc.in, got, err, tc.want)
		case tc.reason != "" && (!errors.As(err, &valueErr) || valueErr.Reason != tc.reason || valueErr.Key != tc.key):
			t.Errorf("%s(%q) error = %v, want reason %s", tc.key, tc.in, err, tc.reason)
		}
	}

	link := SettingField{Key: "home_btn_link", Type: SettingURL, MaxLength: defaultLineMaxLength}
	for in, ok := range map[string]bool{
		"/contact": true, "https://example.com/x?y=1": true, "#pricing": true, "mailto:hi@example.com": true, "tel:+15551234": true,
		"//evil.example": false, "/\\evil": false, "javascript:alert(1)": false, "JaVaScRiPt:alert(1)": false,
		"vbscript:x": false, "ftp://example.com": false, "https://": false, "mailto:": false, "/a\nb": false, "example.com": false,
	} {
		if _, err := link.Validate(in); (err == nil) != ok {
			t.Errorf("url %q accepted = %v, want %v", in, err == nil, ok)
		}
	}
}

func TestApplyDefaultsFillsOnlyUnsavedKeys(t *testing.T) {
	schema := sampleSettingsSchema(t)
	got := schema.ApplyDefaults(map[string]string{"home_hero_title": "", "home_interval_ms": "3000", "other": "x"})
	want := map[string]string{
		"home_hero_title":    "", // cleared on purpose
		"home_interval_ms":   "3000",
		"home_show_partners": "1",
		"showcase_layout":    "grid",
		"other":              "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyDefaults = %v, want %v", got, want)
	}
	var none *SettingsSchema
	if got := none.ApplyDefaults(map[string]string{"a": "1"}); !reflect.DeepEqual(got, map[string]string{"a": "1"}) {
		t.Fatalf("a nil schema changed the settings: %v", got)
	}
}

func TestRegisterTranslatableSettings(t *testing.T) {
	option.ClearTranslatableOptions()
	t.Cleanup(option.ClearTranslatableOptions)
	RegisterTranslatableSettings(sampleSettingsSchema(t))
	keys := map[string]option.TranslatableKey{}
	for _, tk := range option.AllTranslatableOptions() {
		keys[tk.Key] = tk
	}
	if len(keys) != 2 || keys["home_hero_title"].Section != "hero" || keys["home_hero_title"].Label != "Hero title" {
		t.Fatalf("registered = %+v", keys)
	}
	if _, ok := keys["site_name"]; !ok {
		t.Fatal("site_name was not registered")
	}
	if option.IsTranslatable("home_interval_ms") {
		t.Fatal("a non-translatable setting was registered")
	}
}

// settingsTestApp adds an option store and an i18n manager to dataTestApp.
type settingsTestApp struct {
	*dataTestApp
	options *option.Store
	i18n    *coreI18n.Manager
}

func (a *settingsTestApp) OptionsStore() *option.Store    { return a.options }
func (a *settingsTestApp) I18nManager() *coreI18n.Manager { return a.i18n }

func TestTemplateSettingsFillDefaultsAndTranslate(t *testing.T) {
	option.ClearTranslatableOptions()
	t.Cleanup(option.ClearTranslatableOptions)
	cfg, err := ParseFileConfig(sampleSettingsTOML)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := cfg.SettingsSchema()
	RegisterTranslatableSettings(schema)

	mgr := coreI18n.NewManager("en")
	mgr.AddMessages("zh", []*goi18n.Message{{ID: coreI18n.OptMsgPrefix + "home_hero_title", Other: "更快构建"}})
	app := &settingsTestApp{
		dataTestApp: &dataTestApp{},
		options:     option.NewMemoryStore(map[string]string{"home_interval_ms": "3000", "showcase_layout": ""}),
		i18n:        mgr,
	}
	b := &BaseTheme{App: app, fileConfig: cfg}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(coreI18n.CtxKeyLocalizer, mgr.NewLocalizer("zh"))
	got := b.TemplateSettings(c)
	for key, want := range map[string]string{
		"home_hero_title":    "更快构建", // default, then translated
		"home_interval_ms":   "3000",
		"home_show_partners": "1",
		"showcase_layout":    "", // saved empty stays empty
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}

	en, _ := gin.CreateTestContext(httptest.NewRecorder())
	en.Set(coreI18n.CtxKeyLocalizer, mgr.NewLocalizer("en"))
	if got := b.TemplateSettings(en)["home_hero_title"]; got != "Build faster" {
		t.Fatalf("default language title = %q, want the declared default", got)
	}

	// buildBaseData uses the same view.
	data := b.buildBaseData(c, "Home")
	if settings, _ := data["Settings"].(map[string]string); settings["home_hero_title"] != "更快构建" {
		t.Fatalf("page Settings = %v", data["Settings"])
	}

	// Without a declaration nothing is added.
	plain := &BaseTheme{App: app}
	if _, ok := plain.TemplateSettings(c)["home_show_partners"]; ok {
		t.Fatal("a theme without [[settings]] got defaults")
	}
}
