// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/option"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

const schemaThemeTOML = `
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
label_key = "settings.home_hero_title"
help = "Shown above the fold."
default = "Build faster"
max_length = 40

[[settings]]
key = "home_hero_{n}_image"
repeat = 2
group = "hero"
type = "image"
label = "Slide {n} image"
label_key = "settings.slide_image"

[[settings]]
key = "home_interval_ms"
group = "layout"
type = "int"
label = "Interval"
default = 5000
min = 1000
max = 20000

[[settings]]
key = "home_show_partners"
group = "layout"
type = "bool"
label = "Show partners"
default = true

[[settings]]
key = "showcase_layout"
group = "layout"
type = "select"
label = "Showcase layout"
default = "grid"
options = [{ value = "grid", label = "Grid" }, { value = "list", label = "List", label_key = "settings.layout_list" }]

[[settings]]
key = "company_email"
type = "email"
label = "Email"
required = true

[[settings]]
key = "home_cta_link"
type = "url"
label = "CTA link"

[[settings]]
key = "site_name"
type = "string"
label = "Site name"
`

func schemaFromTOML(t *testing.T, src string) *coreTheme.SettingsSchema {
	t.Helper()
	cfg, err := coreTheme.ParseFileConfig(src)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := cfg.SettingsSchema()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// schemaThemeManager serves "demo" with the declaration above, "plain" with
// none, and "broken" with an invalid declaration.
func schemaThemeManager(t *testing.T) *ThemeManager {
	schema := schemaFromTOML(t, schemaThemeTOML)
	catalog := coreI18n.NewCatalog("en", map[string]map[string]string{
		"en": {
			"settings.group.hero":      "Hero banner",
			"settings.home_hero_title": "Headline",
			"settings.slide_image":     "Picture for slide {n}",
			"settings.layout_list":     "Stacked list",
		},
	})
	return &ThemeManager{
		AvailableFn: func() []ThemeDisplayInfo {
			return []ThemeDisplayInfo{{Name: "Demo", Slug: "demo"}, {Name: "Plain", Slug: "plain"}, {Name: "Broken", Slug: "broken"}}
		},
		SettingsSchemaFn: func(slug string) (*coreTheme.SettingsSchema, error) {
			switch slug {
			case "demo":
				return schema, nil
			case "broken":
				return nil, errors.New(`setting plugin_x: key "plugin_x" is reserved`)
			}
			return nil, nil
		},
		LocaleCatalogFn: func(slug string) *coreI18n.Catalog {
			if slug == "demo" {
				return catalog
			}
			return nil
		},
	}
}

func schemaSettingsRouter(t *testing.T, store *option.Store, role string, withTemplates bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := &Service{options: store, rbac: user.NewRBAC()}
	var h *Handler
	if withTemplates {
		h = NewHandler(svc, content.NewRegistry(), "templates")
	} else {
		h = &Handler{svc: svc}
	}
	h.SetThemeManager(schemaThemeManager(t))
	router := gin.New()
	handle := func(fn gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("admin_user_id", uint(1))
			c.Set("admin_username", "operator")
			c.Set("admin_role", role)
			fn(c)
		}
	}
	router.GET("/admin/themes/:slug/settings", handle(h.ThemeSettings))
	router.POST("/admin/themes/:slug/settings", handle(h.ThemeSettingsSave))
	return router
}

func postSettingsFor(router http.Handler, slug string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/"+slug+"/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func redirectError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	location, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil {
		t.Fatalf("status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
	return location.Query().Get("error")
}

func TestDeclaredThemeSettingsSaveValidatesAndNormalizes(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"admin_language": "en"})
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, false)
	rec := postSettingsFor(router, "demo", url.Values{
		"home_hero_title":    {"Hello"},
		"home_hero_1_image":  {" /uploads/one.png "},
		"home_interval_ms":   {" 3000 "},
		"home_show_partners": {"1", "0"}, // checked: the checkbox comes before the hidden 0
		"showcase_layout":    {"list"},
		"company_email":      {"ops@example.com"},
		"home_cta_link":      {"#contact"},
		"site_name":          {"Demo Site"},
		"color_scheme":       {"ocean"},
	})
	if got := redirectError(t, rec); got != "" {
		t.Fatalf("error = %q", got)
	}
	for key, want := range map[string]string{
		"home_hero_title": "Hello", "home_hero_1_image": "/uploads/one.png", "home_interval_ms": "3000",
		"home_show_partners": "1", "showcase_layout": "list", "company_email": "ops@example.com",
		"home_cta_link": "#contact", "site_name": "Demo Site", "theme_color_scheme": "ocean",
	} {
		if got := store.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// Unchecking stores 0.
	rec = postSettingsFor(router, "demo", url.Values{"home_show_partners": {"0"}, "company_email": {"ops@example.com"}})
	if got := redirectError(t, rec); got != "" || store.Get("home_show_partners") != "0" {
		t.Fatalf("error = %q, home_show_partners = %q", got, store.Get("home_show_partners"))
	}
}

func TestDeclaredThemeSettingsSaveRejectsUndeclaredKeys(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"admin_language": "en"})
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, false)
	rec := postSettingsFor(router, "demo", url.Values{
		"home_hero_title":              {"Hello"},
		"home_extra":                   {"x"},
		"plugin_commerce_currency":     {"EUR"},
		"new_user_default_role":        {"editor"},
		"home_hero_{n}_image":          {"/x.png"},
		"market_developer_mode":        {"1"},
		"company_email":                {"ops@example.com"},
		"demo_imported_modern-company": {"1"},
	})
	msg := redirectError(t, rec)
	for _, key := range []string{"home_extra", "plugin_commerce_currency", "new_user_default_role", "market_developer_mode", "home_hero_{n}_image"} {
		if !strings.Contains(msg, key) {
			t.Errorf("error %q does not name %s", msg, key)
		}
	}
	for _, key := range []string{"home_hero_title", "plugin_commerce_currency", "new_user_default_role", "market_developer_mode"} {
		if got := store.Get(key); got != "" {
			t.Errorf("%s = %q, want nothing saved", key, got)
		}
	}
}

