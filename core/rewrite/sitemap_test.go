package rewrite

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestSitemapAlternatesMarshal(t *testing.T) {
	set := SitemapURLSet{
		XMLNS:      "http://www.sitemaps.org/schemas/sitemap/0.9",
		XMLNSXhtml: "http://www.w3.org/1999/xhtml",
		URLs: []SitemapURL{{
			Loc: "http://x/products/a",
			Alternates: []SitemapAlternate{
				{Rel: "alternate", HrefLang: "zh", Href: "http://x/products/a"},
				{Rel: "alternate", HrefLang: "en", Href: "http://x/en/products/a"},
			},
		}},
	}
	b, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	t.Log(out)
	if !strings.Contains(out, `xmlns:xhtml="http://www.w3.org/1999/xhtml"`) {
		t.Errorf("missing xmlns:xhtml: %s", out)
	}
	if !strings.Contains(out, `<xhtml:link rel="alternate" hreflang="zh" href="http://x/products/a"`) {
		t.Errorf("missing zh alternate: %s", out)
	}
	if !strings.Contains(out, `<xhtml:link rel="alternate" hreflang="en" href="http://x/en/products/a"`) {
		t.Errorf("missing en alternate: %s", out)
	}
}
