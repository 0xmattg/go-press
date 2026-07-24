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

## Typed PageData + PageService Path

If you prefer a typed `PageService` + custom data structs, the cost is now low: **embed the core scaffolding**. Data-access plumbing (DB, repositories, options, request scoping) and SEO assembly come for free.

- SEO themes embed **`coreTheme.SEOPageService`** (inherits `BuildHomeSEO` / `BuildArchiveSEO` / `BuildContentSEO`).
- Non-SEO themes embed **`coreTheme.BasePageService`** (data-access plumbing only).

```go
// PageData needs an SEO field for the seoHeadFor helper.
type PageData struct {
    Title    string
    Settings map[string]string
    SEO      rewrite.SEOMeta // <- required
}

// Embed SEOPageService: inherits DB / Content / Tax / Options / SEOBuilder /
// Registry / Hooks / I18n + ReqCtx + the three Build*SEO methods.
type PageService struct {
    coreTheme.SEOPageService
}

func NewPageService(engine *core.Engine) *PageService {
    return &PageService{coreTheme.NewSEOPageService(
        coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options),
        engine.SEO, engine.Registry, engine.Hooks, engine.I18n)}
}

// DB-only constructor (CLI / tests); nil SEO fields make Build*SEO return zero SEOMeta.
func NewPageServiceDB(db *gorm.DB) *PageService {
    return &PageService{coreTheme.NewSEOPageService(coreTheme.NewBasePageServiceDB(db), nil, nil, nil, nil)}
}

// Request scoping: replace the embedded base, preserve custom fields.
func (s *PageService) ForRequest(c *gin.Context) *PageService {
    clone := *s
    clone.BasePageService = s.BasePageService.ForRequest(c)
    return &clone
}

// Get*Data just calls the inherited Build*SEO methods.
func (s *PageService) GetProductDetail(slug string) (*ProductDetailData, error) {
    item, _ := s.Content.FindBySlugScoped(s.ReqCtx, "product", slug)
    data := &ProductDetailData{ /* ... */ }
    data.SEO = s.BuildContentSEO(item, "product") // includes site-option overrides + per-content meta filter
    return data, nil
}
```

`SEOPageService` already calls these core helpers internally (don't call or re-implement them in the theme): `LocalizedArchiveTitle` keeps archive titles language-aware; `ApplySiteOptionOverridesFromOptionsForRequest` applies runtime `site_name` / `site_description` / `site_icon` in the current request language; `ApplyContentMetaSEO` lets plugins such as `seo-extras` patch per-content SEO.

A non-SEO theme embeds only `BasePageService`:

```go
type PageService struct {
    coreTheme.BasePageService
}

func NewPageService(engine *core.Engine) *PageService {
    return &PageService{coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options)}
}
```

> Note: `SEOPageService` uses the request-aware override, so multilang site-setting translations reach `<title>` / `<meta name="description">`. A single-page theme with no per-request i18n (e.g. `go-press-landing`) may instead embed `BasePageService` and write its own `buildHomeSEO` using the non-request `ApplySiteOptionOverridesFromOptions`.

All bundled themes now embed the shared scaffolding:

| Theme | PageService scaffold | SEO source |
|---|---|---|
| `atelier-slate`, `civic-estate`, `florafi`, `terra-trail`, `axis-form` | `coreTheme.BasePageService` | Archive / single injected by BaseTheme; custom pages use `gin.H` |
| `modern-company`, `financial-news` | `coreTheme.SEOPageService` | Inherited `BuildHomeSEO` / `BuildArchiveSEO` / `BuildContentSEO` |
| `go-press-landing` | `coreTheme.BasePageService` + own `buildHomeSEO` | Single page, non-request `ApplySiteOptionOverridesFromOptions` |

## Per-content SEO

Activate the `seo-extras` plugin when editors need per-content SEO title, description, Open Graph image, or robots overrides. Themes that follow the contracts above receive those values automatically.
