# Multilingual Plugin

The `multilang` plugin adds WPML-like multilingual behavior to GoPress while
keeping language policy outside core. It supports independent content records,
optional Category/Tag translations, language-specific menus, UI strings, theme
settings, and core site settings.

Core remains fully usable without the plugin. The plugin composes generic
content scopes, taxonomy scopes and commands, hooks, rewrite metadata, SEO
filters, and sitemap transformers instead of importing a theme or adding
language branches to core.

## Feature Summary

- Manage enabled languages, the default language, display names, flags, and
  ordering.
- Clone content into a target language and connect variants in translation
  groups.
- Keep Category and Tag terms shared by default, or opt either taxonomy into
  independent per-language translation.
- Assign translated menus by theme location and rewrite local links to the
  current language.
- Translate theme UI strings, registered theme settings, `site_name`, and
  `site_description`.
- Generate canonical language-prefixed URLs, translation-aware language
  switches, SEO alternates, and sitemap `hreflang` entries.
- Isolate page-cache entries by language.

## Content Translation and Slugs

Each content translation is an independent `Content` row connected by a
translation-group ID (`trid`). Slug uniqueness is scoped per language, so two
translations can intentionally share a clean slug:

```text
Content #1 (en) /products/hepa-filter
Content #24 (zh) /zh/products/hepa-filter
         \             /
          translation group: trid 5
```

The example uses a theme-declared `product` type with
`rewrite_slug = "products"`. The same behavior applies to every registered
content type; the plugin reads the core content and rewrite registries rather
than assuming product, service, or showcase routes.

## Category and Tag Translation Policy

The first taxonomy-translation version supports the core `category` and
`tag` taxonomies. Each type has an independent policy:

| Mode | Behavior | Compatibility |
|---|---|---|
| **Shared** | Every language uses the existing term and taxonomy rows. | Default. Existing sites retain their historical IDs, relationships, slugs, and URLs. |
| **Translate independently** (`translated_only`) | Each language owns separate term and taxonomy rows connected by plugin-owned translation groups. | Names, slugs, descriptions, and hierarchy can differ by language. |

Changing a policy does not rewrite or delete existing taxonomy data. Legacy
rows that have no language association remain visible in the default language,
which makes the default shared mode and first activation backward compatible.
Non-default languages in independent mode expose only explicitly associated
rows.

Independent translations preserve a semantic relationship while retaining
separate core identities:

```text
Taxonomy #10 (en)  category/news
Taxonomy #31 (zh)  category/xinwen
          \                 /
           taxonomy translation group
```

This means the translated term may use a different name, slug, description, and
parent. For hierarchical Categories, translate the parent in the target
language before translating its child.

## Taxonomy URLs

The core taxonomy bases remain stable and are never translated. Only the
language prefix and term slug vary:

| Language | Category | Tag |
|---|---|---|
| Default English | `/category/news` | `/tag/security` |
| Chinese | `/zh/category/xinwen` | `/zh/tag/anquan` |

A localized Unicode slug such as `/zh/tag/洁净室` is also valid. ASCII slugs
are often easier to type and share internationally; localized slugs can be more
natural for readers. Choose one convention per site, keep it stable, and use a
redirect if a published slug changes.

Slug uniqueness is enforced inside the active taxonomy scope. Two language
variants may therefore use the same slug, while duplicates within one language
are rejected.

## Admin Workflow for Taxonomies

1. Enable at least two languages in **Plugin Settings → Languages**.
2. Open **Translation Management → Taxonomy translations**.
3. Set Category and/or Tag to **Translate independently**, then save.
4. In the taxonomy translation table, create a target-language translation
   from an existing term. Translate a hierarchical parent first.
5. Follow the edit link to the normal **Categories** or **Tags** screen, select
   the target language tab, and refine its localized name, slug, description,
   and parent.
6. Use the same language tab in content editors; the Category/Tag selectors
   only show terms valid for that language.

When content is cloned, shared taxonomies keep the same relationship IDs.
Independently translated taxonomies are mapped only when a target-language
translation exists; otherwise that relationship is omitted rather than linked
to the wrong language. Creating the missing term translation later reconciles
matching translated-content relationships.

## Canonical URL and Language Resolution

The default language uses unprefixed canonical URLs:

```text
/products/example
/category/news
```

Non-default languages use an explicit prefix:

```text
/zh/products/example
/zh/category/xinwen
```

Resolution deliberately separates canonical content URLs from language
preference:

- An explicit non-default prefix, such as `/zh/...`, is authoritative.
- An ordinary unprefixed public URL always resolves in the default language,
  even if the browser previously selected another language.
- Only the public root `/` consults `?lang`, the language cookie, then
  `Accept-Language`; a non-default choice is redirected to its canonical
  prefixed root such as `/zh/`.
