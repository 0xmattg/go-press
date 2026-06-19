# URL and SEO

GoPress treats URLs and SEO metadata as framework-level concerns. Themes provide templates and content presentation; core owns rewrite resolution, canonical URLs, sitemap output, redirects, and SEO metadata assembly.

## Rewrite Engine

The rewrite engine resolves incoming URLs against registered content types, taxonomy archives, static pages, and custom theme routes. Theme-declared content types define archive behavior, rewrite slugs, and optional template mapping in `theme.toml`.

Example:

```toml
[[content_types]]
name = "product"
has_archive = true
rewrite_slug = "products"
```

This produces archive and single-content URLs such as `/products` and `/products/example-product`.

`product` is not special to core. A theme can declare any content type name and any public URL base:

```toml
[[content_types]]
name = "module"
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
```

In this example the data model is `module`, the public URLs are `/modules` and `/modules/{slug}`, and the archive/detail pages reuse `templates/pages/products.tmpl` and `templates/pages/product-detail.tmpl`. This keeps routing, admin CRUD, REST API exposure, sitemap entries, and frontend rendering driven by the same registry entry.

Theme templates should use registry-aware helpers for internal content links:

```gotemplate
{{archiveURL "module"}}
{{contentURL . "module"}}
```

`archiveURL` returns the current archive URL for a content type. `contentURL` uses an item's existing `URL` field when present, otherwise combines the item's `Type`/`Slug` with the rewrite registry and falls back to the supplied type.

## SEO Builder

The SEO builder creates page-level metadata for:

- Home pages.
- Archive pages.
- Single content pages.
- Taxonomy archives.

Site-wide values such as `site_name`, `site_description`, and `site_icon` are applied as final fallbacks from system settings. `site_icon` renders `/favicon.ico` first, then a typed image icon and Apple touch icon when present.

## Plugin Overrides

The `seo.content.meta` filter allows plugins to modify SEO output after core builds the default metadata. The built-in `seo-extras` plugin uses this to provide per-content SEO title, description, Open Graph image, and robots overrides.

## Sitemap and Redirects

Sitemap generation reads registered content types and their rewrite configuration, taxonomy URLs, and route transformers. Redirect rules are stored separately and are resolved before normal rewrite handling.

`/sitemap.xml` is served dynamically by the active site process and supports both `GET` and `HEAD`. The admin "Generate Sitemap" action writes a static copy to the active site's `public/` directory, for example `sites/example.com/public/sitemap.xml`, so multiple sites can share one application root without overwriting each other's generated files. Future site-scoped public artifacts such as `robots.txt` or `llms.txt` should use the same directory.
