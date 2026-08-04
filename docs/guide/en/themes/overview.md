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
- Runtime Core version helper (`goPressVersion`) sourced from `version/version.go`.
- Site-timezone-aware `formatDate` and `formatDateTime` helpers from `BaseFuncMap`.
- Responsive image helpers backed by media variants.
- Demo data import through `DemoDataProvider`.
- Admin theme-card logo through `LogoProvider` — drop a `static/logo.svg`; `BaseTheme` reads it automatically (no Go code) and core sanitizes it before inlining.

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
| `mono-journal` | Mono Journal | Monochrome personal journal and blog. |
| `shop-starter` | Shop Starter | Lightweight single-page reference storefront for Commerce. |

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

Templates should generate content and term links with `archiveURL`, `contentURL`, and `taxonomyURL` instead of hard-coding `/products`, `/services`, `/category`, or similar paths.

Navigation active state should also come from core helpers. Use `isMenuURLActive .Ctx menuURL` against menu item URLs instead of comparing `.ActivePage` to theme-specific content type names or labels. The helper follows the current request URL, rewrite slugs, language prefixes, and detail-page paths.

A custom `PageService` is now cheap: it embeds core scaffolding (`coreTheme.BasePageService`, or `coreTheme.SEOPageService` when the theme renders SEO), so it no longer duplicates data-access or SEO plumbing. New themes can pick either the `BaseTheme + gin.H` path for the quickest start, or a typed `PageService` for type safety — see the [SEO integration guide](seo-integration.md).

## Frontend Extension Slots

Production themes expose stable semantic locations instead of requiring plugins
to inspect final HTML:

```gotemplate
<head>
  ...
  {{renderHook "theme.head.end" .}}
</head>
<body>
  {{renderHook "theme.body.open" .}}
  ...
  <script src="/static/js/main.js"></script>
  {{renderHook "theme.footer.end" .}}
</body>
```

The primary navigation list also ends with
`{{renderHook "header.nav.after" .}}`. Theme CSS should style direct navigation
children rather than every descendant, and mobile code should detect nested
extension menus from DOM structure while keeping `aria-expanded` synchronized.

| Slot | Typical use |
|---|---|
| `theme.head.end` | Verification tags, analytics, preconnect, external CSS. |
| `theme.body.open` | Noscript tags, bootstrap code, site banners. |
| `theme.footer.end` | Deferred scripts, chat, heatmaps. |
| `header.nav.after` | Language and account menus. |

Each slot appears exactly once. Plugins return markup matching its surrounding
semantics and remove their filter handles when deactivated.

## Public Account UI

Themes can render provider-neutral account UI with the core helpers `currentUser`, `isLoggedIn`, `loginURL`, `loginProviderURL`, `logoutURL`, and `loginProviders`. A theme may choose where and how account controls appear, but it must not import or special-case identity plugins such as Google Identity or MetaMask Identity.

Use `loginProviders` to discover enabled sign-in choices and `loginProviderURL` to attach a validated same-site return path to each provider's core-published begin URL. See [Public Accounts and External Identity](../architecture/public-authentication.md#theme-integration) for template examples and cache/security notes.

## Theme and Plugin Boundary

Built-in GoPress themes and themes intended for production use must expose the standard semantic hook slots and use core helpers. Plugins should inject through those slots. Neither side should import the other. The required slots and the repository-level contract test are documented in [Creating Themes](creating-themes.md#base-layout-contract).

Module-level plugin requirements may be declared in `theme.toml`, but runtime
theme code still uses only generic core hooks, helpers, and capabilities.
