# Content Scope API

The Content Scope API lets plugins and themes add request-level filters to content queries without coupling themselves to repository internals. It is used by the multilingual plugin to restrict content by language, but the mechanism is generic.

## Why It Exists

Many CMS extensions need to alter content visibility:

- Filter content by language.
- Hide private variants.
- Apply tenant or channel constraints.
- Restrict preview content to authenticated users.

Instead of making every repository method know about every plugin, GoPress stores scope information in the request context.

## Typical Flow

```go
content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
    return db.Where("visible = ?", true)
})

db := content.ScopedDB(c, baseDB)
```

Repository methods that use `ScopedDB` receive the active filters automatically.

The request path is deliberately one-way:

```text
plugin middleware
  -> content.AddContentScope(gin.Context, scope)
  -> scope stored in the request context
  -> BaseTheme / PageService
  -> content.ScopedDB or FindBySlugScoped
```

## Core APIs

- `content.AddContentScope(c, fn)` registers a GORM scope on the current
  `gin.Context`.
- `content.ScopedDB(c, db)` returns a session-isolated database handle with all
  request scopes applied.
- BaseTheme archive, single, and taxonomy paths use scoped reads automatically.
- A custom service embeds `coreTheme.BasePageService` and calls
  `ForRequest(c)`. The returned clone keeps the request in `ReqCtx`, allowing
  detail lookups to use `FindBySlugScoped` as well as list queries.
- Admin lists use `admin.Service.ListContentScoped`, so a plugin can apply the
  same visibility rule to frontend and administrative queries.

This detail matters when different scoped records share a slug. For example,
language variants can both use `hepa-filter`; an unscoped detail lookup could
otherwise return the default-language row.

## Plugin and Theme Example

Plugin middleware registers the condition:

```go
e.Hooks.AddAction("middleware.early", func(_ context.Context, args ...interface{}) {
    r := args[0].(*gin.Engine)
    r.Use(func(c *gin.Context) {
        content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
            return db.Where("visible = ?", true)
        })
        c.Next()
    })
}, 5)
```

A custom theme service consumes it without knowing which plugin supplied it:

```go
func (h *Handler) ProductsList(c *gin.Context) {
    service := h.pageService.ForRequest(c)
    data, _ := service.GetProductsData()
    c.HTML(http.StatusOK, "products", data)
}
```

Themes using BaseTheme's config-driven dynamic routes do not need this custom
handler; core already applies the request scopes.

## Contract

- Scopes are request-local.
- Core repositories remain generic.
- Plugins attach scope data through public APIs.
- Themes pass the current request context into services when they need scoped reads.
- BaseTheme dynamic archive, single, and taxonomy rendering uses scoped reads, so multilingual filtering works for config-driven content routes without theme-specific plugin code.
- With no registered scope, `ScopedDB` returns the original query behavior.
- Scope providers and consumers communicate only through core APIs; neither side
  needs a plugin- or theme-specific branch.

This gives plugins meaningful control over query behavior while keeping the core content repository stable.
