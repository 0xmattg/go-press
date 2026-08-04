# Creating Themes

This guide describes the recommended shape of a GoPress theme.

## Minimal Theme

```go
package mytheme

import (
    "html/template"
    "path/filepath"

    "github.com/gin-gonic/gin"
    "go-press/core"
    coreTheme "go-press/core/theme"
)

func init() {
    core.RegisterTheme("my-theme", func(engine *core.Engine, themeDir string) coreTheme.Theme {
        return New(engine, themeDir)
    })
}

type MyTheme struct {
    coreTheme.BaseTheme
    engine *core.Engine
}

func New(engine *core.Engine, themeDir string) *MyTheme {
    t := &MyTheme{engine: engine}
    t.InitBase(engine, themeDir, nil)
    t.LoadTemplates(t)
    return t
}

// Name, Version, Description, and Author come from the embedded BaseTheme,
// which parses theme.toml as their single source of truth.
func (t *MyTheme) Setup(app coreTheme.App) {}
func (t *MyTheme) ServeHTTP(c *gin.Context) { t.BaseTheme.ServeHTTP(c) }
func (t *MyTheme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }
func (t *MyTheme) TemplateDir() string { return filepath.Join(t.ThemeDir, "templates") }
func (t *MyTheme) StaticDir() string { return filepath.Join(t.ThemeDir, "static") }
```

No manual `cmd/server/main.go` edit is required. Drop the folder into `themes/`, make sure it has both `theme.toml` and at least one non-test `.go` file at its root, then re-run `gopress serve`. The autoload package is regenerated and the new theme's `init()` registers itself with `core.RegisterTheme` at startup. See [Getting Started > Installation](../getting-started/installation.md) for details.

## Theme Metadata

`theme.toml` is required — it both serves as the auto-detection marker (the `gopress` CLI ignores a `themes/<name>/` directory without it) and supplies the content type and menu location declarations consumed by core. Minimum schema:

```toml
[theme]
name = "My Theme"
version = "1.0.0"
description = "Example theme"
author = "Me"

[[content_types]]
name = "product"
label = "Product"
label_plural = "Products"
archive_title_key = "page_title_product"
supports = ["title", "content", "excerpt", "thumbnail", "sort_order"]
taxonomies = ["category", "tag"]
has_archive = true
rewrite_slug = "products"
menu_icon = "blocks"
menu_order = 1

[[menu_locations]]
name = "header"
label = "Header Navigation"
```

Core types such as `post`, `page`, and `contact_message` should not be redeclared by themes. `product` is only an example custom content type; GoPress does not require a theme to provide products, services, or showcases.

Theme versions must be valid semver. `BaseTheme` parses the `[theme]` block and
implements `Name`, `Version`, `Description`, and `Author`; do not repeat those
values in Go methods. This keeps `theme.toml` as the single source used by admin
cards and dependency validation.

For frontend multilingual labels, add `content_type.<name>` entries to the theme locale files. BaseTheme uses those keys for content type badges on taxonomy archives and falls back to `label` when a locale key is missing:

```json
{
  "content_type.product": "Product"
}
```

### Rewrite Slugs And Template Mapping

`rewrite_slug` is the public URL base for a content type. The example above produces:

```text
/products
/products/{content-slug}
```

When the visual template name differs from the content type name, add an explicit `templates` mapping instead of hard-coding routes in Go:

```toml
[[content_types]]
name = "module"
label = "Module"
label_plural = "Modules"
archive_title_key = "page_title_module"
supports = ["title", "content", "excerpt", "thumbnail", "sort_order"]
taxonomies = ["category", "tag"]
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
menu_icon = "blocks"
menu_order = 1
```

This keeps the content model (`module`), public URLs (`/modules`), and presentation templates (`products`, `product-detail`) independently configurable. It is useful when a theme reuses an existing layout for a differently named business concept. `archive_title_key` points to a theme locale key used for archive `<title>` and Open Graph title, so multilingual pages do not fall back to the static `label_plural` text.

### Frontend-authorable Content Types

An authenticated frontend workflow can opt a theme-defined type into Core's
generic submission policy:

```toml
[content_types.public_submission]
enabled = true
roles = ["subscriber", "contributor"]
default_status = "pending"
allow_update_own = true
allow_delete_own = true
```

The declaration supplies temporary type-scoped RBAC capabilities while the
theme is active. Routes and UI remain theme-owned; writes must go through
`theme.PublicSubmissionApp.PublicSubmissionService()` so Core can enforce the
active account, allowed role, capability, ownership, status, validation, slug,
and rate-limit rules. Never bind the service's trusted `PublishImmediately`
field to browser input. See
[Public Content Submission](../architecture/public-content-submission.md).

## Template Hierarchy

```text
templates/
  layouts/base.tmpl
  partials/header.tmpl
  pages/home.tmpl
  pages/products.tmpl
  pages/product-detail.tmpl
  pages/archive.tmpl
  pages/single.tmpl
```

