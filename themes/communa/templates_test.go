package communa

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/0xmattg/go-press/core"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

func TestThemeConfigAndInterfaces(t *testing.T) {
	cfg, err := coreTheme.ParseFileConfig(themeTOML)
	if err != nil {
		t.Fatalf("parse theme.toml: %v", err)
	}
	if cfg.Theme.Name != "Communa" || cfg.Theme.Version != "1.0.12" {
		t.Fatalf("unexpected metadata: %+v", cfg.Theme)
	}
	names := map[string]bool{}
	for _, ct := range cfg.ContentTypes {
		names[ct.Name] = true
	}
	for _, want := range []string{"member", "group", "discussion"} {
		if !names[want] {
			t.Fatalf("theme.toml missing content type %q", want)
		}
	}
	var value interface{} = &CommunaTheme{}
	if _, ok := value.(coreTheme.DemoDataProvider); !ok {
		t.Fatal("theme must implement DemoDataProvider")
	}
	if _, ok := value.(coreTheme.SettingsProvider); !ok {
		t.Fatal("theme must implement SettingsProvider")
	}
}

func TestAllPageTemplatesCompile(t *testing.T) {
	theme := NewWithDB(nil, filepath.Clean("."))
	if _, err := coreTheme.LoadAllPageBundles(theme); err != nil {
		t.Fatalf("compile templates: %v", err)
	}
}

func TestAdminSettingsTemplateCompiles(t *testing.T) {
	path := filepath.Join("templates", "admin", "theme_settings.tmpl")
	funcs := template.FuncMap{
		"X": func(_ interface{}, _ string, fallback string, args ...interface{}) string {
			if len(args) == 0 {
				return fallback
			}
			return fmt.Sprintf(fallback, args...)
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
	}
	if _, err := template.New("settings").Funcs(funcs).ParseFiles(path); err != nil {
		t.Fatalf("compile admin settings template: %v", err)
	}
}

// TestStandardHooksDeclaredExactlyOnce enforces the repository frontend contract:
// each of the four standard hooks is declared exactly once across the theme.
func TestStandardHooksDeclaredExactlyOnce(t *testing.T) {
	body := readAllTemplates(t)
	for _, hook := range []string{"theme.head.end", "theme.body.open", "theme.footer.end", "header.nav.after"} {
		count := strings.Count(body, `renderHook "`+hook+`"`)
		if count != 1 {
			t.Errorf("hook %q declared %d times, want exactly 1", hook, count)
		}
	}
}

// TestHeaderNavHookInsidePrimaryList checks header.nav.after sits inside the
// primary-navigation <ul> and receives the full template data ("." ).
func TestHeaderNavHookInsidePrimaryList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "partials", "header.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	header := string(data)
	if !strings.Contains(header, `{{renderHook "header.nav.after" .}}`) {
		t.Fatal("header must render header.nav.after with the full data context")
	}
	listStart := strings.Index(header, `id="cmn-primary-nav"`)
	hookAt := strings.Index(header, `{{renderHook "header.nav.after" .}}`)
	listEnd := strings.Index(header[listStart:], "</ul>")
	if listStart < 0 || hookAt < 0 || listEnd < 0 {
		t.Fatal("could not locate primary nav list boundaries")
	}
	if !(hookAt > listStart && hookAt < listStart+listEnd) {
		t.Fatal("header.nav.after must be inside the primary-navigation <ul>")
	}
}

func TestDemoSeedIsValidAndContainsNoAdmin(t *testing.T) {
	var seed core.SeedData
	if _, err := toml.DecodeFile(filepath.Join("demo", "data", "seed.toml"), &seed); err != nil {
		t.Fatalf("parse demo seed: %v", err)
	}
	if seed.Admin.Username != "" || seed.Admin.Password != "" || seed.Admin.Email != "" {
		t.Fatal("demo seed must not contain administrator credentials")
	}
	byType := map[string]int{}
	for _, c := range seed.Contents {
		byType[c.Type]++
	}
	for _, want := range []struct {
		typ string
		min int
	}{{"member", 6}, {"group", 6}, {"discussion", 6}, {"post", 6}} {
		if byType[want.typ] < want.min {
			t.Errorf("seed has %d %q items, want >= %d", byType[want.typ], want.typ, want.min)
		}
	}
	seen := map[string]bool{}
	for _, term := range append(append([]core.SeedTaxonomy{}, seed.Categories...), seed.Tags...) {
		if seen[term.Slug] {
			t.Fatalf("duplicate taxonomy slug %q", term.Slug)
		}
		seen[term.Slug] = true
	}
}

