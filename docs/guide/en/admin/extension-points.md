# Admin Extension Points

Admin extension points let plugins and themes add behavior to the CMS while keeping the core admin stable.

## Plugin Settings Pages

A plugin can provide an admin settings page by implementing settings provider interfaces. The admin will show a **Plugin Settings** button on the plugin card and route requests to the plugin-owned template or renderer.

Typical responsibilities:

- Render settings UI.
- Load current settings data.
- Save submitted settings.
- Return translated labels through the admin locale system.

## Content Form Fields

Plugins can add extra fields to content editing pages through `admin.content_form.fields`.

The `seo-extras` plugin uses this hook to append a collapsible SEO panel with fields for SEO title, description, Open Graph image, and robots.

## Content Save Actions

Plugins can listen to `admin.content.saved` and persist additional form values. This keeps plugin data separate from the core content model while still making it part of the editorial workflow.

## Content List Tabs

The admin exposes `admin.HookContentListTabs` for plugins that need additional list filters. The multilingual plugin uses it to add language tabs and counts to content list pages.

## Translation Requirements

Admin-facing theme and plugin settings should not hard-code Chinese or English in templates. They should use the admin translation helper or component-owned locale files. If a component ships only one language, the admin should fall back to that available language instead of hiding the UI.

## Design Rule

Extensions should communicate with the admin through core hooks, providers, and template functions. Avoid direct imports between themes and plugins, and avoid post-processing full HTML responses to inject admin UI.

