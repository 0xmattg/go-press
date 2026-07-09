# Theme SEO Integration

GoPress centralizes SEO data in the framework. Themes should consume SEO metadata rather than inventing their own title, canonical, Open Graph, or JSON-LD pipelines.

## Data Flow

```text
System Settings
  -> SEOBuilder
  -> ApplySiteOptionOverridesForRequest
  -> optional seo.content.meta filters
  -> data["SEO"] or PageData.SEO
  -> {{pageTitleFor . $fallbackTitle}} and {{seoHeadFor .}}
  -> HTML head, including <title> and favicon links when site_icon is set
```

## Required Contracts

### Use `pageTitleFor` for document titles

```gotemplate
{{$fallbackTitle := printf "%s - %s" .Title (settingOr .Settings "site_name" "My Theme")}}
<title>{{pageTitleFor . $fallbackTitle}}</title>
```

`pageTitleFor` is a core page-metadata helper, not a plugin dependency. It returns the current page title from core metadata when available, including values changed by optional filters, and otherwise returns the theme fallback. Do not hard-code the brand name in `<title>`, and do not use theme-local keys such as `company_name` for document titles.

### Use `seoHeadFor`

```gotemplate
{{$siteIcon := settingOr .Settings "site_icon" ""}}
{{with seoHeadFor .}}
  {{.}}
{{else}}
  <meta name="description" content="{{settingOr $.Settings "site_description" "Default description"}}">
  {{faviconLinks $siteIcon}}
{{end}}
```

`seoHeadFor` works with both `gin.H` and custom structs. If no SEO field is available, it returns an empty value and lets the template fallback run. The fallback should still render `site_icon` from settings so static pages and partially wired themes keep a working favicon.

### Use `site_icon` for favicons

The admin **System Settings** page stores the site favicon source in `site_icon`. Themes should not introduce theme-local favicon keys. When `SEOMeta.SiteIcon` is set, `seoHeadFor` renders `/favicon.ico` first, then the typed image icon and Apple touch icon. When a page has no SEO object and falls back to the `else` branch, the layout should call `{{faviconLinks $siteIcon}}` so the output stays identical.

The generated `/favicon.ico`, `/static/*` assets, `/sitemap.xml`, and `/robots.txt` support `HEAD` as well as `GET`. Missing static files return `Cache-Control: no-store` so crawler or CDN caches do not keep stale 404 favicon/image checks.

## BaseTheme Path

With `BaseTheme + gin.H`, GoPress injects SEO automatically for home, archive, taxonomy, and single pages.

For archive pages, set `archive_title_key` on `[[content_types]]` when the frontend title should come from theme locales:

```toml
[[content_types]]
name = "service"
label_plural = "服务列表"
archive_title_key = "page_title_service"
rewrite_slug = "services"
```

Core resolves that key through the current request language before building the archive SEO title. If the key is absent, core tries generic keys such as `page_title_<rewrite_slug>` and then falls back to `label_plural`.

## Custom PageData Path

Custom PageService themes must attach `rewrite.SEOMeta` to their page data and reuse core helpers:

```go
func (s *PageService) ForRequest(c *gin.Context) *PageService {
    clone := *s
    clone.reqCtx = c
    return &clone
}

title := coreTheme.LocalizedArchiveTitle(c, i18nMgr, typeDef)
seo := seoBuilder.ForArchiveTitle(typeDef, title)
coreTheme.ApplySiteOptionOverridesFromOptionsForRequest(c, options, i18nMgr, seoBuilder, &seo)
coreTheme.ApplyContentMetaSEO(hooks, contentRepo, &seo, item)
```

`LocalizedArchiveTitle` keeps archive titles language-aware. `ApplySiteOptionOverridesFromOptionsForRequest` applies runtime settings such as `site_name`, `site_description`, and `site_icon` using the current request language, without removing the page-specific title prefix. `ApplyContentMetaSEO` is what allows plugins such as `seo-extras` to patch per-content SEO output.

If a custom theme still calls `ApplySiteOptionOverridesFromOptions`, single-language SEO remains valid, but multilang site setting translations will not reach `<title>` or `<meta name="description">`. Any theme that builds `SEOMeta` itself should keep the current `gin.Context` and `i18n.Manager` in its request-scoped PageService clone and call the request-aware helper.

Bundled theme guidance:

| Theme pattern | Required integration |
|---|---|
| BaseTheme + `gin.H` themes | No theme work; core injects translated site options automatically. |
| `modern-company`, `atelier-slate-gp`, `financial-news` | Custom PageService using core SEOBuilder; call `ApplySiteOptionOverridesFromOptionsForRequest`. |
| `go-press-landing` | Pass `gin.Context` / `i18n.Manager` into PageService before using the request-aware helper. |
| `bitcuz-mag` | Custom SEO pipeline; translate `site_name` / `site_description` through core i18n or convert generated `SEOMeta` through the request-aware override. |

## Per-content SEO

Activate the `seo-extras` plugin when editors need per-content SEO title, description, Open Graph image, or robots overrides. Themes that follow the contracts above receive those values automatically.
