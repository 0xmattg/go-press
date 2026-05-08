# SEO Extras Plugin

The `seo-extras` plugin provides Yoast-like per-content SEO overrides. After activation, content edit pages receive an optional SEO panel with fields for SEO title, SEO description, Open Graph image, and robots directives.

## Why It Exists

Core SEO metadata is inferred from content fields:

| SEO field | Default source |
|---|---|
| Description and Open Graph description | `Content.Excerpt`, truncated when needed. |
| Open Graph image | `Content.ImageURL`. |
| Title and Open Graph title | `Content.Title`. |
| Robots | `index, follow`. |

Some editorial workflows need a separate SEO title, custom description, special share image, or `noindex` directive without changing the visible page content.

## Storage

The plugin stores values in `gp_content_meta` with `_seo_` keys:

| Field | Meta key |
|---|---|
| SEO Title | `_seo_title` |
| SEO Description | `_seo_description` |
| Open Graph Image | `_seo_image` |
| Robots | `_seo_robots` |

Empty fields are deleted rather than stored as empty strings. This keeps the meaning clear: missing metadata means use the default SEO output.

## Hooks

The plugin is implemented without core schema changes. It subscribes to:

```text
admin.content_form.fields  -> render SEO panel
admin.content.saved        -> persist submitted values
seo.content.meta           -> patch SEOMeta
```

BaseTheme-based themes receive the SEO patch automatically. Custom `PageData` themes must call `coreTheme.ApplyContentMetaSEO` when building page SEO.

## Custom SEO Plugins

Additional SEO plugins can subscribe to the same `seo.content.meta` filter. Priority order controls how multiple plugins compose their changes.

