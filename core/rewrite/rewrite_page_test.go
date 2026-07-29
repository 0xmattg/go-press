package rewrite

import (
	"testing"

	"go-press/core/content"
)

func pageTestRegistry() *content.Registry {
	reg := content.NewRegistry()
	reg.RegisterType(content.ContentTypeDef{
		Name:       "post",
		HasArchive: true,
		Rewrite:    content.RewriteRule{Slug: "blog"},
	})
	reg.RegisterType(content.ContentTypeDef{
		Name:    "page",
		Rewrite: content.RewriteRule{Rootless: true},
	})
	return reg
}

func TestBuildTaxonomyURL(t *testing.T) {
	e := NewEngine(pageTestRegistry())
	if got := e.BuildTaxonomyURL("category", "cleanroom-standards"); got != "/category/cleanroom-standards" {
		t.Fatalf("BuildTaxonomyURL() = %q, want /category/cleanroom-standards", got)
	}
}

func TestResolveRootlessPage(t *testing.T) {
	e := NewEngine(pageTestRegistry())

	t.Run("single segment resolves to page single", func(t *testing.T) {
		route := e.Resolve("/about")
		if route == nil {
			t.Fatal("Resolve(/about) = nil, want page route")
		}
		if route.ContentType != "page" || route.Slug != "about" {
			t.Fatalf("got type=%q slug=%q, want page/about", route.ContentType, route.Slug)
		}
		if route.IsArchive || route.IsTaxonomy {
			t.Fatalf("page route should be a single item, got archive=%v taxonomy=%v", route.IsArchive, route.IsTaxonomy)
		}
	})

	t.Run("archive prefix wins over page fallback", func(t *testing.T) {
		route := e.Resolve("/blog")
		if route == nil || route.ContentType != "post" || !route.IsArchive {
			t.Fatalf("Resolve(/blog) = %+v, want post archive", route)
		}
	})

	t.Run("prefixed single still resolves normally", func(t *testing.T) {
		route := e.Resolve("/blog/hello")
		if route == nil || route.ContentType != "post" || route.Slug != "hello" {
			t.Fatalf("Resolve(/blog/hello) = %+v, want post/hello single", route)
		}
	})

	t.Run("nested path is not treated as a page in this version", func(t *testing.T) {
		if route := e.Resolve("/about/team"); route != nil {
			t.Fatalf("Resolve(/about/team) = %+v, want nil (nested pages unsupported)", route)
		}
	})

	t.Run("type-name prefix must not give a rootless page a second URL", func(t *testing.T) {
		// /page/about would be a duplicate of the canonical /about; it must not
		// resolve.
		if route := e.Resolve("/page/about"); route != nil {
			t.Fatalf("Resolve(/page/about) = %+v, want nil (no duplicate page URL)", route)
		}
	})
}

func TestBuildURLRootless(t *testing.T) {
	e := NewEngine(pageTestRegistry())

	if got := e.BuildURL("page", "about"); got != "/about" {
		t.Fatalf("BuildURL(page, about) = %q, want /about", got)
	}
	if got := e.BuildURL("post", "hello"); got != "/blog/hello" {
		t.Fatalf("BuildURL(post, hello) = %q, want /blog/hello", got)
	}
	if got := e.BuildURL("page", ""); got != "/" {
		t.Fatalf("BuildURL(page, \"\") = %q, want /", got)
	}
}
