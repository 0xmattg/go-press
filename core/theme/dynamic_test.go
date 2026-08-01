package theme

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
)

func TestDynamicArchivePageCandidatesUseRewriteSlug(t *testing.T) {
	typeDef := &content.ContentTypeDef{
		Name:        "case_study",
		LabelPlural: "Case Studies",
		Rewrite:     content.RewriteRule{Slug: "cases"},
	}

	got := archivePageCandidates("case_study", typeDef)
	want := []string{"archive-case_study", "cases", "case_study", "case_studies", "archive"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestDynamicArchivePageCandidatesUseConfiguredTemplate(t *testing.T) {
	typeDef := &content.ContentTypeDef{
		Name:      "architecture",
		Rewrite:   content.RewriteRule{Slug: "architecture"},
		Templates: content.TemplateDef{Archive: "services"},
	}

	got := archivePageCandidates("architecture", typeDef)
	want := []string{"archive-architecture", "architecture", "architectures", "services", "archive"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestDynamicSinglePageCandidatesUseContentTypeAndRewriteSlug(t *testing.T) {
	typeDef := &content.ContentTypeDef{
		Name:    "case_study",
		Rewrite: content.RewriteRule{Slug: "cases"},
	}

	got := singlePageCandidates("case_study", "acme", typeDef)
	want := []string{"single-case_study-acme", "single-case_study", "case_study-detail", "case_study_detail", "cases-detail", "single"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestDynamicSinglePageCandidatesUseConfiguredTemplate(t *testing.T) {
	typeDef := &content.ContentTypeDef{
		Name:      "architecture",
		Rewrite:   content.RewriteRule{Slug: "architecture"},
		Templates: content.TemplateDef{Single: "service-detail"},
	}

	got := singlePageCandidates("architecture", "theme-build", typeDef)
	if !containsString(got, "service-detail") {
		t.Fatalf("single candidates should include configured service-detail template: %#v", got)
	}
}

func TestLegacyAliasesPointToCurrentArchiveOnly(t *testing.T) {
	items := []map[string]interface{}{{"Title": "Dynamic item"}}
	data := gin.H{}
	addLegacyListAliases(data, items)

	for _, key := range []string{"Products", "Services", "Showcases", "Posts", "Articles", "Updates", "Analyses"} {
		got, ok := data[key].([]map[string]interface{})
		if !ok {
			t.Fatalf("%s alias type = %T, want []map[string]interface{}", key, data[key])
		}
		if got[0]["Title"] != "Dynamic item" {
			t.Fatalf("%s alias did not point at current archive items", key)
		}
	}
}

func TestArchiveOrderUsesSortOrderWhenSupported(t *testing.T) {
	typeDef := &content.ContentTypeDef{Supports: []string{"title", "sort_order"}}

	if got := archiveOrderField(typeDef); got != "sort_order" {
		t.Fatalf("archiveOrderField = %q, want sort_order", got)
	}
	if got := archiveOrderDir(typeDef); got != "ASC" {
		t.Fatalf("archiveOrderDir = %q, want ASC", got)
	}
}

func TestArchiveOrderDefaultsToPublishedAt(t *testing.T) {
	typeDef := &content.ContentTypeDef{Supports: []string{"title"}}

	if got := archiveOrderField(typeDef); got != "published_at" {
		t.Fatalf("archiveOrderField = %q, want published_at", got)
	}
	if got := archiveOrderDir(typeDef); got != "DESC" {
		t.Fatalf("archiveOrderDir = %q, want DESC", got)
	}
}

func TestArchiveQueryTaxonomyFilterUsesRegisteredTaxonomyQuery(t *testing.T) {
	c := &gin.Context{}
	c.Request = httptest.NewRequest("GET", "/blog?tag=hvac", nil)
	typeDef := &content.ContentTypeDef{Taxonomies: []string{"category", "tag"}}

	taxonomy, term := archiveQueryTaxonomyFilter(c, typeDef)
	if taxonomy != "tag" || term != "hvac" {
		t.Fatalf("archiveQueryTaxonomyFilter() = (%q, %q), want (tag, hvac)", taxonomy, term)
	}
}

func TestArchiveQueryTaxonomyFilterIgnoresUnregisteredQuery(t *testing.T) {
	c := &gin.Context{}
	c.Request = httptest.NewRequest("GET", "/blog?tag=hvac", nil)
	typeDef := &content.ContentTypeDef{Taxonomies: []string{"category"}}

	taxonomy, term := archiveQueryTaxonomyFilter(c, typeDef)
	if taxonomy != "" || term != "" {
		t.Fatalf("archiveQueryTaxonomyFilter() = (%q, %q), want empty filter", taxonomy, term)
	}
}

func TestArchiveSearchQueryTrimsAndLimitsUnicodeByRunes(t *testing.T) {
	c := &gin.Context{}
	c.Request = httptest.NewRequest("GET", "/store?q=++"+strings.Repeat("商", maxArchiveSearchRunes+5)+"++", nil)

	got := archiveSearchQuery(c)
	if len([]rune(got)) != maxArchiveSearchRunes {
		t.Fatalf("query rune length = %d, want %d", len([]rune(got)), maxArchiveSearchRunes)
	}
	if strings.Contains(got, " ") {
		t.Fatalf("query was not trimmed: %q", got)
	}
}

func TestArchivePageURLPreservesSearchAndTaxonomyFilters(t *testing.T) {
	c := &gin.Context{}
	c.Request = httptest.NewRequest("GET", "/zh/store/page/2?q=headphones&product_cat=audio&page=99", nil)

	if got := archivePageURL(c, 3); got != "/zh/store/page/3?product_cat=audio&q=headphones" {
		t.Fatalf("archivePageURL() = %q", got)
	}
	if got := archivePageURL(c, 1); got != "/zh/store?product_cat=audio&q=headphones" {
		t.Fatalf("first archive page URL = %q", got)
	}
}

func TestArchiveBasePathOnlyStripsValidPageSuffix(t *testing.T) {
	tests := map[string]string{
		"/store/page/4":      "/store",
		"/store/page/latest": "/store/page/latest",
		"/page/2":            "/",
		"/store/":            "/store",
	}
	for input, want := range tests {
		if got := archiveBasePath(input); got != want {
			t.Errorf("archiveBasePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestArchivePageWindowIsBoundedAroundCurrentPage(t *testing.T) {
	if got := archivePageWindow(50, 100); !reflect.DeepEqual(got, []int{47, 48, 49, 50, 51, 52, 53}) {
		t.Fatalf("middle page window = %#v", got)
	}
	if got := archivePageWindow(100, 100); !reflect.DeepEqual(got, []int{94, 95, 96, 97, 98, 99, 100}) {
		t.Fatalf("end page window = %#v", got)
	}
	if got := archivePageWindow(1, 3); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("short page window = %#v", got)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
