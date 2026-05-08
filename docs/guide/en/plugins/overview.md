# Plugin System Overview

Plugins extend GoPress through public core contracts. They can register hooks, add admin settings pages, store plugin-owned data, inject frontend HTML into standard template slots, transform SEO metadata, and participate in multilingual or sitemap behavior.

## Core Ideas

- Plugins register themselves with `core.RegisterPlugin`.
- Activation state is stored in options and can be changed from the admin.
- Hooks returned during activation must be removed during deactivation.
- Plugin database tables should use `dbprefix.PluginTable`.
- Plugin admin UI should use core settings-provider interfaces and locale files.

## Plugin Lifecycle

```text
register -> activate -> setup hooks/settings/routes -> run -> deactivate -> remove hooks
```

Hot disable is an important contract. A disabled plugin should stop affecting admin forms, frontend HTML, SEO metadata, sitemap output, menus, and middleware behavior without requiring a process restart.

## Built-in Plugins

| Plugin | Purpose |
|---|---|
| `multilang` | WPML-like content translation, menu assignment, language-prefixed URLs, and theme setting translations. |
| `seo-extras` | Yoast-like per-content SEO title, description, Open Graph image, and robots overrides. |
| `code-snippets` | WPCode-like site-level HTML/JS injection into head, body, and footer slots. |

## Boundary Rule

Plugins should only depend on core packages and public interfaces. They should not import a theme, assume a theme's HTML structure, or scan final HTML responses to patch output.

