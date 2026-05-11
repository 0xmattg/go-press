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

Core types such as `post` and `contact_message` should not be redeclared by themes.

## Template Hierarchy

```text
templates/
  layouts/base.tmpl
  front-page.tmpl
  archive-product.tmpl
  single-product.tmpl
  single.tmpl
  archive.tmpl
  404.tmpl
  index.tmpl
```

For a product named `air-shower`, BaseTheme searches:

```text
single-product-air-shower.tmpl
single-product.tmpl
single.tmpl
index.tmpl
```

## Base Layout Contract

Every plugin-friendly theme should declare:

```gotemplate
{{renderHook "theme.head.end" .}}
{{renderHook "theme.body.open" .}}
{{renderHook "theme.footer.end" .}}
{{renderHook "header.nav.after" .}}
```

Use `seoHeadFor`, `settingOr`, `currentLang`, `langPrefixURL`, `menuByLocation`, and the responsive image helpers from the core funcmap instead of implementing theme-local equivalents.

## Demo Data

Implement `DemoSeedPath()` to enable one-click demo import from the admin:

```go
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```