func TestSeedHasNoDefaultPassword(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("demo", "data", "seed.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "admin123") || strings.Contains(string(data), "[admin]") {
		t.Fatal("seed must not contain admin credentials or an [admin] section")
	}
}

func TestLocalesAreValidJSON(t *testing.T) {
	paths := []string{"locales/en.json", "locales/zh.json", "locales/admin/en.json", "locales/admin/zh-CN.json"}
	for _, path := range paths {
		values := readLocale(t, path)
		if len(values) == 0 {
			t.Fatalf("locale %s is empty", path)
		}
	}
	if en := readLocale(t, "locales/en.json"); en["page_title_members"] != "Members" {
		t.Fatalf("English members archive title = %q, want Members", en["page_title_members"])
	}
}

func TestFrontendLocalesHaveMatchingKeys(t *testing.T) {
	en := readLocale(t, "locales/en.json")
	zh := readLocale(t, "locales/zh.json")
	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Errorf("zh locale missing %q", key)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("en locale missing %q", key)
		}
	}
}

// TestDefaultLocaleCoversTemplateContract verifies every static T key used in
// templates, every declared content-type label, and every archive title key is
// present in the default (en) locale — so no UI falls back to a raw message ID.
func TestDefaultLocaleCoversTemplateContract(t *testing.T) {
	en := readLocale(t, "locales/en.json")
	body := readAllTemplates(t)

	keyRe := regexp.MustCompile(`(?:\{\{|\()T \$?\.Ctx "([a-z0-9_.]+)"`)
	for _, m := range keyRe.FindAllStringSubmatch(body, -1) {
		if _, ok := en[m[1]]; !ok {
			t.Errorf("default locale missing template key %q", m[1])
		}
	}

	cfg, err := coreTheme.ParseFileConfig(themeTOML)
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range cfg.ContentTypes {
		if _, ok := en["content_type."+ct.Name]; !ok {
			t.Errorf("default locale missing content_type.%s", ct.Name)
		}
		if ct.ArchiveTitleKey != "" {
			if _, ok := en[ct.ArchiveTitleKey]; !ok {
				t.Errorf("default locale missing archive title key %q", ct.ArchiveTitleKey)
			}
		}
	}
	// The blog archive resolves its title from the core "post" rewrite slug.
	if _, ok := en["page_title_blog"]; !ok {
		t.Error("default locale missing page_title_blog for the post archive")
	}
}

func TestThemeHelpers(t *testing.T) {
	if got := initials("Maya Chen"); got != "MC" {
		t.Errorf("initials = %q, want MC", got)
	}
	if got := initials("Cher"); got != "C" {
		t.Errorf("initials single = %q, want C", got)
	}
	if got := atoiOr("1,284", 0); got != 1284 {
		t.Errorf("atoiOr = %d, want 1284", got)
	}
	if got := settingIntBetween(map[string]string{"n": "9"}, "n", 3, 1, 6); got != 6 {
		t.Errorf("settingIntBetween clamp = %d, want 6", got)
	}
	if got := compactExcerpt("", "<p>Hello world</p>", 100); got != "Hello world" {
		t.Errorf("compactExcerpt strip = %q", got)
	}
}

// TestLoginModalContract checks the theme ships a themed sign-in modal wired to
// the core public-auth provider helpers and opened from the header.
func TestLoginModalContract(t *testing.T) {
	modal, err := os.ReadFile(filepath.Join("templates", "partials", "login-modal.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"isLoggedIn .Ctx", "loginProviders", "loginProviderURL .BeginURL $.Ctx", `role="dialog"`, `aria-modal="true"`, "data-login-close"} {
		if !strings.Contains(string(modal), want) {
			t.Errorf("login modal missing %q", want)
		}
	}
	header, err := os.ReadFile(filepath.Join("templates", "partials", "header.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(header), "data-login-open") {
		t.Error("header must open the login modal via data-login-open")
	}
	base, err := os.ReadFile(filepath.Join("templates", "layouts", "base.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(base), `{{template "cmn-login-modal" .}}`) {
		t.Error("base layout must include the login modal partial")
	}
}

// ---- helpers ----

func readLocale(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return values
}

func readAllTemplates(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk("templates", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".tmpl" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
