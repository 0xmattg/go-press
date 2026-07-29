package moderncompany

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/content"
	coreI18n "go-press/core/i18n"
	"go-press/core/rewrite"
	coreTheme "go-press/core/theme"
)

func TestTemplatesCompile(t *testing.T) {
	theme := NewWithDB(nil, ".")
	if err := theme.handler.LoadPageTemplates(theme); err != nil {
		t.Fatal(err)
	}
}

func TestDemoSeedDoesNotDefineAdmin(t *testing.T) {
	var data core.SeedData
	if _, err := toml.DecodeFile("demo/data/seed.toml", &data); err != nil {
		t.Fatal(err)
	}
	if data.Admin.Username != "" || data.Admin.Password != "" {
		t.Fatal("theme demo seed must not define admin credentials")
	}
}

func TestBaseTemplateUsesSEOTitleWhenAvailable(t *testing.T) {
	tmpl := newBaseTemplateTest(t)
	data := PageData{
		Title:    "Visible Title",
		Settings: map[string]string{"site_name": "Hurricane Techs"},
		SEO:      rewrite.SEOMeta{Title: "Custom SEO Title"},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "base", data); err != nil {
		t.Fatalf("execute base template: %v", err)
	}
	if !strings.Contains(out.String(), "<title>Custom SEO Title</title>") {
		t.Fatalf("expected SEO title, got: %s", out.String())
	}
}

func TestBaseTemplateFallsBackWhenSEOTitleEmpty(t *testing.T) {
	tmpl := newBaseTemplateTest(t)
	data := PageData{Title: "Visible Title", Settings: map[string]string{"site_name": "Hurricane Techs"}}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "base", data); err != nil {
		t.Fatalf("execute base template: %v", err)
	}
	if !strings.Contains(out.String(), "<title>Visible Title - Hurricane Techs</title>") {
		t.Fatalf("expected fallback title, got: %s", out.String())
	}
}

func TestContentMegaMenuForURLUsesRewriteSlugFromRegistry(t *testing.T) {
	engine := newArchiveURLTestEngine()
	svc := &PageService{
		SEOPageService: coreTheme.SEOPageService{Registry: engine.Registry},
		rewriteEngine:  engine.Rewrite,
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://example.test/", nil)

	tests := []struct {
		name        string
		rawURL      string
		contentType string
	}{
		{name: "theme rewrite slug", rawURL: "/projects", contentType: "showcase"},
		{name: "localized theme rewrite slug", rawURL: "/es/projects", contentType: "showcase"},
		{name: "core rewrite slug", rawURL: "/blog", contentType: "post"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			menu := svc.ContentMegaMenuForURL(c, tt.rawURL)
			if menu.ContentType != tt.contentType {
				t.Fatalf("ContentMegaMenuForURL(%q).ContentType = %q, want %q", tt.rawURL, menu.ContentType, tt.contentType)
			}
		})
	}

	if menu := svc.ContentMegaMenuForURL(c, "/showcase"); menu.ContentType != "" {
		t.Fatalf("ContentMegaMenuForURL(%q).ContentType = %q, want empty", "/showcase", menu.ContentType)
	}
}

func TestContentTaxonomyURLUsesCanonicalTaxonomyPath(t *testing.T) {
	engine := newArchiveURLTestEngine()
	svc := &PageService{rewriteEngine: engine.Rewrite}
	if got := svc.contentTaxonomyURL("category", "cleanroom-standards"); got != "/category/cleanroom-standards" {
		t.Fatalf("contentTaxonomyURL() = %q", got)
	}
}

func TestRedirectLegacyTaxonomyFilterPreservesLanguage(t *testing.T) {
	engine := newArchiveURLTestEngine()
	engine.I18n = coreI18n.NewManager("en")
	theme := &ModernCompanyTheme{engine: engine}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "https://example.test/blog?category=cleanroom-standards", nil)
	c.Set(coreI18n.CtxKeyLang, "es")

	if !theme.redirectLegacyTaxonomyFilter(c) {
		t.Fatal("expected legacy taxonomy filter to redirect")
	}
	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/es/category/cleanroom-standards" {
		t.Fatalf("Location = %q", location)
	}
}

func TestTemplatesDoNotGenerateLegacyTaxonomyQueryLinks(t *testing.T) {
	paths := []string{
		"templates/pages/blog.tmpl",
		"templates/pages/post-detail.tmpl",
		"templates/pages/services.tmpl",
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "?category=") || strings.Contains(string(body), "?tag=") {
			t.Fatalf("%s still contains a legacy taxonomy query link", path)
		}
	}
}

func newArchiveURLTestEngine() *core.Engine {
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name:       "product",
		HasArchive: true,
		Rewrite:    content.RewriteRule{Slug: "products"},
	})
	registry.RegisterType(content.ContentTypeDef{
		Name:       "service",
		HasArchive: true,
		Rewrite:    content.RewriteRule{Slug: "services"},
	})
	registry.RegisterType(content.ContentTypeDef{
		Name:       "showcase",
		HasArchive: true,
		Rewrite:    content.RewriteRule{Slug: "projects"},
	})
	registry.RegisterType(content.ContentTypeDef{
		Name:       "post",
		HasArchive: true,
		Taxonomies: []string{"category", "tag"},
		Rewrite:    content.RewriteRule{Slug: "blog"},
	})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "category", Hierarchical: true})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "tag"})
	return &core.Engine{
		Registry: registry,
		Rewrite:  rewrite.NewEngine(registry),
	}
}

func newBaseTemplateTest(t *testing.T) *template.Template {
	t.Helper()
	tmpl := template.New("base_test").Funcs(template.FuncMap{
		"currentLang": func(*gin.Context) string { return "en" },
		"settingOr": func(m map[string]string, key, fallback string) string {
			if v := m[key]; v != "" {
				return v
			}
			return fallback
		},
		"seoHeadFor": func(interface{}) template.HTML { return "" },
		"faviconLinks": func(string) template.HTML {
			return ""
		},
		"pageTitleFor": func(data interface{}, fallback string) string {
			return coreTheme.CommonFuncMap()["pageTitleFor"].(func(interface{}, string) string)(data, fallback)
		},
		"responsiveImagePreload": func(string, string) template.HTML { return "" },
		"renderHook":             func(string, interface{}) template.HTML { return "" },
	})
	if _, err := tmpl.Parse(`{{define "header"}}{{end}}{{define "content"}}{{end}}{{define "footer"}}{{end}}`); err != nil {
		t.Fatalf("parse template stubs: %v", err)
	}
	if _, err := tmpl.ParseFiles("templates/layouts/base.tmpl"); err != nil {
		t.Fatalf("parse base template: %v", err)
	}
	return tmpl
}
