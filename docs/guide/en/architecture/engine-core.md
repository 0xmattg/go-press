# Engine Core

The engine is the runtime container of GoPress. It wires together storage, content repositories, rewrite rules, SEO rendering, hooks, cache, workers, admin routes, API routes, installer routes, and the active frontend theme.

## Main Modules

| Module | Responsibility |
|---|---|
| `core/engine.go` | Engine lifecycle, route setup, shutdown, and the `App` surface. |
| `core/bootstrap.go` | One-call bootstrap orchestration. |
| `core/migrate.go` | GORM AutoMigrate for core tables. |
| `core/seeder.go` | Declarative demo data import from TOML. |
| `core/themes.go` | Theme registry and factory lookup. |
| `core/plugins.go` | Plugin registry and activation lifecycle. |
| `core/table_registry.go` | Tracks core, plugin, and theme-owned tables. |

## Core Types

Core content types such as `post`, `contact_message`, `category`, and `tag` are registered by the engine and are not owned by any theme. They remain available across theme switches and are used by admin, REST APIs, sitemap generation, and fallback templates.

Theme-specific content types are declared in `theme.toml`. During activation, the engine reads the active theme metadata and registers those types in the content registry.

## Runtime Boundaries

GoPress keeps a strict boundary:

- Core owns shared services and contracts.
- Themes own rendering and theme-specific data presentation.
- Plugins own additive behavior and optional admin settings.

When new features cross this boundary, they should usually become a core hook, a core helper, or a small interface instead of a direct dependency.

