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

func TestRenderHeadEmitsOpenGraphURLAndSiteName(t *testing.T) {
	builder := NewSEOBuilder("https://example.com", "Example & Co", NewEngine(nil))

	got := string(builder.RenderHead(SEOMeta{
		CanonicalURL:  "https://example.com/blog/clean-room?ref=a&b=c",
		OGTitle:       `Cleanroom "Testing"`,
		OGDescription: "A practical guide & checklist.",
		OGImage:       "https://example.com/uploads/cleanroom.jpg",
		OGType:        "article",
	}))

	for _, want := range []string{
		`<meta property="og:url" content="https://example.com/blog/clean-room?ref=a&amp;b=c">`,
		`<meta property="og:site_name" content="Example &amp; Co">`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderHead missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `name="twitter:`) {
		t.Fatalf("core should not emit provider-specific Twitter Card tags:\n%s", got)
	}
}
