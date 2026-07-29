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

Activate `seo-extras` from **Admin > Plugins**. The four editor fields are
optional and can be mixed: a filled value overrides its corresponding field,
while an empty value continues to use core output.

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

BaseTheme-based themes receive the SEO patch automatically, and so do typed themes that embed `coreTheme.SEOPageService` — its `BuildContentSEO` already calls `coreTheme.ApplyContentMetaSEO`. Only themes that build `SEOMeta` entirely by hand (not via `SEOPageService`) must call `coreTheme.ApplyContentMetaSEO` themselves when building page SEO.

The complete rendering path is:

```text
SEOBuilder.ForContent
  -> request-aware site option overrides
  -> ApplyContentMetaSEO
     -> seo.content.meta filter chain
  -> PageData.SEO / data["SEO"]
  -> {{seoHeadFor .}}
```

## Activation Behavior

| State | Result |
|---|---|
| Inactive | No panel or filter; core title, excerpt, image, and robots values render. |
| Active, fields empty | Panel is visible, but SEO output remains unchanged. |
| Active, some fields set | Only supplied fields override core output. |
| Deactivated after use | Hook output disappears; stored `_seo_*` values remain but are not read. |

The plugin owns no custom table or route. It reuses `gp_content_meta`, and
deactivation removes all three hook handles.

## Custom SEO Plugins

Additional SEO plugins can subscribe to the same `seo.content.meta` filter. Priority order controls how multiple plugins compose their changes.

Keep custom plugins additive: use the shared metadata filter for new schema or
Open Graph behavior rather than importing a theme or replacing its complete
HTML response.
