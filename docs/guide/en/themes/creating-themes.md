# Creating Themes

This guide describes the recommended shape of a GoPress theme compiled into GoPress. For themes without Go code, distributed as a `.zip`, see [Theme Packages](theme-packages.md).

## Minimal Theme

```go
// themes/my-theme/meta.go
package mytheme

import "embed"

//go:embed theme.toml
var themeTOML string

// themeFiles carries the theme's own files inside the binary.
//
//go:embed templates static locales
var themeFiles embed.FS
```

```go
// themes/my-theme/theme.go
package mytheme

import (
    "html/template"
    "path/filepath"

    "github.com/gin-gonic/gin"
    "go-press/core"
    coreTheme "go-press/core/theme"
)

func init() {
    core.RegisterThemeWithMeta("my-theme", themeTOML, func(engine *core.Engine, themeDir string) coreTheme.Theme {
        return New(engine, themeDir)
    })
}

type MyTheme struct {
    coreTheme.BaseTheme
    engine *core.Engine
}

func New(engine *core.Engine, themeDir string) *MyTheme {
    t := &MyTheme{engine: engine}
    t.InitBaseFS(engine, themeDir, themeFiles, themeTOML, nil)
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

No manual `cmd/server/main.go` edit is required. Drop the folder into `themes/`, make sure it has both `theme.toml` and at least one non-test `.go` file at its root, then re-run `gopress serve`. The autoload package is regenerated and the new theme's `init()` registers itself with `core.RegisterThemeWithMeta` at startup. See [Getting Started > Installation](../getting-started/installation.md) for details.

### Theme Files in the Binary

`InitBaseFS` hands BaseTheme the theme's own files. Page templates, the template hierarchy, `/static/*`, locales, the admin settings page and demo data are all read from them:

- With `server.mode = "release"`, only the embedded copy is used, so the binary does not need the theme directory.
- In any other mode, the theme directory on disk is used as a whole when it exists, so template and CSS edits show without rebuilding.
- Embed every runtime directory the theme has (`templates`, `static`, `locales`, `demo`). `go:embed` skips files whose names start with `.` or `_`; prefix the pattern with `all:` if the theme needs such files.
- Core finds the admin settings page at `templates/admin/theme_settings.tmpl` and demo data at `demo/data/seed.toml`; no Go method is needed for either.
- `RegisterThemeWithMeta` lets the installer list the theme from the embedded `theme.toml`.

Themes that call `InitBase(engine, themeDir, themeTOML, funcs)` instead keep reading `TemplateDir()`, `StaticDir()` and the theme directory from disk.

### Reporting Build Errors

A theme whose factory fails does not stop the site: core skips it, logs the error, and lists it as failed on the admin Themes page. Register with `core.RegisterThemeE` to return the error instead of panicking:

```go
func init() {
    core.RegisterThemeE("my-theme", core.ThemeSource{TOML: themeTOML},
        func(engine *core.Engine, themeDir string) (coreTheme.Theme, error) {
            return New(engine, themeDir) // New returns (*MyTheme, error)
        })
}
```

Factories registered with `RegisterTheme` or `RegisterThemeWithMeta` keep working; a panic in them is recovered the same way.

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

The home page uses the first of the page bundles `front-page`, `home` and `index`. "Not found" responses (status 404) use the page bundle `404`, or plain text when there is none. A route the theme registers itself, such as `AddRoute("GET", "/")`, takes priority.

Inside templates, prefer core URL helpers:

```gotemplate
<a href="{{archiveURL "product"}}">Products</a>
<a href="{{contentURL . "product"}}">{{.Title}}</a>
<a href="{{taxonomyURL "category" .Slug}}">{{.Name}}</a>
```

`archiveURL`, `contentURL`, and `taxonomyURL` consult the rewrite registry, so a later `rewrite_slug` change or content-type rename does not require template edits.

Dynamic archive pages also honor query-string filters for taxonomies declared on the content type. For example, a `post` type with `taxonomies = ["category", "tag"]` can be filtered with `/blog?category=industry-news` or `/blog?tag=cleanroom`. Query parameters for taxonomies not registered on that content type are ignored.

Treat those query-string filters as compatibility or non-indexable UI filters. Links to indexable term landing pages must use `taxonomyURL`, optionally wrapped with `langPrefixURL`, so internal links, taxonomy canonicals, and sitemap entries all point to `/category/{term}` or `/tag/{term}` consistently.

Taxonomy rows supplied to BaseTheme are request-scoped. When a plugin provides
independent term translations, `.Name`, `.Slug`, parent relationships, and
`.URL` already describe the current-language identity. Themes must not look up
an unscoped term by ID or attempt to map translation groups themselves. Keep
the stable `category` / `tag` path bases and let `taxonomyURL` plus
`langPrefixURL` compose the canonical URL.

For navigation active state, compare the current request URL with the menu item URL through core:

```gotemplate
{{with menuByLocation "header"}}
  {{range .Items}}
    <a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a>
  {{end}}
{{end}}
```

Avoid hard-coded checks such as `.ActivePage == "products"` in reusable themes. Menu labels, content type names, and rewrite slugs are configuration, not theme code contracts.

## Read-only Data Functions

BaseTheme pages hand templates their own data: `.Items` on archives, `.Item` on single pages, `.RecentPosts` everywhere. To show other content, such as six products on the home page or a page body inside a section, call the core data functions. They need no Go code and work in every theme:

```gotemplate
{{range contentList .Ctx "product" 6 "order" "sort_order asc"}}
  <a href="{{langPrefixURL $.Ctx .URL}}">{{.Title}}</a>
{{end}}

{{with contentBySlug .Ctx "page" "about"}}{{safeHTML .Content}}{{end}}

{{range taxonomyTerms .Ctx "category" "hide_empty" true}}
  <a href="{{.URL}}">{{.Name}} ({{.Count}})</a>
{{end}}
```

| Function | Returns |
|---|---|
| `contentList CTX TYPE LIMIT [KEY VALUE]...` | Published items of one content type |
| `contentBySlug CTX TYPE SLUG` | One published item, or nothing (use `with`) |
| `taxonomyTerms CTX TAXONOMY [KEY VALUE]...` | Terms of one taxonomy |

- `CTX` is the request: `.Ctx`, or `$` for the whole page data. Inside `range` and `with`, `.` is the current item, so write `$.Ctx` or `$`.
- `LIMIT` is required. It accepts a number or a numeric string, so it can come straight from a setting: `contentList .Ctx "product" (settingOr .Settings "home_products_max" "6")`.

`contentList` options:

| Key | Value |
|---|---|
| `order` | A field and an optional direction, such as `"published_at desc"` or `"title asc"`. Fields: `published_at`, `created_at`, `updated_at`, `title`, `sort_order`; the direction defaults to `desc`. Without `order`, items sort like the archive page: by `sort_order` ascending when the type supports it, otherwise newest first. Items with equal values are ordered by ID. |
| `taxonomy`, `term` | Both together: only items in that term, such as `"taxonomy" "product_cat" "term" "pumps"`. The taxonomy must be declared on the content type. |
| `exclude` | A content ID to leave out, for "related items": `contentList .Ctx .Item.Type 3 "exclude" .Item.ID` |
| `offset` | 0–50 items to skip, for a lead story followed by a list |
| `meta_key`, `meta_value` | Both together: only items whose meta field equals the value, such as `"meta_key" "featured" "meta_value" "1"` |

`taxonomyTerms` options: `limit` (1–50, default 50), `order` (`"count"`, the default, for most items first; or `"name"`), and `hide_empty` (`true` leaves out terms without published items).

**What they can read.** Only content types that are registered by the active theme and have a public route (an archive, or root-level pages like `page`); only items that are published and whose publish time has passed. The request's scopes apply, so with a multilingual plugin the results are in the current language. In short, nothing a visitor could not open themselves.

**Fields.** Items have the same fields as `.Items` on archive pages: `ID`, `Type`, `Title`, `Slug`, `Content`, `Excerpt`, `ImageURL`, `PublishedAt`, `URL`, `Meta`, every meta field by name, `Categories`, `Tags`, `Category`, and `Terms`, a map from each taxonomy the type declares to its terms (`{{range index .Terms "product_cat"}}`). `Content` is raw HTML; print it with `safeHTML`. `URL` has no language prefix; wrap it with `langPrefixURL`. Terms have `ID`, `Name`, `Slug`, `Taxonomy`, `URL` (with the language prefix), `Count`, `Description` and `ParentID`.

**Limits.** One call returns at most 50 items; a larger limit is lowered to 50. One request makes at most 20 distinct calls; repeating a call with the same arguments, for example in both the header and the footer, reuses the first result and does not count. Each call runs a small fixed number of queries whatever the limit. When a call is over budget or its arguments are wrong (an unknown option, a type without a public route, a taxonomy the type does not declare), it returns an empty result and logs one warning per request; the page still renders. Avoid calling a data function inside `range` over another call's results, which spends one call per item.

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

`uiScript` loads core's front-end interaction script: menus, dialogs, carousels and more, configured with `data-gp-*` attributes. See [Theme Interactions](interactions.md).

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
    t.InitBaseFS(engine, themeDir, themeFiles, themeTOML, template.FuncMap{
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

Declare the settings a theme offers in `theme.toml`. Core then validates and saves them, registers the translatable ones for multilingual sites, fills unsaved ones with their defaults in `.Settings`, and, when the theme has no settings template of its own, builds the settings page from the declaration:

```toml
[[setting_groups]]            # optional: sections of the settings page, in order
key = "hero"
label = "Home hero"
label_key = "settings.group.hero"   # optional: looked up in locales/admin/<lang>.json

[[settings]]
key = "home_hero_title"
group = "hero"
type = "string"
label = "Hero title"
label_key = "settings.home_hero_title"
help = "Shown at the top of the home page."
default = "Build faster"
translatable = true
max_length = 200

[[settings]]
key = "home_hero_{n}_image"   # repeat expands to home_hero_1_image … home_hero_6_image
repeat = 6
group = "hero"
type = "image"
label = "Slide {n} image"

[[settings]]
key = "showcase_layout"
type = "select"
default = "grid"
options = [{ value = "grid", label = "Grid" }, { value = "list", label = "List" }]
```

| Type | Control | Accepted values |
|---|---|---|
| `string` | single line | at most `max_length` characters (default 2048) |
| `text` | multiple lines | at most `max_length` characters (default 20000) |
| `int` | number | a whole number, optionally between `min` and `max` |
| `bool` | checkbox | stored as `1` or `0` |
| `select` | drop-down | one of `options` |
| `image` | path with the media picker | a site path such as `/uploads/a.png` or an `http(s)` URL |
| `url` | URL | a site path, an `http(s)` URL, `#anchor`, `mailto:` or `tel:` |
| `email` | email | one address |

- Keys use lowercase letters, digits and underscores (at most 64 characters). `repeat` (2–24) replaces `{n}` in the key, label and help, and in translated labels.
- Every type accepts an empty value unless `required = true`. `default` may be written as a string, number or boolean.
- `translatable = true` is allowed for `string`, `text`, `url`, `image` and `email` settings.
- Settings are site-wide options: a key means the same value whichever theme declares it, so shared details such as company information carry over when switching themes.
- Keys starting with `plugin_`, `market_` or `demo_imported_` cannot be declared, nor can System Settings keys other than the translatable `site_name` and `site_description`. Plugin settings and site behavior stay on their own pages.
- A theme with an invalid declaration cannot be activated, and its settings page shows the error.

Saving accepts only declared keys. A form that submits an undeclared key, or a value that does not fit its type, saves nothing and lists every problem. `site_name` and `site_description` keep the System Settings validation. The whole form is saved in one transaction.

A Go theme can still ship `templates/admin/theme_settings.tmpl` for a custom layout; it is used instead of the generated page, and saving still follows the declaration. Themes without `[[settings]]` are saved with the older convention, which accepts keys registered as translatable or starting with `home_`, `about_`, `company_`, `social_`, `footer_`, `showcase_`, `site_`, `nav_`, `contact_` or `package_`; declare your settings instead, as this convention will be removed.

Do not create theme-local alternatives for shared site identity. Document titles, default descriptions, and favicons use `site_name`, `site_description`, and `site_icon` from System Settings.

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

Put the seed at `demo/data/seed.toml` and embed `demo` with the other theme files; the admin then offers one-click demo import. A theme that uses `InitBase` implements `DemoSeedPath()` instead:

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
