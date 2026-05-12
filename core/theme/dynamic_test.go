package theme

import (
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

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
