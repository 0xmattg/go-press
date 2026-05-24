# Admin Overview

The GoPress admin is a built-in CMS interface for managing content, media, menus, themes, plugins, cache, redirects, users, and system settings.

## Main Areas

- **Dashboard** — overview metrics and recent operation logs.
- **Content** — core posts and theme-defined content types.
- **Taxonomy** — categories and tags.
- **System** — menus, themes, plugins, cache, redirects, media library, system settings, and users.

The sidebar is generated from core content types, active theme metadata, plugin capabilities, and registered admin routes.

## Content Management

The admin CRUD surface is data-driven. A content type declared in `theme.toml` can automatically receive list, create, edit, delete, media, taxonomy, sorting, REST, and rewrite behavior depending on its `supports` and `taxonomies` settings.

## Settings

System settings are split into website settings and admin settings. Website settings affect the public site, SEO metadata, sitemap, favicon, publish-time timezone, and branding options. The `site_icon` value is the shared favicon source for all themes. `site_timezone` is the shared timezone used to parse admin publish-time inputs and format content dates in admin lists and themes; timestamps are stored in UTC. Admin settings control the CMS interface, including the admin language.

## Extension Points

Plugins can add settings pages, content form fields, save handlers, content-list tabs, frontend hook output, SEO overrides, and sitemap transformers without modifying admin core templates directly.
