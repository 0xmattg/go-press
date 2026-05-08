# Caching and i18n

GoPress includes cache and internationalization as core services because both affect routes, templates, admin labels, plugin settings, and frontend rendering.

## Cache Layers

| Layer | Role |
|---|---|
| L1 memory cache | Fast in-process cache for hot data and page output. |
| L2 Redis cache | Optional shared cache for multi-process or multi-node deployments. |
| Database | Source of truth for content, options, menus, media, and plugin data. |

When Redis is unavailable or not configured, the system continues with memory cache and database access.

## Cache Invalidation

Admin writes, theme switching, plugin activation, menu changes, and selected settings updates clear relevant cache paths. The cache admin page also provides manual operations for clearing all cache, page cache, or fragment cache.

## Core i18n

The core i18n manager loads locale files and exposes translation helpers to admin templates, installer templates, theme templates, and plugins. It supports fallback behavior so missing keys do not break the page.

## Language Scope

The multilingual plugin adds content-language scoping through the Content Scope API. Core remains language-aware but does not directly depend on the plugin. This allows a site to run as a single-language CMS or as a multilingual CMS with the same theme and admin surface.