BaseTheme compiles `templates/pages/*.tmpl` as page bundles. For a `product` detail page named `air-shower`, it first tries page bundle names derived from the route and content type:

```text
single-product-air-shower
single-product
product-detail
products-detail
<templates.single from theme.toml>
single
```

For a `product` archive with `rewrite_slug = "products"`, it tries:

```text
archive-product
products
product
<templates.archive from theme.toml>
archive
```

If no page bundle matches, BaseTheme falls back to the classic root-template hierarchy (`archive-product.tmpl`, `single-product.tmpl`, `archive.tmpl`, `single.tmpl`, `index.tmpl`) and finally to built-in fallback templates.

Inside templates, prefer core URL helpers:

```gotemplate
<a href="{{archiveURL "product"}}">Products</a>
<a href="{{contentURL . "product"}}">{{.Title}}</a>
<a href="{{taxonomyURL "category" .Slug}}">{{.Name}}</a>
```

`archiveURL`, `contentURL`, and `taxonomyURL` consult the rewrite registry, so a later `rewrite_slug` change or content-type rename does not require template edits.

Dynamic archive pages also honor query-string filters for taxonomies declared on the content type. For example, a `post` type with `taxonomies = ["category", "tag"]` can be filtered with `/blog?category=industry-news` or `/blog?tag=cleanroom`. Query parameters for taxonomies not registered on that content type are ignored.

Treat those query-string filters as compatibility or non-indexable UI filters. Links to indexable term landing pages must use `taxonomyURL`, optionally wrapped with `langPrefixURL`, so internal links, taxonomy canonicals, and sitemap entries all point to `/category/{term}` or `/tag/{term}` consistently.

For navigation active state, compare the current request URL with the menu item URL through core:

```gotemplate
{{with menuByLocation "header"}}
  {{range .Items}}
    <a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a>
  {{end}}
{{end}}
```

Avoid hard-coded checks such as `.ActivePage == "products"` in reusable themes. Menu labels, content type names, and rewrite slugs are configuration, not theme code contracts.

## Base Layout Contract

Every built-in GoPress theme and every third-party theme intended for production use **must** expose the standard frontend slots. A deliberately closed, minimal theme may omit them, but it should then be treated as incompatible with generic frontend plugins.

```gotemplate
{{define "base"}}<!DOCTYPE html>
<html lang="{{currentLang .Ctx}}">
<head>
  ...
  {{renderHook "theme.head.end" .}}
</head>
<body>
  {{renderHook "theme.body.open" .}}
  {{template "header" .}}
  <main>{{template "content" .}}</main>
  {{template "footer" .}}
  <script src="/static/js/main.js"></script>
  {{renderHook "theme.footer.end" .}}
</body>
</html>{{end}}
```

The navigation slot belongs to the Header contract. Put it at the end of the primary navigation list and pass the complete current template data:

```gotemplate
{{define "header"}}
<header class="site-header">
  <nav class="site-nav" aria-label="Primary navigation">
    <ul>
      {{with menuByLocation "header"}}
        {{range .Items}}
        <li><a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a></li>
        {{end}}
      {{end}}
      {{renderHook "header.nav.after" .}}
    </ul>
  </nav>
</header>
{{end}}
```

Use `pageTitleFor`, `seoHeadFor`, `settingOr`, `archiveURL`, `contentURL`, `taxonomyURL`, `isMenuURLActive`, `currentLang`, `langPrefixURL`, `menuByLocation`, and the responsive image helpers from the core funcmap instead of implementing theme-local equivalents.

`goPressVersion` returns the running Core version from the single source of truth in `version/version.go`. Use it for version labels in theme-rendered documentation or diagnostic UI instead of copying a version string into theme settings or templates:

```gotemplate
<span>GoPress Core v{{goPressVersion}}</span>
```

### Navigation Extension Styling And Interaction

Scope theme navigation styles to the structure owned by the theme so they do not overwrite nested menus contributed by extensions:

```css
/* Recommended: style only direct primary-navigation children. */
.site-nav > ul > li > a { /* ... */ }

/* Avoid: these also overwrite nested extension menus. */
.site-nav ul { /* ... */ }
.site-nav a { /* ... */ }
```

On mobile, a top-level extension item may contain a direct child `<ul>`. Detect submenus from the DOM structure rather than plugin-specific class names, keep `aria-expanded` synchronized, and collapse open submenus when the primary navigation closes. Themes must not import plugin packages, read plugin-private settings, or depend on implementation classes such as `.gp-lang-*`.

### Standard Frontend Contract Checklist

| Check | Requirement |
|---|---|
| `theme.head.end` | Exactly once, immediately before `</head>` |
| `theme.body.open` | Exactly once, immediately after `<body>` opens |
| `theme.footer.end` | Exactly once, after theme scripts and before `</body>` |
| `header.nav.after` | Exactly once, inside the primary-navigation `<ul>`, receiving `.` |
| CSS scope | Direct-child selectors for primary navigation; nested extension menus remain untouched |
| Mobile behavior | Generic submenu support with synchronized `aria-expanded` state |
| Architecture | Core hooks only; no knowledge of a specific plugin implementation |

