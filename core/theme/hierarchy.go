package theme

// ResolveTemplate returns an ordered list of template names to try,
// following a WordPress-style template hierarchy.
//
// Examples:
//   Content single: single-product-air-shower.tmpl → single-product.tmpl → single.tmpl → index.tmpl
//   Archive:        archive-product.tmpl → archive.tmpl → index.tmpl
//   Taxonomy:       taxonomy-category-news.tmpl → taxonomy-category.tmpl → taxonomy.tmpl → archive.tmpl → index.tmpl
//   Page:           page-about.tmpl → page.tmpl → single.tmpl → index.tmpl
//   Home:           front-page.tmpl → home.tmpl → index.tmpl
//   404:            404.tmpl → index.tmpl
func ResolveTemplate(contentType, slug, taxonomy, term string, isArchive, isHome, is404 bool) []string {
	var candidates []string

	switch {
	case is404:
		candidates = append(candidates, "404.tmpl")

	case isHome:
		candidates = append(candidates, "front-page.tmpl", "home.tmpl")

	case taxonomy != "":
		// Taxonomy archive
		if term != "" {
			candidates = append(candidates, "taxonomy-"+taxonomy+"-"+term+".tmpl")
		}
		candidates = append(candidates, "taxonomy-"+taxonomy+".tmpl", "taxonomy.tmpl", "archive.tmpl")

	case isArchive:
		if contentType != "" {
			candidates = append(candidates, "archive-"+contentType+".tmpl")
		}
		candidates = append(candidates, "archive.tmpl")

	default:
		// Single content
		if slug != "" && contentType != "" {
			candidates = append(candidates, "single-"+contentType+"-"+slug+".tmpl")
		}
		if contentType != "" {
			candidates = append(candidates, "single-"+contentType+".tmpl")
		}
		candidates = append(candidates, "single.tmpl")
	}

	// Ultimate fallback
	candidates = append(candidates, "index.tmpl")
	return candidates
}
