package moderncompany

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/content"
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

func TestIsProductArchiveURLNormalizesMenuPaths(t *testing.T) {
	engine := newArchiveURLTestEngine()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://example.test/zh/products", nil)

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "plain archive", raw: "/products", want: true},
		{name: "trailing slash", raw: "/products/", want: true},
		{name: "localized", raw: "/zh/products", want: true},
		{name: "relative", raw: "products", want: true},
		{name: "same host absolute", raw: "https://example.test/products", want: true},
		{name: "other host absolute", raw: "https://example.org/products", want: false},
		{name: "other archive", raw: "/services", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isProductArchiveURL(c, engine, tt.raw); got != tt.want {
				t.Fatalf("isProductArchiveURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsContentArchiveURLUsesRewriteSlugFromRegistry(t *testing.T) {
	engine := newArchiveURLTestEngine()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://example.test/", nil)

	tests := []struct {
		contentType string
		rawURL      string
		want        bool
	}{
		{contentType: "product", rawURL: "/products", want: true},
		{contentType: "service", rawURL: "/services/", want: true},
		{contentType: "showcase", rawURL: "/zh/projects", want: true},
		{contentType: "post", rawURL: "/blog", want: true},
		{contentType: "showcase", rawURL: "/showcase", want: false},
		{contentType: "service", rawURL: "/products", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType+" "+tt.rawURL, func(t *testing.T) {
			if got := isContentArchiveURL(c, engine, tt.contentType, tt.rawURL); got != tt.want {
				t.Fatalf("isContentArchiveURL(%q, %q) = %v, want %v", tt.contentType, tt.rawURL, got, tt.want)
			}
		})
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
		Rewrite:    content.RewriteRule{Slug: "blog"},
	})
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
