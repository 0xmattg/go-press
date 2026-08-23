# GoPress

GoPress is a content management framework and CMS engine written in Go. It is designed for self-hosted websites and content applications that need theme rendering, plugin extension, media handling, SEO, multilingual support, and a practical admin interface without giving up Go's runtime and deployment advantages.

It is not a line-by-line rewrite of WordPress, and it is not a claim that PHP-based CMS platforms are obsolete. GoPress takes familiar CMS concepts—content types, themes, hooks, plugins, menus, options, media, taxonomies, and REST APIs—and reorganizes them around a compiled Go service, PostgreSQL, a clear extension contract, and a maintainable engineering model.

## What GoPress Provides

- Unified content and metadata models for posts, pages, theme-defined content types, and plugin-owned data.
- A built-in admin CMS with data-driven CRUD pages, media library, menu management, theme settings, plugin settings, users, permissions, audit logs, cache controls, and system settings.
- A theme runtime with config-driven content routing, dynamic template resolution, WordPress-like fallback hierarchy, SEO injection, responsive image helpers, menu locations, language-aware URLs, and frontend hook slots.
- A plugin system based on Go interfaces, actions, filters, request-local
  content/taxonomy scopes, and optional settings providers.
- Core services for caching, workers, URL rewriting, sitemap generation, redirects, REST APIs, i18n, and table-prefix isolation.
- Provider-neutral public accounts, external identity bindings, revocable sessions, registration policy, Google OIDC login, and MetaMask EIP-4361 wallet login.
- Policy-driven public content submission with active-account checks,
  owner-scoped RBAC, review states, validation, and rate limits.
- Core comments with authenticated posting, one-level replies, moderation, admin
  pagination, cache invalidation, and theme-owned account/profile presentation.
- A protocol-neutral Core Agent Registry and Executor with principal refresh,
  scope plus RBAC authorization, ownership checks, idempotency, and mandatory
  audit, exposed through the optional read-only-by-default `gopress-mcp` plugin.

## Project Status

GoPress is currently **beta** software. The content model, admin, theme runtime,
plugin contracts, SEO, cache, media pipeline, public accounts, and bundled
examples are usable, but broader production validation, benchmarks, migration
guidance, and continuing security review remain active work.

For production adoption, start with an internal site, company site,
documentation site, or content application, and establish load testing,
database backup, media backup, and rollback procedures for the deployment.

## Why GoPress

The project retains the proven CMS shape of content models, themes, plugins,
menus, and editorial administration while using a compiled Go service,
goroutines, static typing, and a standard Go toolchain. The goal is not to rank
technology ecosystems; it is to offer a maintainable self-hosted option for
teams that want CMS workflows inside a Go architecture.

| Dimension | Traditional WordPress deployment | GoPress |
|---|---|---|
| Runtime | PHP-FPM/web-server request lifecycle | Long-running compiled Go service |
| Extension model | Mature runtime-loaded theme/plugin ecosystem | Compiled Go interfaces and removable hooks |
| Cache | Commonly assembled from plugins, object cache, and proxy layers | Core L1, optional Redis, and page-cache paths |
| Scheduled work | WP-Cron or system cron | In-process worker pool and scheduler |
| Delivery | Web server, PHP runtime, database, and related services | One server binary, PostgreSQL, and optional Redis |

## Core Design Principles

1. **Content as a first-class model** — content and metadata are modeled as stable engine concepts instead of being scattered across theme code.
2. **Themes render, plugins extend** — themes own presentation; plugins attach behavior through core extension points.
3. **Typed extension contracts** — themes and plugins communicate with the engine, not directly with each other.
4. **Cache by default** — the engine provides L1 memory cache, optional Redis, and page-level cache paths.
5. **SEO and URLs belong to the framework** — rewrite rules, canonical URLs, sitemap output, metadata, template mapping, and SEO overrides are coordinated in core.
6. **Admin first** — content teams should manage most site behavior from the CMS instead of editing code.
7. **API first** — public content types can expose REST APIs documented through Swagger/OpenAPI.
8. **Multi-instance isolation** — site configuration and table prefixes let instances share infrastructure without sharing data boundaries.
9. **Open-source ready architecture** — public APIs, docs, and extension boundaries are designed to survive third-party themes and plugins.

## Start Here

- [Installation](getting-started/installation.md)
- [Configuration](getting-started/configuration.md)
- [Architecture Overview](architecture/overview.md)
- [Content and Taxonomy Scope APIs](architecture/content-scope.md)
- [Public Authentication](architecture/public-authentication.md)
- [Public Content Submission](architecture/public-content-submission.md)
- [Comments and Replies](architecture/comments.md)
- [Theme Development](themes/overview.md)
- [Plugin Development](plugins/overview.md)
- [Multilingual Plugin](plugins/multilang.md)
- [Agent and MCP](agent/overview.md)
- [GoPress MCP Plugin Setup](plugins/gopress-mcp.md)
- [Roadmap and Contributions](reference/roadmap.md)
