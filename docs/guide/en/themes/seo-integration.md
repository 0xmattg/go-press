# Theme SEO Integration

GoPress centralizes SEO data in the framework. Themes should consume SEO metadata rather than inventing their own title, canonical, Open Graph, or JSON-LD pipelines.

## Data Flow

```text
System Settings
  -> SEOBuilder
  -> ApplySiteOptionOverrides
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
  {{if $siteIcon}}<link rel="icon" href="{{$siteIcon}}">
  <link rel="apple-touch-icon" href="{{$siteIcon}}">{{end}}
{{end}}
```

`seoHeadFor` works with both `gin.H` and custom structs. If no SEO field is available, it returns an empty value and lets the template fallback run. The fallback should still render `site_icon` from settings so static pages and partially wired themes keep a working favicon.

### Use `site_icon` for favicons

The admin **System Settings** page stores the site favicon in `site_icon`. Themes should not introduce theme-local favicon keys. When `SEOMeta.SiteIcon` is set, `seoHeadFor` renders both `<link rel="icon">` and `<link rel="apple-touch-icon">`. When a page has no SEO object and falls back to the `else` branch, the layout should render the same two tags from `site_icon`.

## BaseTheme Path

With `BaseTheme + gin.H`, GoPress injects SEO automatically for home, archive, taxonomy, and single pages.

## Custom PageData Path

Custom PageService themes must attach `rewrite.SEOMeta` to their page data and call:

```go
coreTheme.ApplySiteOptionOverrides(app, &seo)
coreTheme.ApplyContentMetaSEO(hooks, contentRepo, &seo, item)
```

`ApplySiteOptionOverrides` applies runtime settings such as `site_name`, `site_description`, and `site_icon`. The second call is what allows plugins such as `seo-extras` to patch per-content SEO output.

## Per-content SEO

Activate the `seo-extras` plugin when editors need per-content SEO title, description, Open Graph image, or robots overrides. Themes that follow the contracts above receive those values automatically.
