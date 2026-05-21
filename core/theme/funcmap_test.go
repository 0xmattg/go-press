package theme

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-press/core/content"
	coreI18n "go-press/core/i18n"
	"go-press/core/rewrite"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

type fakeOptionGetter map[string]string

func (f fakeOptionGetter) Get(name string) string {
	return f[name]
}

func TestPageTitleForUsesSEOTitleFromStruct(t *testing.T) {
	type meta struct{ Title string }
	type page struct{ SEO meta }

	fn := CommonFuncMap()["pageTitleFor"].(func(interface{}, string) string)
	if got := fn(page{SEO: meta{Title: " Custom Title "}}, "Fallback Title"); got != "Custom Title" {
		t.Fatalf("pageTitleFor() = %q, want Custom Title", got)
	}
}

func TestPageTitleForUsesSEOTitleFromMap(t *testing.T) {
	fn := CommonFuncMap()["pageTitleFor"].(func(interface{}, string) string)
	data := map[string]interface{}{"SEO": map[string]interface{}{"Title": "Mapped Title"}}

	if got := fn(data, "Fallback Title"); got != "Mapped Title" {
		t.Fatalf("pageTitleFor() = %q, want Mapped Title", got)
	}
}

func TestPageTitleForFallsBackWhenNoTitle(t *testing.T) {
	fn := CommonFuncMap()["pageTitleFor"].(func(interface{}, string) string)

	if got := fn(struct{}{}, "Fallback Title"); got != "Fallback Title" {
		t.Fatalf("pageTitleFor() = %q, want Fallback Title", got)
	}
}

func TestApplySiteOptionOverridesPreservesArchiveTitle(t *testing.T) {
	reg := content.NewRegistry()
	reg.RegisterType(content.ContentTypeDef{
		Name:        "service",
		LabelPlural: "Services",
		Rewrite:     content.RewriteRule{Slug: "services"},
	})
	builder := rewrite.NewSEOBuilder("https://example.test", "Config Site", rewrite.NewEngine(reg))
	seo := builder.ForArchive(reg.GetType("service"))

	ApplySiteOptionOverridesFromOptions(fakeOptionGetter{"site_name": "Runtime Site"}, builder, &seo)

	if seo.Title != "Services | Runtime Site" {
		t.Fatalf("seo.Title = %q, want Services | Runtime Site", seo.Title)
	}
	if seo.OGTitle != "Services" {
		t.Fatalf("seo.OGTitle = %q, want Services", seo.OGTitle)
	}
}

func TestApplySiteOptionOverridesUpdatesHomeTitle(t *testing.T) {
	builder := rewrite.NewSEOBuilder("https://example.test", "Config Site", rewrite.NewEngine(content.NewRegistry()))
	seo := builder.ForHome("Default description")

	ApplySiteOptionOverridesFromOptions(fakeOptionGetter{"site_name": "Runtime Site"}, builder, &seo)

	if seo.Title != "Runtime Site" {
		t.Fatalf("seo.Title = %q, want Runtime Site", seo.Title)
	}
	if seo.OGTitle != "Runtime Site" {
		t.Fatalf("seo.OGTitle = %q, want Runtime Site", seo.OGTitle)
	}
}

func TestApplySiteOptionOverridesDoesNotRewriteCustomTitle(t *testing.T) {
	builder := rewrite.NewSEOBuilder("https://example.test", "Config Site", rewrite.NewEngine(content.NewRegistry()))
	seo := rewrite.SEOMeta{
		Title:   "Custom Campaign Title",
		OGTitle: "Custom Campaign",
		OGType:  "website",
	}

	ApplySiteOptionOverridesFromOptions(fakeOptionGetter{"site_name": "Runtime Site"}, builder, &seo)

	if seo.Title != "Custom Campaign Title" {
		t.Fatalf("seo.Title = %q, want Custom Campaign Title", seo.Title)
	}
	if seo.OGTitle != "Custom Campaign" {
		t.Fatalf("seo.OGTitle = %q, want Custom Campaign", seo.OGTitle)
	}
}

func TestLocalizedArchiveTitleUsesConfiguredMessageKey(t *testing.T) {
	mgr := coreI18n.NewManager("en")
	mgr.AddMessages("en", []*goi18n.Message{{ID: "page_title_services", Other: "Services"}})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	typeDef := &content.ContentTypeDef{
		Name:            "service",
		LabelPlural:     "服务列表",
		ArchiveTitleKey: "page_title_services",
	}

	if got := LocalizedArchiveTitle(c, mgr, typeDef); got != "Services" {
		t.Fatalf("LocalizedArchiveTitle() = %q, want Services", got)
	}
}

func TestLocalizedArchiveTitleFallsBackToRewriteMessageKey(t *testing.T) {
	mgr := coreI18n.NewManager("en")
	mgr.AddMessages("en", []*goi18n.Message{{ID: "page_title_blog", Other: "News & Blog"}})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	typeDef := &content.ContentTypeDef{
		Name:        "post",
		LabelPlural: "文章列表",
		Rewrite:     content.RewriteRule{Slug: "blog"},
	}

	if got := LocalizedArchiveTitle(c, mgr, typeDef); got != "News & Blog" {
		t.Fatalf("LocalizedArchiveTitle() = %q, want News & Blog", got)
	}
}

func TestIsMenuURLActiveMatchesCurrentRequestPath(t *testing.T) {
	fn := CommonFuncMap()["isMenuURLActive"].(func(*gin.Context, string) bool)
	tests := []struct {
		name        string
		currentPath string
		menuURL     string
		want        bool
	}{
		{name: "archive", currentPath: "/solutions", menuURL: "/solutions", want: true},
		{name: "single under archive", currentPath: "/solutions/cleanroom-design", menuURL: "/solutions", want: true},
		{name: "localized archive with unprefixed menu", currentPath: "/zh/solutions", menuURL: "/solutions", want: true},
		{name: "localized single with localized menu", currentPath: "/zh/solutions/cleanroom-design", menuURL: "/zh/solutions", want: true},
		{name: "home", currentPath: "/", menuURL: "/", want: true},
		{name: "other section", currentPath: "/solutions", menuURL: "/equipment", want: false},
		{name: "external link", currentPath: "/solutions", menuURL: "https://example.org/solutions", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "http://example.test"+tt.currentPath, nil)
			if got := fn(c, tt.menuURL); got != tt.want {
				t.Fatalf("isMenuURLActive(%q, %q) = %v, want %v", tt.currentPath, tt.menuURL, got, tt.want)
			}
		})
	}
}
