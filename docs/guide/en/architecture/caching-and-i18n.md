# Caching and i18n

GoPress treats cache and internationalization as core services because both
affect routes, templates, admin labels, settings, SEO, and frontend rendering.

## Multi-level Cache

| Layer | Medium | Role |
|---|---|---|
| L1 | In-process LRU | Hot data with no network round trip. |
| L2 | Optional Redis | Shared cache for multi-process or multi-node deployments. |
| Full page | Reuses L1 and L2 | Returns complete HTML before theme rendering on a hit. |
| Database | PostgreSQL | Source of truth for content, options, menus, media, and extension data. |

Cache keys include the language dimension so translated pages never share HTML
entries. Content writes, menu updates, settings changes, theme switches, and
plugin activation changes invalidate the corresponding tags or cache paths. The
admin cache page also provides manual all-cache, page-cache, and fragment-cache
operations.

Redis is optional. When it is missing or unavailable, GoPress continues with the
in-process cache and database rather than making the site unavailable.

## Core i18n Architecture

Core owns localization primitives; an optional multilingual plugin contributes
database overrides and management UI through public contracts:

```text
core/i18n.Manager
  -> load core, plugin, and theme locale files
  -> T(ctx, key)
  -> TranslateOption / TranslateSettings

core/option registry
  -> RegisterTranslatable(key, section, label)
  -> IsTranslatable / AllTranslatableKeys

resolution priority
  1. database StringTranslation override
     domain="theme" for UI; domain="option" for settings
  2. component locale file
  3. original message ID
```

This fallback chain keeps a page usable when a translation is incomplete. Core
remains a complete single-language CMS when no plugin supplies database
translations.

### UI Strings

Themes place JSON messages under `locales/` and use `{{T .Ctx "welcome"}}` in
templates. A multilingual extension may add or override messages in the
database without editing theme files.

### Translatable Settings

Themes register copy-oriented option keys such as hero headings or About text.
Core translates only registered keys when rendering the settings map:

```go
func registerTranslatableOptions() {
    option.RegisterTranslatable("home_hero_title", "hero", "Hero title")
    option.RegisterTranslatable("home_about_title", "about", "About title")
}

func (p *PageData) TranslateSettings(c *gin.Context, manager *i18n.Manager) {
    p.Settings = manager.TranslateSettings(
        c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
}
```

### Template Helpers

```gotemplate
{{T .Ctx "welcome"}}
{{currentLang .Ctx}}
{{langPrefixURL .Ctx "/about"}}
{{archiveURL "product"}}
{{contentURL . "product"}}
{{taxonomyURL "category" .Slug}}
```

`product` is only an example theme-defined type. The URL helpers consult the
rewrite registry, so custom type names and changed `rewrite_slug` values do not
require hard-coded link changes.

## Language Scope

A multilingual plugin can add language-aware content and term queries through
the generic [Content and Taxonomy Scope APIs](content-scope.md), resolve
per-language menus through menu hooks, and add URL prefixes through core
helpers. Core and themes do not import or inspect that plugin; without it, the
same APIs retain single-language behavior.

See the [Multilingual Plugin](../plugins/multilang.md) for content and
Category/Tag translation, canonical URL detection, menu assignment, SEO
alternates, and language-switching behavior.
