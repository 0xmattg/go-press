# Content and Taxonomy Scope APIs

GoPress exposes request-local scope APIs for both content and taxonomy queries.
They let plugins add language, tenant, channel, visibility, or preview
constraints without teaching core repositories about a specific extension.

The two scope families are separate because content rows and taxonomy
identities have different query and mutation rules, but they share the same
architecture:

```text
plugin middleware
  -> core AddScope API
  -> request context
  -> admin / REST / BaseTheme / custom PageService
  -> scoped repository and command service
```

When no scope is registered, both APIs preserve ordinary single-language
behavior.

## Content Scope

Register a content filter on the current Gin request:

```go
content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
    return db.Where("visible = ?", true)
})
```

Important content APIs include:

- `content.AddContentScope(c, fn)` — append a request-local GORM filter.
- `content.RequestContext(c)` — bridge Gin state into the standard
  `context.Context` used by core services.
- `content.ScopedDB(c, db)` and `ScopedDBContext(ctx, db)` — apply every scope
  to a session-isolated database handle.
- `Repository.FindBySlugScoped` / context-aware repository methods — resolve a
  detail row inside the same scope used by list queries.
- `EnsureUniqueSlugScoped` — enforce slug uniqueness inside the active scope.

Same-slug language variants rely on this consistency: list, detail, save, and
admin operations must all consume the same request context.

## Taxonomy Scope

Taxonomy scopes carry both an opaque key and a query function:

```go
taxonomy.AddScope(c, taxonomy.Scope{
    Key: "variant-a",
    Apply: func(db *gorm.DB) *gorm.DB {
        return db.Where("taxonomies.id IN (?)", visibleTaxonomyIDs)
    },
})
```

Core never interprets `Scope.Key`. An extension may use it to associate a new
taxonomy row with the same variant that constrained the request.

The public taxonomy scope surface includes:

- `taxonomy.AddScope(c, scope)` — add a scope to a Gin request and its standard
  request context.
- `taxonomy.WithScope(ctx, scope)` — add a scope without Gin.
- `taxonomy.RequestContext(c)` — retrieve the standard context consumed by
  repositories and commands.
- `taxonomy.Scopes(ctx)` and `ScopeKey(ctx)` — inspect the generic scope chain;
  the key remains extension-owned.
- `taxonomy.ScopedDB` / `ScopedDBContext` — apply the complete scope chain.
- `Repository.WithContext(ctx)` — clone a taxonomy repository for a request.

Scoped repository reads cover:

- term lookup by slug;
- taxonomy lookup by ID or type plus slug;
- flat lists and hierarchical trees;
- live content-reference counts;
- a content row's taxonomy relationships.

BaseTheme taxonomy archives, archive filter data, content badges, REST term
resolution, admin taxonomy lists, and content-form selectors all use these
context-aware paths.

## Scope-Safe Taxonomy Writes

`taxonomy.CommandService` is the single mutation boundary. Admin and extension
workflows pass `taxonomy.RequestContext(c)` into it for create, update, delete,
and relationship changes.

The service validates:

- the taxonomy type is registered;
- names and slugs are present;
- a slug is unique inside the active scope;
- a hierarchical parent belongs to the same taxonomy type and scope and does
  not create a cycle;
- every submitted relationship ID belongs to an allowed taxonomy type and the
  active scope;
- each mutation completes transactionally before observers are notified.

These checks are domain validation, not authorization. HTTP/admin transports
must still enforce the operation's `taxonomy.read`, `taxonomy.create`,
`taxonomy.update`, or `taxonomy.delete` capability before calling the command
service.

## Theme Consumption

Themes using BaseTheme's config-driven routes need no special scope code. A
custom theme service should embed `coreTheme.BasePageService` and clone it for
each request:

```go
func (h *Handler) ProductsList(c *gin.Context) {
    service := h.pageService.ForRequest(c)
    data, _ := service.GetProductsData()
    c.HTML(http.StatusOK, "products", data)
}
```

`ForRequest(c)` carries both content and taxonomy contexts. Custom detail
lookups should use scoped repository methods rather than creating an unscoped
repository from the raw database handle.

## Extension and Admin Integration

The bundled multilingual plugin demonstrates the complete composition without
core/plugin coupling:

1. Early middleware determines the canonical request language.
2. It registers a content scope and, for configured taxonomy types, a taxonomy
   scope.
3. Core admin lists, selectors, BaseTheme, REST handlers, SEO, and command
   services consume those generic scopes.
4. `admin.content_list.tabs` and `admin.taxonomy_list.tabs` provide language
   tabs without core knowing what a language is.
5. Generic taxonomy mutation notifications let the plugin maintain its own
   translation-group records.

See the [Multilingual Plugin](../plugins/multilang.md) for the Category/Tag
translation policy and URL behavior.

## Contract Checklist

- Scopes are request-local and must not leak between requests.
- Apply scopes to both list and detail reads.
- Pass the same standard context into mutation services.
- Never treat a frontend filter as authorization; enforce RBAC at the transport
  and ownership/type/scope invariants in the domain service.
- Themes and plugins communicate only through core scope, repository, hook,
  command, and template-helper contracts.
- With no registered scope, existing single-language data and URLs retain their
  historical behavior.
