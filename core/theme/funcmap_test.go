package theme

import "testing"

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
