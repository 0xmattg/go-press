package multilang

import (
	"testing"

	"go-press/core/menu"
)

func TestRewriteItemURLSkipsNonPageLinks(t *testing.T) {
	p := &Plugin{}
	tests := []string{
		"https://github.com/0xmattg/go-press",
		"http://example.com",
		"//cdn.example.com/app.js",
		"mailto:hello@example.com",
		"tel:+15550100",
		"#features",
		"?preview=1",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			if got := p.rewriteItemURL(item, "en"); got != rawURL {
				t.Fatalf("rewriteItemURL(%q) = %q, want unchanged", rawURL, got)
			}
		})
	}
}

func TestRewriteItemURLPrefixesLocalLinks(t *testing.T) {
	p := &Plugin{}
	tests := map[string]string{
		"/about": "en/about",
		"about":  "en/about",
		"/":      "en/",
	}

	for rawURL, wantSuffix := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			want := "/" + wantSuffix
			if got := p.rewriteItemURL(item, "en"); got != want {
				t.Fatalf("rewriteItemURL(%q) = %q, want %q", rawURL, got, want)
			}
		})
	}
}
