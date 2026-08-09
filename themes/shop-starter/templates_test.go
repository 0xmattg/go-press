package shopstarter

import (
	"encoding/json"
	"encoding/xml"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/option"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

func stubFuncs() template.FuncMap {
	html := func(...interface{}) template.HTML { return "" }
	str := func(...interface{}) string { return "" }
	any := func(...interface{}) interface{} { return nil }
	boolFn := func(...interface{}) bool { return false }
	return template.FuncMap{
		"T":                       str,
		"currentLang":             str,
		"settingOr":               str,
		"themeURL":                str,
		"seoHeadFor":              html,
		"faviconLinks":            html,
		"renderHook":              html,
		"menuByLocation":          any,
		"responsiveImage":         html,
		"responsiveImagePriority": html,
		"safeHTML":                html,
		"isMenuURLActive":         boolFn,
		"isLoggedIn":              boolFn,
		"loginURL":                str,
		"loginProviderURL":        str,
		"logoutURL":               str,
		"loginProviders":          any,
		"contentURL":              str,
		"archiveURL":              str,
		"taxonomyURL":             str,
		"langPrefixURL":           str,
		"archivePageURL":          str,
		"archivePageWindow":       any,
		"formatDate":              str,
	}
}

func loadLocale(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return values
}

func TestTemplatesCompile(t *testing.T) {
	base := filepath.Join("templates", "layouts", "base.tmpl")
	partials, err := filepath.Glob(filepath.Join("templates", "partials", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	pages, err := filepath.Glob(filepath.Join("templates", "pages", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("no page templates found")
	}
	for _, page := range pages {
		files := append([]string{base}, partials...)
		files = append(files, page)
		if _, err := template.New("").Funcs(stubFuncs()).ParseFiles(files...); err != nil {
			t.Errorf("parse %s: %v", filepath.Base(page), err)
		}
	}
}

func TestEmptyHomeRendersCompleteStorefront(t *testing.T) {
	base := filepath.Join("templates", "layouts", "base.tmpl")
	partials, _ := filepath.Glob(filepath.Join("templates", "partials", "*.tmpl"))
	files := append([]string{base}, partials...)
	files = append(files, filepath.Join("templates", "pages", "home.tmpl"))
	tmpl, err := template.New("").Funcs(stubFuncs()).ParseFiles(files...)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := gin.CreateTestContext(nil)
	data := &HomeData{Ctx: c, ActivePage: "home", Settings: map[string]string{}, HeroImage: defaultHeroImage}
	var output strings.Builder
	if err := tmpl.ExecuteTemplate(&output, "base", data); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"<header", `id="products"`, "<footer", "</html>"} {
		if !strings.Contains(output.String(), marker) {
			t.Errorf("home output missing %q", marker)
		}
	}
}

func TestAdminSettingsTemplateCompiles(t *testing.T) {
	path := filepath.Join("templates", "admin", "theme_settings.tmpl")
	if _, err := template.New("").Funcs(template.FuncMap{
		"X": func(...interface{}) string { return "" },
	}).ParseFiles(path); err != nil {
		t.Fatal(err)
	}
}

func TestHeaderUsesCorePublicAuthContracts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "partials", "header.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, marker := range []string{"isLoggedIn .Ctx", "loginURL .Ctx", "data-login-open", `method="post" action="{{logoutURL}}"`, `name="return_to"`, "/my-account/orders"} {
		if !strings.Contains(source, marker) {
			t.Errorf("header is missing public-auth contract %q", marker)
		}
	}
	if strings.Contains(source, `<a href="{{logoutURL}}"`) {
		t.Error("logout must use Core's POST endpoint, not a GET link")
	}
	searchPosition := strings.Index(source, `class="ss-search"`)
	navPosition := strings.Index(source, `class="ss-nav"`)
	if searchPosition < 0 || navPosition < 0 || searchPosition > navPosition {
		t.Error("desktop header must place search between the brand and the right-aligned navigation controls")
	}
}

func TestLoginModalUsesProviderDiscoveryAndSafeReturnHelper(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "partials", "login-modal.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, marker := range []string{"loginProviders", "loginProviderURL .BeginURL $.Ctx", `role="dialog"`, `aria-modal="true"`, "data-login-close"} {
		if !strings.Contains(source, marker) {
			t.Errorf("login modal is missing %q", marker)
		}
	}
}

