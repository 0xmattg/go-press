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

func (t *MyTheme) Name() string        { return "My Theme" }
func (t *MyTheme) Version() string     { return "1.0.0" }
func (t *MyTheme) Description() string { return "Example theme" }
func (t *MyTheme) Author() string      { return "Me" }
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

Core types such as `post` and `contact_message` should not be redeclared by themes. `product` is only an example custom content type; GoPress does not require a theme to provide products, services, or showcases.

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
```

`archiveURL` and `contentURL` consult the rewrite registry, so a later `rewrite_slug` change or content-type rename does not require template edits.

Dynamic archive pages also honor query-string filters for taxonomies declared on the content type. For example, a `post` type with `taxonomies = ["category", "tag"]` can be filtered with `/blog?category=industry-news` or `/blog?tag=cleanroom`. Query parameters for taxonomies not registered on that content type are ignored.

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

Every plugin-friendly theme should declare:

```gotemplate
{{renderHook "theme.head.end" .}}
{{renderHook "theme.body.open" .}}
{{renderHook "theme.footer.end" .}}
{{renderHook "header.nav.after" .}}
```

Use `pageTitleFor`, `seoHeadFor`, `settingOr`, `archiveURL`, `contentURL`, `isMenuURLActive`, `currentLang`, `langPrefixURL`, `menuByLocation`, and the responsive image helpers from the core funcmap instead of implementing theme-local equivalents.

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

## Demo Data

Implement `DemoSeedPath()` to enable one-click demo import from the admin:

```go
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```
