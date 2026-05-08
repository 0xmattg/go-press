// Package rewrite owns public URL resolution, SEO metadata, redirects, and
// sitemap generation.
//
// The rewrite Engine maps incoming paths to registered content types and
// taxonomy archives. SEOBuilder turns those resolved pages into canonical,
// Open Graph, robots, favicon, and JSON-LD metadata. SitemapGenerator uses the
// same registry and repositories so generated URLs match the front-end routing
// contract exactly.
package rewrite