func TestDeclaredThemeSettingsSaveListsEveryInvalidValue(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"admin_language": "en", "home_hero_title": "Old"})
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, false)
	rec := postSettingsFor(router, "demo", url.Values{
		"home_hero_title":   {"New"},
		"home_interval_ms":  {"50"},
		"home_hero_2_image": {"javascript:alert(1)"},
		"showcase_layout":   {"masonry"},
		"company_email":     {""},
		"home_cta_link":     {"//evil.example"},
	})
	msg := redirectError(t, rec)
	for _, part := range []string{
		"Nothing was saved",
		"Interval: must be between 1000 and 20000",
		"Picture for slide 2: must be a site path or an http(s) address",
		"Showcase layout: is not one of the choices",
		"Email: required",
		"CTA link: must be a site path",
	} {
		if !strings.Contains(msg, part) {
			t.Errorf("error %q is missing %q", msg, part)
		}
	}
	if got := store.Get("home_hero_title"); got != "Old" {
		t.Fatalf("home_hero_title = %q, want the valid value not saved either", got)
	}
}

func TestDeclaredThemeSettingsSaveKeepsPermissionAndSystemRules(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"admin_language": "en"})
	rec := postSettingsFor(schemaSettingsRouter(t, store, user.RoleEditor, false), "demo", url.Values{"home_hero_title": {"Hijacked"}})
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/?error=") || store.Get("home_hero_title") != "" {
		t.Fatalf("editor: status = %d location = %q stored = %q", rec.Code, rec.Header().Get("Location"), store.Get("home_hero_title"))
	}

	// A broken declaration saves nothing.
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, false)
	msg := redirectError(t, postSettingsFor(router, "broken", url.Values{"home_title": {"x"}}))
	if !strings.Contains(msg, "settings declaration is invalid") || store.Get("home_title") != "" {
		t.Fatalf("broken: error = %q stored = %q", msg, store.Get("home_title"))
	}

	// A theme without a declaration keeps the legacy prefix rules.
	if msg := redirectError(t, postSettingsFor(router, "plain", url.Values{"home_title": {"Legacy"}, "unrelated": {"x"}})); msg != "" {
		t.Fatalf("plain: error = %q", msg)
	}
	if store.Get("home_title") != "Legacy" || store.Get("unrelated") != "" {
		t.Fatalf("plain: home_title = %q unrelated = %q", store.Get("home_title"), store.Get("unrelated"))
	}
}

func TestGeneratedThemeSettingsForm(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{
		"admin_language":     "en",
		"home_interval_ms":   "7000",
		"home_show_partners": "0",
		"home_hero_1_image":  `/x.png" onerror="alert(1)`,
		"company_email":      "<script>alert(1)</script>",
	})
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/themes/demo/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<form method="POST" action="/admin/themes/demo/settings"`,
		`<h3 class="settings-section-title">Hero banner</h3>`,
		`<h3 class="settings-section-title">Layout</h3>`,
		`<h3 class="settings-section-title">Other</h3>`,
		`<label for="ts-home_hero_title">Headline</label>`,
		`value="Build faster"`, // unsaved: the default
		`maxlength="40"`,
		`aria-describedby="ts-home_hero_title-help"`,
		`<small id="ts-home_hero_title-help">Shown above the fold.</small>`,
		`<label for="ts-home_hero_2_image">Picture for slide 2</label>`,
		`type="number" id="ts-home_interval_ms" name="home_interval_ms" value="7000" min="1000" max="20000"`,
		`<input type="checkbox" id="ts-home_show_partners" name="home_show_partners" value="1" >`,
		`<input type="hidden" name="home_show_partners" value="0">`,
		`<option value="grid" selected>Grid</option>`,
		`<option value="list" >Stacked list</option>`,
		`type="email" id="ts-company_email"`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`/x.png&#34; onerror=&#34;alert(1)`,
		`type="url" id="ts-home_cta_link"`,
		`required aria-required="true"`,
		`Fields marked * are required.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form is missing %s", want)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("a stored value was rendered without escaping")
	}
	hero, layout, other := strings.Index(body, ">Hero banner<"), strings.Index(body, ">Layout<"), strings.Index(body, ">Other<")
	if !(hero < layout && layout < other) {
		t.Fatalf("sections out of declaration order: %d %d %d", hero, layout, other)
	}
}

func TestThemeSettingsPageWithoutFormOrWithBrokenDeclaration(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"admin_language": "en"})
	router := schemaSettingsRouter(t, store, user.RoleSuperAdmin, true)
	for slug, want := range map[string]string{
		"plain":  "does not provide a settings page",
		"broken": `settings declaration is invalid: setting plugin_x: key "plugin_x" is reserved`,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/themes/"+slug+"/settings", nil))
		if msg := redirectError(t, rec); !strings.Contains(msg, want) || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/themes?") {
			t.Errorf("%s: location = %q", slug, rec.Header().Get("Location"))
		}
	}
	// Roles without setting.read cannot open it.
	rec := httptest.NewRecorder()
	schemaSettingsRouter(t, store, user.RoleAuthor, true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/themes/demo/settings", nil))
	if rec.Code != http.StatusFound || strings.Contains(rec.Body.String(), "settings-section") {
		t.Fatalf("author: status = %d", rec.Code)
	}
}