- Admin lists and REST requests can use `?lang=zh` to select their request
  scope without changing public canonical URLs.

These rules prevent a valid unprefixed default-language link from returning 404
because of a stale language cookie.

On a translated content or taxonomy detail page, the switcher resolves the
target record and its real slug. If no target translation exists, it stays on
the current detail page and does not persist the unavailable target language.
Archive and static routes can still switch to their canonical prefixed form.

## Core Contracts Used

| Core contract | Role |
|---|---|
| `content.AddContentScope` | Restrict content reads to the current language. |
| Scoped content repository methods | Resolve same-slug content and validate writes inside one language. |
| `taxonomy.AddScope` / `taxonomy.WithScope` | Restrict term identities, trees, counts, relationships, and selectors. |
| Scoped taxonomy repository methods | Resolve translated term slugs and render language-correct archives. |
| `taxonomy.CommandService` | Validate scoped slug uniqueness, parents, relationships, and transactional writes. |
| `BasePageService.ForRequest` | Carry both content and taxonomy request contexts into theme services. |
| `admin.HookContentListTabs` / `admin.HookTaxonomyListTabs` | Add language tabs without core knowing what a language means. |
| `admin.HookContentPermalinkPrefix` | Display the canonical language prefix in the editor permalink. |
| Taxonomy SEO filters and sitemap transformers | Emit real translated URLs and `hreflang` alternates. |

See [Content and Taxonomy Scope APIs](../architecture/content-scope.md) for the
extension contract and mutation safety rules.

## SEO and Sitemap Behavior

Translation groups drive alternates rather than guessed paths:

- Content and independently translated Category/Tag pages use the actual target
  slug in canonical and `hreflang` links.
- `x-default` points to the default-language canonical URL.
- Shared-taxonomy sitemap entries reuse the same term slug with language
  prefixes.
- A missing translation is not emitted as an alternate.
- The sitemap transformer applies the same rules to generated entries.

## Translation Management

The plugin settings page contains:

- Languages.
- Content translations.
- Taxonomy translations.
- Menu translations.
- String translations.
- Theme setting translations.
- Site setting translations for `site_name` and `site_description`.
- Basic settings and help.

If a theme or plugin provides only one admin locale, the UI falls back to that
available locale instead of hiding its settings.

## Plugin Tables

| Table | Purpose |
|---|---|
| `gp_plgn_multilang_translations` | Content translation groups and language codes. |
| `gp_plgn_multilang_languages` | Enabled languages, default flag, and display order. |
| `gp_plgn_multilang_string_translations` | UI string and option/site-setting overrides. |
| `gp_plgn_multilang_menu_translations` | Per-language menu translation groups. |
| `gp_plgn_multilang_taxonomy_translation_groups` | Category/Tag translation group identities. |
| `gp_plgn_multilang_taxonomy_translations` | Taxonomy row, language, and source-language associations. |

## Template Helpers and Navigation

```gotemplate
{{T .Ctx "welcome"}}
{{currentLang .Ctx}}
{{langPrefixURL .Ctx "/about"}}
{{archiveURL "product"}}
{{contentURL . "product"}}
{{taxonomyURL "category" .Slug}}
{{renderHook "header.nav.after" .}}
```

The language switcher is contributed through `header.nav.after`. Themes place
that generic slot inside the primary navigation and support nested extension
menus on mobile; they must not inspect plugin classes, tables, or option keys.

BaseTheme themes automatically receive translated site settings and taxonomy
URLs. Custom services should clone `BasePageService.ForRequest(c)` and use
scoped repositories. See [Theme SEO Integration](../themes/seo-integration.md)
for themes that build `SEOMeta` manually.

## Menu and i18n Resolution

The plugin filters `menu.location.resolve` after core resolves a menu location.
It finds the current language's menu in the translation group, clones it,
prefixes local URLs, and resolves translated content links. The theme continues
to call only `menuByLocation "header"`.

UI strings resolve from database overrides (`domain="theme"`), then component
locale files, then the original message ID. Translatable theme and site options
use `domain="option"` and the `_opt.` namespace. See
[Caching and i18n](../architecture/caching-and-i18n.md).

## Compatibility and Operational Safety

- Shared mode is the default and preserves existing taxonomy behavior.
- Enabling the plugin does not automatically duplicate, migrate, rename, or
  delete terms.
- Switching an independently translated taxonomy back to shared mode is blocked
  while translation records for that type exist.
- Plugin deactivation is blocked while taxonomy translation records exist, so
  translated identities cannot silently collapse into an ambiguous shared
  namespace.
- Back up the database before restructuring established taxonomy translations.

These guards protect data shape; they do not replace authorization. Plugin
settings and translation mutations continue to use core admin authentication
and specific RBAC capabilities.
