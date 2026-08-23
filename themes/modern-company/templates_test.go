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

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
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

func TestBaseTemplateScopesXCardToBlogArticles(t *testing.T) {
	tmpl := newBaseTemplateTest(t)
	article := PageData{
		Title:      "Cleanroom Testing",
		ActivePage: "blog",
		Settings:   map[string]string{"site_name": "Hurricane Techs"},
		SEO: rewrite.SEOMeta{
			OGType:        "article",
			OGTitle:       "Cleanroom Testing",
			OGDescription: "A practical guide.",
			OGImage:       "https://example.test/uploads/cleanroom.jpg",
		},
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "base", article); err != nil {
		t.Fatalf("execute article template: %v", err)
	}
	for _, want := range []string{
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="twitter:title" content="Cleanroom Testing">`,
		`<meta name="twitter:description" content="A practical guide.">`,
		`<meta name="twitter:image" content="https://example.test/uploads/cleanroom.jpg">`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("article head is missing %q: %s", want, out.String())
		}
	}

	article.ActivePage = "product"
	out.Reset()
	if err := tmpl.ExecuteTemplate(&out, "base", article); err != nil {
		t.Fatalf("execute non-blog template: %v", err)
	}
	if strings.Contains(out.String(), `name="twitter:`) {
		t.Fatalf("non-blog page should not emit theme-specific X Card tags: %s", out.String())
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

func TestBlogArchiveUsesEighteenItemsPerPage(t *testing.T) {
	theme := NewWithDB(nil, ".")
	if got := theme.ArchivePageSize("post"); got != 18 {
		t.Fatalf("post archive page size = %d, want 18", got)
	}
}

func TestBlogTemplateRendersCorePagination(t *testing.T) {
	body, err := os.ReadFile("templates/pages/blog.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	templateBody := string(body)
	for _, want := range []string{"{{with .Pagination}}", ".TotalPages", "archivePageWindow", "archivePageURL", `aria-current="page"`} {
		if !strings.Contains(templateBody, want) {
			t.Fatalf("blog template is missing pagination marker %q", want)
		}
	}
}

func TestBlogTemplateLinksCardImagesToPost(t *testing.T) {
	body, err := os.ReadFile("templates/pages/blog.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	templateBody := string(body)
	link := `<a href="{{langPrefixURL $.Ctx (contentURL . "post")}}" class="blog-card-image-link" aria-label="{{.Title}}">`
	if !strings.Contains(templateBody, link) {
		t.Fatalf("blog card image link is missing: %s", link)
	}
}

func TestPostDetailTemplateProvidesSocialShareActions(t *testing.T) {
	body, err := os.ReadFile("templates/pages/post-detail.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	templateBody := string(body)
	for _, want := range []string{
		`https://x.com/intent/tweet?url=`,
		`https://www.facebook.com/sharer/sharer.php?u=`,
		`https://www.linkedin.com/sharing/share-offsite/?url=`,
		`https://www.pinterest.com/pin/create/button/?url=`,
		`{{urlquery .SEO.CanonicalURL}}`,
		`class="post-share-button post-share-copy"`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(templateBody, want) {
			t.Fatalf("post detail template is missing social share marker %q", want)
		}
	}
}

func TestPostShareRailDoesNotConsumeArticleGridWidth(t *testing.T) {
	body, err := os.ReadFile("static/css/style.css")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet := string(body)
	for _, want := range []string{
		`grid-template-columns: minmax(0, 1fr) 320px;`,
		`transform: translateX(-68px);`,
		`@media (max-width: 1380px)`,
	} {
		if !strings.Contains(stylesheet, want) {
			t.Fatalf("post share layout is missing %q", want)
		}
	}
	if strings.Contains(stylesheet, `grid-template-columns: 44px minmax(0, 1fr) 320px;`) {
		t.Fatal("post share rail must not consume a dedicated desktop grid column")
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
