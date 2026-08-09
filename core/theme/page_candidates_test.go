package theme

import (
	"testing"

	"github.com/0xmattg/go-press/core/content"
)

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

func TestPageBundleCandidates(t *testing.T) {
	pageType := &content.ContentTypeDef{Name: "page", Rewrite: content.RewriteRule{Rootless: true}}

	t.Run("page hierarchy before generic single", func(t *testing.T) {
		got := pageBundleCandidates(pageType, "page", "about", "")
		iAbout := indexOf(got, "page-about")
		iPage := indexOf(got, "page")
		iSingle := indexOf(got, "single")
		if iAbout < 0 || iPage < 0 || iSingle < 0 {
			t.Fatalf("missing expected candidates in %v", got)
		}
		if !(iAbout < iPage && iPage < iSingle) {
			t.Fatalf("order wrong: page-about(%d) < page(%d) < single(%d) not satisfied: %v", iAbout, iPage, iSingle, got)
		}
	})

	t.Run("selected template takes precedence", func(t *testing.T) {
		got := pageBundleCandidates(pageType, "page", "about", "page-full-width")
		if len(got) == 0 || got[0] != "page-full-width" {
			t.Fatalf("selected template should be first, got %v", got)
		}
	})

	t.Run("non-rootless type has no page hierarchy", func(t *testing.T) {
		postType := &content.ContentTypeDef{Name: "post", HasArchive: true, Rewrite: content.RewriteRule{Slug: "blog"}}
		got := pageBundleCandidates(postType, "post", "hello", "")
		if indexOf(got, "page") >= 0 {
			t.Fatalf("post should not get a page template candidate: %v", got)
		}
		if indexOf(got, "single-post") < 0 {
			t.Fatalf("post should keep its single-post candidate: %v", got)
		}
	})
}

func TestSingleEngineCandidates(t *testing.T) {
	pageType := &content.ContentTypeDef{Name: "page", Rewrite: content.RewriteRule{Rootless: true}}
	got := singleEngineCandidates(pageType, "page", "about", "")
	if indexOf(got, "page-about.tmpl") < 0 || indexOf(got, "page.tmpl") < 0 {
		t.Fatalf("engine candidates missing page templates: %v", got)
	}
	if indexOf(got, "index.tmpl") < 0 {
		t.Fatalf("engine candidates should keep the index.tmpl fallback: %v", got)
	}
}
