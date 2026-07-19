# Theme System Overview

GoPress themes are Go packages that register themselves with the engine. A theme controls frontend rendering, templates, static assets, menu locations, theme settings, and optional demo data.

## Main Features

- `BaseTheme` runtime for rewrite resolution, template fallback, SEO injection, and common helpers.
- Theme-declared content types from `theme.toml`, including rewrite slugs and optional page-template mapping.
- WordPress-like template hierarchy.
- Built-in fallback templates for archives, singles, and taxonomy pages.
- Standard frontend hook slots for plugins.
- Menu locations and language-aware menu rendering.
- URL-based active menu helper from the core funcmap.
- Site-timezone-aware `formatDate` and `formatDateTime` helpers from `BaseFuncMap`.
- Responsive image helpers backed by media variants.
- Demo data import through `DemoDataProvider`.

## Built-in Themes

| Slug | Name | Type |
|---|---|---|
| `modern-company` | Modern Company | Company website. |
| `financial-news` | Financial News | Finance/news portal. |
| `atelier-slate` | Atelier Slate | Digital studio. |
| `axis-form` | Axis Form | Architecture and interior portfolio. |
| `florafi` | FloraFi | Stablecoin and fintech product site. |
| `civic-estate` | Civic Estate | Commercial real estate. |
| `terra-trail` | Terra Trail | Outdoor travel. |
| `go-press-landing` | GoPress Landing | SaaS landing page. |

## Dynamic Content Routing

Core does not assume a site must have `product`, `service`, or `showcase`. The only always-registered editorial content type is `post`; themes add their own content types through `theme.toml`.

For each registered content type, `rewrite_slug` defines the public archive/detail URL shape, and optional `templates` selects the theme page bundles used for archive and detail rendering:

```toml
[[content_types]]
name = "module"
label = "Module"
label_plural = "Modules"
archive_title_key = "page_title_module"
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
```

With that configuration, `/modules` and `/modules/{slug}` resolve to the `module` content type while reusing `templates/pages/products.tmpl` and `templates/pages/product-detail.tmpl`. If `templates` is omitted, BaseTheme tries conventional names derived from the content type and rewrite slug before falling back to generic archive/single templates and built-in fallback pages.

Templates should generate content links with `archiveURL` and `contentURL` instead of hard-coding `/products`, `/services`, or similar paths.

Navigation active state should also come from core helpers. Use `isMenuURLActive .Ctx menuURL` against menu item URLs instead of comparing `.ActivePage` to theme-specific content type names or labels. The helper follows the current request URL, rewrite slugs, language prefixes, and detail-page paths.

New themes should prefer the `BaseTheme + gin.H` path unless they have a strong reason to maintain a custom PageService.

## Public Account UI

Themes can render provider-neutral account UI with the core helpers `currentUser`, `isLoggedIn`, `loginURL`, `logoutURL`, and `loginProviders`. A theme may choose where and how account controls appear, but it must not import or special-case identity plugins such as Google Identity or MetaMask Identity.

Use `loginProviders` to discover enabled sign-in choices and link through each provider's core-published begin URL. See [Public Accounts and External Identity](../architecture/public-authentication.md#theme-integration) for template examples and cache/security notes.

## Theme and Plugin Boundary

Themes should expose semantic hook slots and use core helpers. Plugins should inject through those slots. Neither side should import the other.
