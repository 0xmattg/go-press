# Multilingual Plugin

The `multilang` plugin provides WPML-like multilingual behavior for GoPress. It supports enabled languages, default language, language-prefixed URLs, content translation links, menu assignment per language, UI string translations, theme setting translations, and core site setting translations.

## Features

- Manage enabled languages from the plugin settings page.
- Clone default-language content into target languages.
- Keep translation groups across related content records.
- Resolve language from URL prefix and request context.
- Scope content queries through the Content Scope API.
- Assign menus per location and language.
- Translate menu labels, UI strings, and theme settings.
- Translate core `site_name` and `site_description` so document titles,
  descriptions, Open Graph output, and template settings use the request
  language.
- Isolate page-cache entries by language.

## Translation Groups and Slug Semantics

Each language version is an independent `Content` row connected by a
translation-group ID (`trid`). Slug uniqueness is scoped per language, so two
translations can share `hepa-filter` and remain distinct through their URL
prefixes.

```text
Content #1 (en) /products/hepa-filter
Content #24 (zh) /zh/products/hepa-filter
         \             /
          translation group: trid 5
```

The implementation composes generic core APIs:

| Core contract | Role |
|---|---|
| `content.AddContentScope` | Restrict the request to translated content IDs for the active language. |
| `FindBySlugScoped` | Resolve duplicate slugs inside the active request scope. |
| `EnsureUniqueSlugScoped` | Check uniqueness in the target language while cloning a translation. |
| `BasePageService.ForRequest` | Carry the current scope into custom theme services. |
| `admin.HookContentPermalinkPrefix` | Show `/zh` or another prefix in the editor permalink. |

Without the plugin, scoped methods preserve ordinary single-language behavior.

## URL Behavior

The examples below use a theme-declared `product` content type whose `rewrite_slug` is `products`. The same behavior applies to any registered content type; the plugin reads core rewrite configuration instead of assuming product/service/showcase routes.

The default language uses normal URLs:

```text
/products/example
```

Other languages receive a prefix:

```text
/zh/products/example
```

The plugin preserves same-slug semantics where possible, so translated content can share the same slug under different language prefixes.

Language detection follows URL prefix, then the language cookie, then
`Accept-Language`, and finally the configured default language.

When the current detail page has no translation in the target language, the language switcher does not invent a target URL that would 404. It leaves the user on the current page and does not persist the target language cookie for that failed detail-page switch. Archive pages and static pages can still be prefixed normally.

## Admin Translation Management

The plugin settings page contains tabs for:

- Languages.
- Content translations.
- Menu translations.
- String translations.
- Theme setting translations.
- Site setting translations for `site_name` and `site_description`.
- Basic settings and help.

If a theme or plugin provides only one locale, the admin falls back to the available language instead of hiding the settings UI.

## Plugin Tables

| Table | Purpose |
|---|---|
| `gp_plgn_multilang_translations` | Content translation groups and language codes. |
| `gp_plgn_multilang_languages` | Enabled languages, default flag, and display order. |
| `gp_plgn_multilang_string_translations` | Theme UI and option/site-setting overrides. |
| `gp_plgn_multilang_menu_translations` | Per-language menu translation groups. |

## Architecture

The plugin relies on core extension points:

- Content Scope API for language-aware queries.
- Menu location resolution hooks for language-specific menus.
- Admin content list tabs for language filters.
- Template helpers such as `currentLang`, `langPrefixURL`, `archiveURL`, and `contentURL`.
- Option translation helpers for theme setting translations.
- Core site option translation helpers for SEO titles, meta descriptions, OpenGraph descriptions, and `.Settings` values.

Core remains usable without the plugin; multilingual behavior is additive.

## Template Helpers and Navigation Slot

```gotemplate
{{T .Ctx "welcome"}}
{{currentLang .Ctx}}
{{langPrefixURL .Ctx "/about"}}
{{archiveURL "product"}}
{{contentURL . "product"}}
{{renderHook "header.nav.after" .}}
```

The language switcher is contributed through `header.nav.after`; themes must
place that generic slot inside the primary navigation and support nested
extension menus on mobile. The theme must not inspect plugin classes or options.

## Menu Resolution

The plugin filters `menu.location.resolve` after core loads the menu assigned to
a location. It finds the matching menu in the current translation group, clones
it, prefixes local URLs, and resolves translated content links. `menu.deleted`
removes associated translation records. The theme continues to call only
`menuByLocation "header"`.

## i18n Resolution

UI strings resolve from database overrides (`domain="theme"`), then component
locale files, then the original message ID. Translatable theme and site options
use `domain="option"` and the `_opt.` namespace. See
[Caching and i18n](../architecture/caching-and-i18n.md).

## Theme Integration for Site Setting Translations

BaseTheme themes support site setting translations automatically. Layouts should continue using `{{pageTitleFor . $fallbackTitle}}` and `{{seoHeadFor .}}`.

Themes that embed `coreTheme.SEOPageService` need no extra work: `BuildArchiveSEO` / `BuildContentSEO` already call `ApplySiteOptionOverridesFromOptionsForRequest` in the current request language. Only themes that build `SEOMeta` entirely by hand (not via `SEOPageService`) must keep the current request context and call `coreTheme.ApplySiteOptionOverridesFromOptionsForRequest(c, options, i18nMgr, seoBuilder, &seo)` after building SEO metadata. See [Theme SEO Integration](../themes/seo-integration.md) for the full contract.
