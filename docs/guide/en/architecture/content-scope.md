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

## Contract

- Scopes are request-local.
- Core repositories remain generic.
- Plugins attach scope data through public APIs.
- Themes pass the current request context into services when they need scoped reads.
- BaseTheme dynamic archive, single, and taxonomy rendering uses scoped reads, so multilingual filtering works for config-driven content routes without theme-specific plugin code.

This gives plugins meaningful control over query behavior while keeping the core content repository stable.
