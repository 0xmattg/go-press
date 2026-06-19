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
