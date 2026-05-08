# Theme System Overview

GoPress themes are Go packages that register themselves with the engine. A theme controls frontend rendering, templates, static assets, menu locations, theme settings, and optional demo data.

## Main Features

- `BaseTheme` runtime for rewrite resolution, template fallback, SEO injection, and common helpers.
- Theme-declared content types from `theme.toml`.
- WordPress-like template hierarchy.
- Built-in fallback templates for archives, singles, and taxonomy pages.
- Standard frontend hook slots for plugins.
- Menu locations and language-aware menu rendering.
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

New themes should prefer the `BaseTheme + gin.H` path unless they have a strong reason to maintain a custom PageService.

## Theme and Plugin Boundary

Themes should expose semantic hook slots and use core helpers. Plugins should inject through those slots. Neither side should import the other.

