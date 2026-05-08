# Theme SEO Integration

GoPress centralizes SEO data in the framework. Themes should consume SEO metadata rather than inventing their own title, canonical, Open Graph, or JSON-LD pipelines.

## Data Flow

```text
System Settings
  -> SEOBuilder
  -> ApplySiteOptionOverrides
  -> optional seo.content.meta filters
  -> data["SEO"] or PageData.SEO
  -> {{seoHeadFor .}}
  -> HTML head
```

## Required Contracts

### Use `site_name` for titles

```html
<title>{{.Title}} - {{settingOr .Settings "site_name" "My Theme"}}</title>
```

Do not hard-code the brand name in `<title>`, and do not use theme-local keys such as `company_name` for SEO titles.

### Use `seoHeadFor`

```gotemplate
{{with seoHeadFor .}}
  {{.}}
{{else}}
  <meta name="description" content="{{settingOr $.Settings "site_description" "Default description"}}">
{{end}}
```

`seoHeadFor` works with both `gin.H` and custom structs. If no SEO field is available, it returns an empty value and lets the template fallback run.

## BaseTheme Path

With `BaseTheme + gin.H`, GoPress injects SEO automatically for home, archive, taxonomy, and single pages.

## Custom PageData Path

Custom PageService themes must attach `rewrite.SEOMeta` to their page data and call:

```go
coreTheme.ApplySiteOptionOverrides(app, &seo)
coreTheme.ApplyContentMetaSEO(hooks, contentRepo, &seo, item)
```

The second call is what allows plugins such as `seo-extras` to patch per-content SEO output.

## Per-content SEO

Activate the `seo-extras` plugin when editors need per-content SEO title, description, Open Graph image, or robots overrides. Themes that follow the contracts above receive those values automatically.

