package rewrite

import (
	"strings"
	"testing"
)

func TestRenderHeadUsesGeneratedFaviconFirst(t *testing.T) {
	builder := NewSEOBuilder("https://example.com", "Example", NewEngine(nil))

	got := string(builder.RenderHead(SEOMeta{
		SiteIcon: "/static/uploads/2026/05/icon.png",
	}))

	for _, want := range []string{
		`<link rel="icon" href="/favicon.ico" sizes="any">`,
		`<link rel="icon" type="image/png" sizes="192x192" href="/static/uploads/2026/05/icon.png">`,
		`<link rel="apple-touch-icon" sizes="180x180" href="/static/uploads/2026/05/icon.png">`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderHead missing %q:\n%s", want, got)
		}
	}
}

func TestForTaxonomyUsesTermCanonical(t *testing.T) {
	builder := NewSEOBuilder("https://example.com/", "Example", NewEngine(nil))

	got := builder.ForTaxonomy("category", "Cleanroom Standards", "cleanroom-standards")

	if got.CanonicalURL != "https://example.com/category/cleanroom-standards" {
		t.Fatalf("CanonicalURL = %q", got.CanonicalURL)
	}
	if got.Title != "Cleanroom Standards | Example" {
		t.Fatalf("Title = %q", got.Title)
	}
	if got.Robots != "index,follow" {
		t.Fatalf("Robots = %q", got.Robots)
	}
}
