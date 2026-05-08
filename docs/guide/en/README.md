# GoPress

GoPress is a content management framework and CMS engine written in Go. It is designed for self-hosted websites and content applications that need theme rendering, plugin extension, media handling, SEO, multilingual support, and a practical admin interface without giving up Go's runtime and deployment advantages.

It is not a line-by-line rewrite of WordPress, and it is not a claim that PHP-based CMS platforms are obsolete. GoPress takes familiar CMS concepts—content types, themes, hooks, plugins, menus, options, media, taxonomies, and REST APIs—and reorganizes them around a compiled Go service, PostgreSQL, a clear extension contract, and a maintainable engineering model.

## What GoPress Provides

- Unified content and metadata models for posts, pages, theme-defined content types, and plugin-owned data.
- A built-in admin CMS with data-driven CRUD pages, media library, menu management, theme settings, plugin settings, users, permissions, audit logs, cache controls, and system settings.
- A theme runtime with WordPress-like template fallback, SEO injection, responsive image helpers, menu locations, language-aware URLs, and frontend hook slots.
- A plugin system based on Go interfaces, actions, filters, and optional settings providers.
- Core services for caching, workers, URL rewriting, sitemap generation, redirects, REST APIs, i18n, and table-prefix isolation.

## Core Design Principles

1. **Content as a first-class model** — content and metadata are modeled as stable engine concepts instead of being scattered across theme code.
2. **Themes render, plugins extend** — themes own presentation; plugins attach behavior through core extension points.
3. **Typed extension contracts** — themes and plugins communicate with the engine, not directly with each other.
4. **Cache by default** — the engine provides L1 memory cache, optional Redis, and page-level cache paths.
5. **SEO and URLs belong to the framework** — rewrite rules, canonical URLs, sitemap output, metadata, and SEO overrides are coordinated in core.
6. **Admin first** — content teams should manage most site behavior from the CMS instead of editing code.
7. **Open-source ready architecture** — public APIs, docs, and extension boundaries are designed to survive third-party themes and plugins.

## Start Here

- [Installation](getting-started/installation.md)
- [Configuration](getting-started/configuration.md)
- [Architecture Overview](architecture/overview.md)
- [Theme Development](themes/overview.md)
- [Plugin Development](plugins/overview.md)