func TestLocalesCoverTemplateKeys(t *testing.T) {
	en := loadLocale(t, filepath.Join("locales", "en.json"))
	zh := loadLocale(t, filepath.Join("locales", "zh.json"))
	adminEN := loadLocale(t, filepath.Join("locales", "admin", "en.json"))
	adminZH := loadLocale(t, filepath.Join("locales", "admin", "zh-CN.json"))
	assertSameKeys := func(name string, left, right map[string]string) {
		t.Helper()
		for key := range left {
			if _, ok := right[key]; !ok {
				t.Errorf("%s missing %q", name, key)
			}
		}
		for key := range right {
			if _, ok := left[key]; !ok {
				t.Errorf("%s has extra %q", name, key)
			}
		}
	}
	assertSameKeys("Chinese storefront locale", en, zh)
	assertSameKeys("Chinese admin locale", adminEN, adminZH)

	frontKey := regexp.MustCompile(`T\s+\$?\.Ctx\s+"([^"]+)"`)
	for _, dir := range []string{"layouts", "pages", "partials"} {
		paths, _ := filepath.Glob(filepath.Join("templates", dir, "*.tmpl"))
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range frontKey.FindAllStringSubmatch(string(data), -1) {
				if _, ok := en[match[1]]; !ok {
					t.Errorf("%s uses missing locale key %q", path, match[1])
				}
			}
		}
	}

	adminData, err := os.ReadFile(filepath.Join("templates", "admin", "theme_settings.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	adminKey := regexp.MustCompile(`X\s+\$\s+"([^"]+)"`)
	for _, match := range adminKey.FindAllStringSubmatch(string(adminData), -1) {
		if _, ok := adminEN[match[1]]; !ok {
			t.Errorf("admin template uses missing locale key %q", match[1])
		}
	}
}

func TestManifestDeclaresCommerceWithoutOwningProducts(t *testing.T) {
	cfg, err := coreTheme.LoadFileConfig(".")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Version != "1.0.4" {
		t.Fatalf("theme version = %q", cfg.Theme.Version)
	}
	if len(cfg.ContentTypes) != 0 {
		t.Fatalf("shop theme must not declare plugin-owned content types: %#v", cfg.ContentTypes)
	}
	found := false
	for _, requirement := range cfg.Requires.Plugins {
		if requirement.Slug == "commerce" && requirement.Version == ">=0.2.1" {
			found = true
		}
	}
	if !found {
		t.Fatal("commerce dependency is missing")
	}
}

func TestDemoSeedIsSafeAndComplete(t *testing.T) {
	path := filepath.Join("demo", "data", "seed.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "[admin]") || strings.Contains(source, "admin123") {
		t.Fatal("theme demo must not contain administrator credentials")
	}
	if regexp.MustCompile(`\p{Han}`).Match(data) {
		t.Fatal("default demo data must remain English")
	}
	var seed core.SeedData
	if _, err := toml.Decode(source, &seed); err != nil {
		t.Fatal(err)
	}
	if len(seed.Contents) != 6 {
		t.Fatalf("demo product count = %d, want 6", len(seed.Contents))
	}
	for _, item := range seed.Contents {
		if item.Type != "product" || item.Meta["_commerce_price"] == "" || item.Meta["_commerce_sku"] == "" {
			t.Errorf("incomplete product seed: %#v", item)
		}
	}
	seen := map[string]bool{}
	for _, taxonomy := range seed.Taxonomies {
		if seen[taxonomy.Slug] {
			t.Errorf("duplicate global term slug %q", taxonomy.Slug)
		}
		seen[taxonomy.Slug] = true
	}
}

func TestSettingsHaveAdminSeedAndRuntimeConsumers(t *testing.T) {
	adminData, _ := os.ReadFile(filepath.Join("templates", "admin", "theme_settings.tmpl"))
	runtimeFiles := []string{
		filepath.Join("templates", "layouts", "base.tmpl"),
		filepath.Join("templates", "partials", "header.tmpl"),
		filepath.Join("templates", "partials", "footer.tmpl"),
		filepath.Join("templates", "pages", "home.tmpl"),
		"services.go",
	}
	var runtime strings.Builder
	for _, path := range runtimeFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		runtime.Write(data)
	}
	seedData, _ := os.ReadFile(filepath.Join("demo", "data", "seed.toml"))
	for _, key := range []string{
		"site_name", "site_description", "home_announcement", "home_hero_eyebrow",
		"home_hero_title", "home_hero_description", "home_hero_primary_cta",
		"home_hero_secondary_cta", "home_hero_image", "home_products_title",
		"home_products_description", "footer_tagline", "company_email", "social_x", "social_github",
	} {
		if !strings.Contains(string(adminData), `name="`+key+`"`) {
			t.Errorf("admin settings missing %q", key)
		}
		if !strings.Contains(string(seedData), `key = "`+key+`"`) {
			t.Errorf("demo seed missing %q", key)
		}
		if !strings.Contains(runtime.String(), key) {
			t.Errorf("setting %q has no runtime consumer", key)
		}
	}
}

func TestTranslatableOptionsAreRegistered(t *testing.T) {
	option.ClearTranslatableOptions()
	defer option.ClearTranslatableOptions()
	registerTranslatableOptions()
	registered := map[string]bool{}
	for _, item := range option.AllTranslatableOptions() {
		registered[item.Key] = true
	}
	for _, key := range []string{
		"home_announcement", "home_hero_eyebrow", "home_hero_title", "home_hero_description",
		"home_hero_primary_cta", "home_hero_secondary_cta", "home_products_title",
		"home_products_description", "footer_tagline",
	} {
		if !registered[key] {
			t.Errorf("translatable option %q is not registered", key)
		}
	}
}

func TestThemeURLsRejectExecutableSchemes(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "data:text/html,unsafe", "//evil.example/test", "relative/path"} {
		if got := safeThemeURL(value, "/fallback"); got != "/fallback" {
			t.Errorf("safeThemeURL(%q) = %q", value, got)
		}
	}
	for _, value := range []string{"/static/images/starter-hero.svg", "https://cdn.example.com/hero.jpg"} {
		if got := safeThemeURL(value, "/fallback"); got != value {
			t.Errorf("safeThemeURL(%q) = %q", value, got)
		}
	}
}

func TestSVGAssetsAreValid(t *testing.T) {
	for _, path := range []string{filepath.Join("static", "logo.svg"), filepath.Join("static", "images", "starter-hero.svg")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal(data, &root); err != nil || root.XMLName.Local != "svg" {
			t.Errorf("invalid SVG %s: %v", path, err)
		}
	}
}

func TestThemeImplementationDoesNotImportPlugins(t *testing.T) {
	paths, _ := filepath.Glob("*.go")
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "github.com/0xmattg/go-press/plugins/") {
			t.Errorf("theme implementation imports a plugin in %s", path)
		}
	}
}
