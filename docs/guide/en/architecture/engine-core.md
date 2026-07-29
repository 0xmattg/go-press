# Engine Core

The GoPress engine is the runtime container for the CMS. It wires storage,
content repositories, rewrite rules, SEO rendering, hooks, cache, workers,
admin and API routes, installer routes, and the active frontend theme.

## Main Modules

| Module | Responsibility |
|---|---|
| `core/engine.go` | Engine lifecycle, route setup, shutdown, and the shared `App` capability surface. |
| `core/bootstrap.go` | One-call build and bootstrap orchestration. |
| `core/migrate.go` | GORM AutoMigrate for core and registered extension tables. |
| `core/seeder.go` | Declarative TOML demo-data import. |
| `core/themes.go` | Theme registry and factory lookup. |
| `core/plugins.go` | Plugin registry and activation lifecycle. |
| `core/table_registry.go` | Tracks core-, plugin-, and theme-owned tables. |

## Content System

- **Unified model** — `Content`, `ContentMeta`, and the `ContentType` registry
  drive every editorial type.
- **Core types** — `post`, `page`, `contact_message`, `category`, and `tag` are
  registered by the engine and survive theme switches.
- **Theme types** — themes declare custom types in `theme.toml` under
  `[[content_types]]`; core registers them when the theme is activated.
- **Registry-driven behavior** — one `ContentTypeDef` controls admin navigation,
  CRUD forms, REST exposure, rewrites, sitemap entries, taxonomy archives, and
  BaseTheme archive/detail rendering. `rewrite_slug` defines public URLs, while
  `templates = { archive = "...", single = "..." }` can map a type to a
  different visual page bundle.
- **Fluent queries** — themes can query through the shared builder, for example
  `ContentQuery.Type("product").Published().Taxonomy("category", "hepa").Paginate(1, 20)`.
- **Taxonomies** — hierarchical categories and flat tags support many-to-many
  relationships and automatic counts. A theme attaches them to a content type
  with `taxonomies = ["category", "tag"]`.
- **Canonical term archives** — `/category/{slug}` and `/tag/{slug}` aggregate
  registered content types. Type badges use the active theme's
  `content_type.<name>` locale key and fall back to the registry label.
- **Safe filtering** — taxonomy archives ignore content types no longer
  registered by the active theme. Dynamic content archives accept taxonomy
  query parameters only for taxonomies declared on that type; indexable term
  links should use the canonical term archive URLs instead.

Names such as `product`, `service`, and `showcase` are conventions used by some
themes, not core requirements. A theme can declare `module`, `project`,
`case_study`, or any other type and receive the same framework behavior.

## Hook Event Bus

`AddAction`, `DoAction`, `AddFilter`, and `ApplyFilter` provide ordered extension
points throughout the engine lifecycle. Themes expose semantic frontend slots
with `{{renderHook "slot.name" .}}`. Every registration returns a handle so a
plugin can remove its actions and filters during runtime deactivation. See the
[Hook System](hooks.md).

## Multi-level Cache

- **L1 memory + optional L2 Redis** — cache keys include the language dimension,
  and missing Redis degrades safely to the in-process cache.
- **Tag invalidation** — related entries can be invalidated in batches.
- **Full-page cache** — middleware can return a complete cached HTML response
  before theme rendering.

See [Caching and i18n](caching-and-i18n.md).

## Asynchronous Work

The worker pool combines goroutine workers with cron-style scheduling for
background tasks that should not block page rendering.

## Users and Permissions

Core owns users, JWT and public sessions, roles, capabilities, and audit logs.
Protected handlers must check a concrete `resource.action` permission, not only
whether a session exists. Themes and identity plugins use core's provider-neutral
public-auth contracts instead of creating their own user or session stores.

## Media

The media service handles uploads, metadata, responsive variants, optional WebP
generation, and the admin media library. Frontend themes consume variants through
the shared responsive-image helpers rather than querying media tables directly.

## Menus

Themes declare named locations such as `header` and `footer`. Core stores menus,
builds nested item trees, resolves content-linked URLs through the rewrite
registry, and exposes location-resolution hooks for language-aware assignment.

## Global Options

The options repository stores site settings, theme settings, plugin settings,
and the active theme/plugin state. Components may register translatable option
keys; runtime code reads them through core option and i18n helpers.

## Internationalization

The core i18n manager loads core, theme, and plugin locale files and exposes the
`T` template helper. Optional database translations can override UI strings,
theme options, and site settings without introducing direct theme/plugin
dependencies.

## Demo Data

Themes can expose a TOML seed path through `DemoDataProvider`. The importer
creates content, metadata, taxonomy relationships, and referenced media while
tracking whether a theme's demo data has already been imported.

## Database Prefixes

All tables use the configured site prefix. Core, plugin, and theme table helpers
also encode ownership, allowing multiple GoPress instances to share one
PostgreSQL database safely. See [Database Prefixes](../reference/database-prefix.md).

## Runtime Boundaries

GoPress keeps these dependencies explicit:

- Core owns shared services, data models, authorization, and extension contracts.
- Themes depend on core contracts and own presentation; they do not import plugins.
- Plugins depend on core contracts and contribute behavior through hooks,
  providers, middleware, routes, and settings pages; they do not import themes.
- A theme may declare a module-level plugin requirement in `theme.toml`, but its
  runtime implementation still communicates only through generic core APIs.
