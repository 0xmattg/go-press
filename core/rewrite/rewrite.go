package rewrite

import (
	"strings"

	"go-press/core/content"
)

// ResolvedRoute is the normalized result of front-end URL resolution.
//
// BaseTheme uses this value to choose between archive, single, and taxonomy
// rendering paths. It contains only routing metadata; content lookup remains in
// the content repository so plugins can apply request scopes such as language.
type ResolvedRoute struct {
	ContentType string // e.g. "product"
	Slug        string // e.g. "air-shower"
	IsArchive   bool   // true = listing page, false = single item
	IsTaxonomy  bool
	TaxSlug     string // taxonomy slug when IsTaxonomy is true
	TermSlug    string // term slug when IsTaxonomy is true
	Page        int    // pagination page (0 = not paginated)
}

// Engine resolves and builds public URLs from the active content registry.
//
// The rewrite engine is registry-driven: switching themes can change content
// type archive prefixes without changing router code. It intentionally does not
// hit the database; it only maps path shape to content type, slug, taxonomy, and
// pagination metadata.
type Engine struct {
	registry *content.Registry
}

// NewEngine creates a rewrite engine backed by registry.
func NewEngine(registry *content.Registry) *Engine {
	return &Engine{registry: registry}
}

// Resolve maps a URL path to a content type, slug, and archive flag.
// Examples:
//
//	/products        → ContentType="product", IsArchive=true
//	/products/air-shower → ContentType="product", Slug="air-shower"
//	/blog            → ContentType="post", IsArchive=true
//	/blog/my-post    → ContentType="post", Slug="my-post"
func (e *Engine) Resolve(path string) *ResolvedRoute {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return nil
	}

	parts := strings.SplitN(path, "/", 3)
	prefix := parts[0]

	// Check content type rewrite rules
	for _, typeDef := range e.registry.AllTypes() {
		slug := typeDef.Rewrite.Slug
		if slug == "" {
			slug = typeDef.Name
		}
		if slug != prefix {
			continue
		}

		route := &ResolvedRoute{ContentType: typeDef.Name}
		if len(parts) == 1 {
			// /products → archive
			route.IsArchive = true
		} else {
			// /products/air-shower → single
			route.Slug = parts[1]
			// Check for /page/N on single item — likely pagination
			if len(parts) == 3 && parts[1] == "page" {
				route.IsArchive = true
				route.Page = parsePageNum(parts[2])
			}
		}
		return route
	}

	// Check taxonomy rewrite rules
	for _, taxDef := range e.registry.AllTaxonomies() {
		taxSlug := taxDef.Name
		if taxSlug != prefix {
			continue
		}
		if len(parts) < 2 {
			continue
		}
		return &ResolvedRoute{
			IsTaxonomy: true,
			TaxSlug:    taxDef.Name,
			TermSlug:   parts[1],
		}
	}

	return nil
}

// BuildURL generates a public path for a content item.
//
// When the content type is unknown, the function falls back to /{type}/{slug}
// so callers can still produce a deterministic path during partial setup or
// migration scenarios.
func (e *Engine) BuildURL(contentType, slug string) string {
	typeDef := e.registry.GetType(contentType)
	if typeDef == nil {
		return "/" + contentType + "/" + slug
	}
	prefix := typeDef.Rewrite.Slug
	if prefix == "" {
		prefix = typeDef.Name
	}
	if slug == "" {
		return "/" + prefix
	}
	return "/" + prefix + "/" + slug
}

// BuildArchiveURL generates the archive URL for a content type.
func (e *Engine) BuildArchiveURL(contentType string) string {
	return e.BuildURL(contentType, "")
}

func parsePageNum(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 1
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return 1
	}
	return n
}
