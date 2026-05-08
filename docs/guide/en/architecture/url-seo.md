# URL and SEO

GoPress treats URLs and SEO metadata as framework-level concerns. Themes provide templates and content presentation; core owns rewrite resolution, canonical URLs, sitemap output, redirects, and SEO metadata assembly.

## Rewrite Engine

The rewrite engine resolves incoming URLs against registered content types, taxonomy archives, static pages, and custom theme routes. Theme-declared content types can define archive behavior and rewrite slugs in `theme.toml`.

Example:

```toml
[[content_types]]
name = "product"
has_archive = true
rewrite_slug = "products"
```

This produces archive and single-content URLs such as `/products` and `/products/example-product`.

## SEO Builder

The SEO builder creates page-level metadata for:

- Home pages.
- Archive pages.
- Single content pages.
- Taxonomy archives.

Site-wide values such as `site_name`, `site_description`, and `site_icon` are applied as final fallbacks from system settings. `site_icon` is rendered as both favicon and Apple touch icon links when present.

## Plugin Overrides

The `seo.content.meta` filter allows plugins to modify SEO output after core builds the default metadata. The built-in `seo-extras` plugin uses this to provide per-content SEO title, description, Open Graph image, and robots overrides.

## Sitemap and Redirects

Sitemap generation reads registered content, taxonomy URLs, and route transformers. Redirect rules are stored separately and are resolved before normal rewrite handling.