The repository-level `internal/contracts/theme_contracts_test.go` test automatically discovers every built-in theme containing `theme.toml` and validates the four standard hooks, their template-data argument, and their semantic positions. New themes require no manual registration, but must pass:

```bash
go test ./internal/contracts
```

## Dates And Site Timezone

Use `formatDate` and `formatDateTime` from `BaseFuncMap()` when rendering content publish times. These helpers read `site_timezone` from System Settings, convert UTC timestamps from the database into the site timezone, and then format the value for templates.

If a theme needs a custom date formatter, convert through `engine.SiteLocation()` before formatting:

```go
func New(engine *core.Engine, themeDir string) *MyTheme {
    t := &MyTheme{engine: engine}
    t.InitBase(engine, themeDir, template.FuncMap{
        "formatLongDate": func(tm *time.Time) string {
            if tm == nil {
                return ""
            }
            return tm.In(engine.SiteLocation()).Format("2006-01-02")
        },
    })
    t.LoadTemplates(t)
    return t
}
```

This keeps the contract consistent across the admin, frontend, and sitemap path: inputs are parsed in the site timezone, stored as UTC, and displayed in the site timezone. Existing sites without `site_timezone` fall back to the server local timezone until an explicit value is saved.

## Recommended Directory Structure

```text
themes/my-theme/
  theme.go
  theme.toml
  handlers.go                 # optional custom routes
  services.go                 # optional typed page services
  functions.go                # optional template helpers
  translatable.go             # optional translatable option registration
  locales/
    en.json
    zh.json
  demo/data/seed.toml         # optional demo data
  static/
    logo.svg
    css/style.css
    js/main.js
  templates/
    layouts/
    partials/
    pages/
```

## Theme Settings

Themes can expose settings for presentation and content such as hero media,
brand copy, social links, and calls to action. Use section-oriented prefixes
such as `home_`, `about_`, `social_`, and `footer_` so the engine recognizes and
persists theme-owned keys.

Do not create theme-local alternatives for shared site identity. Document
titles, default descriptions, and favicons use `site_name`, `site_description`,
and `site_icon` from System Settings. Image settings should open the shared
media picker rather than requiring operators to paste opaque paths.

## Recommended BaseTheme Path

For the lowest maintenance cost, delegate the frontend catch-all to BaseTheme:

```go
func (t *MyTheme) ServeHTTP(c *gin.Context) {
    t.BaseTheme.ServeHTTP(c)
}
```

Home, archive, taxonomy, and single pages then receive routing, scoped queries,
fallback templates, and SEO injection from core. A theme that needs custom,
typed assembly can embed `coreTheme.BasePageService` or
`coreTheme.SEOPageService` instead of copying repository and SEO plumbing.

Type safety does not require abandoning BaseTheme. A handler may add typed view
models to its `gin.H` data while retaining core routing and metadata:

```go
data := gin.H{
    "Title":    "Products",
    "Products": productViews, // []ProductView
}
```

## Demo Data

Implement `DemoSeedPath()` to enable one-click demo import from the admin:

```go
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```

## Required Plugins

If a theme needs certain plugins to work, declare them by slug in `theme.toml`'s
`[requires]` block. Core pre-checks them on theme switch and auto-activates any
that are compiled in but inactive. See [Dependencies and Versioning](dependencies.md).

## Admin Card Logo

The admin **Themes** page shows an icon next to each theme name. The convention is minimal: **just drop a `static/logo.svg`** — `BaseTheme` implements the optional `LogoProvider.LogoSVG()` and reads that file automatically, so no Go code is required.

- Use a square icon with `viewBox="0 0 48 48"` (the card renders it at ~34px).
- Core runs `content.SanitizeSVG` (stripping `<script>`, `on*` handlers, `javascript:` URIs, …) before inlining, so even a third-party theme's logo cannot smuggle script into the admin origin.
- With no such file, the card simply shows no icon.
- To generate the logo dynamically, override `LogoSVG() string` on the theme.

## Authenticated Comments And Profile Routes

For public submissions, comments, account pages, bookmarks, or other
authenticated theme workflows:

- Use core's provider-neutral `currentUser`, `loginURL`, `loginProviders`,
  `loginProviderURL`, `PublicSubmissionApp`, `CommentApp`, and
  `PublicAuthorizationApp` contracts.
- Declare a required identity plugin only in `theme.toml`; never import it or inspect its private options/provider ID at runtime.
- Protect every state-changing route with same-origin validation and a concrete `resource.action` capability.
- Validate submitted content/comment IDs against their type, target, ownership, and parent relationship.
- Keep own-account pages on a fixed route, set `Cache-Control: private, no-store`, and never expose another user's email.
- Add tests proving an unauthenticated or permissionless role is rejected and an authorized role succeeds.

See [Public Content Submission](../architecture/public-content-submission.md)
and [Comments and Replies](../architecture/comments.md).
