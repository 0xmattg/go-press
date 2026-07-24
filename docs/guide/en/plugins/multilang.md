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

## Architecture

The plugin relies on core extension points:

- Content Scope API for language-aware queries.
- Menu location resolution hooks for language-specific menus.
- Admin content list tabs for language filters.
- Template helpers such as `currentLang`, `langPrefixURL`, `archiveURL`, and `contentURL`.
- Option translation helpers for theme setting translations.
- Core site option translation helpers for SEO titles, meta descriptions, OpenGraph descriptions, and `.Settings` values.

Core remains usable without the plugin; multilingual behavior is additive.

## Theme Integration for Site Setting Translations

BaseTheme themes support site setting translations automatically. Layouts should continue using `{{pageTitleFor . $fallbackTitle}}` and `{{seoHeadFor .}}`.

Themes that embed `coreTheme.SEOPageService` need no extra work: `BuildArchiveSEO` / `BuildContentSEO` already call `ApplySiteOptionOverridesFromOptionsForRequest` in the current request language. Only themes that build `SEOMeta` entirely by hand (not via `SEOPageService`) must keep the current request context and call `coreTheme.ApplySiteOptionOverridesFromOptionsForRequest(c, options, i18nMgr, seoBuilder, &seo)` after building SEO metadata. See [Theme SEO Integration](../themes/seo-integration.md) for the full contract.
