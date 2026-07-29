# Plugin System Overview

Plugins extend GoPress through public core contracts. They can register hooks, add admin settings pages, store plugin-owned data, inject frontend HTML into standard template slots, transform SEO metadata, and participate in multilingual or sitemap behavior.

## Core Ideas

- Plugins register themselves with `core.RegisterPlugin`.
- Activation state is stored in options and can be changed from the admin.
- Hooks returned during activation must be removed during deactivation.
- Plugin database tables should use `dbprefix.PluginTable`.
- Plugin admin UI should use core settings-provider interfaces and locale files.
- Activation and deactivation rebuild the Gin router, so currently active
  `middleware.early` and `routes.register` hooks take effect immediately.
- Core clears frontend cache after activation changes, preventing cached pages
  from retaining removed navigation or script output.
- Sitemap transformers and other registration APIs return symmetric remove
  handles and follow the same lifecycle contract.

## Plugin Lifecycle

```text
register -> activate -> setup hooks/settings/routes -> run -> deactivate -> remove hooks
```

Hot disable is an important contract. A disabled plugin should stop affecting admin forms, frontend HTML, SEO metadata, sitemap output, menus, and middleware behavior without requiring a process restart.

Long-lived workers and requests already executing on the previous router still
need a lightweight runtime-active guard. Deactivation preserves plugin-owned
data unless an explicit uninstall workflow says otherwise.

## Common Extension Points

| Extension point | Purpose |
|---|---|
| `admin.content_list.tabs` | Add filtered content-list tabs and counts. |
| `admin.content.permalink_prefix` | Add contextual editor URL prefixes. |
| `admin.content_form.fields` | Add editor meta boxes. |
| `admin.content.saved` | Persist extension-owned form values. |
| `admin.dashboard.widgets` | Add permission-aware dashboard summaries. |
| `seo.content.meta` | Transform single-page SEO metadata. |
| `theme.head.end`, `theme.body.open`, `theme.footer.end` | Inject site-level markup at semantic frontend slots. |
| `header.nav.after` | Add a primary-navigation extension item. |

The [Hook System](../architecture/hooks.md) and
[Admin Extension Points](../admin/extension-points.md) document the payloads.

## Built-in Plugins

| Plugin | Purpose |
|---|---|
| `multilang` | WPML-like content translation, menu assignment, language-prefixed URLs, and theme setting translations. |
| `seo-extras` | Yoast-like per-content SEO title, description, Open Graph image, and robots overrides. |
| `code-snippets` | WPCode-like site-level HTML/JS injection into head, body, and footer slots. |
| `gopress-analytics` | First-party self-hosted page-view, visitor, trend, and top-page analytics. |
| `google-identity` | Google OpenID Connect login and registration through the core public-auth contract. |
| `metamask-identity` | MetaMask and EIP-4361 Sign-In with Ethereum login and registration through one-time server challenges. |

## Boundary Rule

Plugins should only depend on core packages and public interfaces. They should not import a theme, assume a theme's HTML structure, or scan final HTML responses to patch output.

See [Public Authentication](../architecture/public-authentication.md) for identity-provider and theme integration rules.

## Next Steps

- [Creating Plugins](creating-plugins.md)
- [Multilingual Plugin](multilang.md)
- [SEO Extras](seo-extras.md)
- [Code Snippets](code-snippets.md)
- [GoPress Analytics](gopress-analytics.md)
