package theme

import (
	"html/template"

	"go-press/core/content"
)

// Fallback templates provide minimal but functional rendering when the
// active theme does not include specific template files. This mirrors
// WordPress behaviour where core always has a last-resort template.

var fallbackTaxonomyTmpl *template.Template
var fallbackSingleTmpl *template.Template
var fallbackArchiveTmpl *template.Template

func init() {
	fns := template.FuncMap{
		"safeHTML": func(s string) template.HTML {
			return template.HTML(content.SanitizeHTML(s))
		},
	}
	fallbackTaxonomyTmpl = template.Must(template.New("fallback-taxonomy").Funcs(fns).Parse(fallbackTaxonomyHTML))
	fallbackSingleTmpl = template.Must(template.New("fallback-single").Funcs(fns).Parse(fallbackSingleHTML))
	fallbackArchiveTmpl = template.Must(template.New("fallback-archive").Funcs(fns).Parse(fallbackArchiveHTML))
}

// FallbackTaxonomyTemplate returns the built-in taxonomy archive template.
func FallbackTaxonomyTemplate() *template.Template { return fallbackTaxonomyTmpl }

// FallbackSingleTemplate returns the built-in single content template.
func FallbackSingleTemplate() *template.Template { return fallbackSingleTmpl }

// FallbackArchiveTemplate returns the built-in archive listing template.
func FallbackArchiveTemplate() *template.Template { return fallbackArchiveTmpl }

const fallbackTaxonomyHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;color:#333;line-height:1.6;background:#f9f9f9}
.container{max-width:960px;margin:0 auto;padding:40px 20px}
h1{font-size:28px;margin-bottom:8px}
.taxonomy-label{color:#666;font-size:14px;margin-bottom:24px}
.items{display:grid;gap:20px}
.item{background:#fff;border-radius:8px;padding:20px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.item h2{font-size:18px;margin-bottom:6px}
.item h2 a{color:#2271b1;text-decoration:none}
.item h2 a:hover{text-decoration:underline}
.item .excerpt{color:#666;font-size:14px}
.item .meta{color:#999;font-size:13px;margin-top:6px}
.item .badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;background:#e9ecef;color:#555;margin-right:6px}
.back{display:inline-block;margin-top:32px;color:#2271b1;text-decoration:none;font-size:14px}
</style>
</head>
<body>
<div class="container">
<div class="taxonomy-label">{{.TaxLabel}}</div>
<h1>{{.TermName}}</h1>
<div class="items">
{{range .Items}}
<div class="item">
<h2><a href="{{if $.BuildURL}}{{call $.BuildURL .Type .Slug}}{{else}}/{{.Slug}}{{end}}">{{.Title}}</a></h2>
{{if .Excerpt}}<p class="excerpt">{{.Excerpt}}</p>{{end}}
<div class="meta">
<span class="badge">{{.Type}}</span>
{{if .PublishedAt}}{{.PublishedAt.Format "2006-01-02"}}{{end}}
</div>
</div>
{{else}}
<p style="color:#999">暂无内容</p>
{{end}}
</div>
<a class="back" href="/">← 返回首页</a>
</div>
</body>
</html>`

const fallbackSingleHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;color:#333;line-height:1.8;background:#f9f9f9}
.container{max-width:760px;margin:0 auto;padding:40px 20px}
h1{font-size:28px;margin-bottom:12px}
.meta{color:#999;font-size:14px;margin-bottom:24px}
.meta .badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;background:#e9ecef;color:#555;margin-right:6px}
.content{background:#fff;border-radius:8px;padding:32px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.content img{max-width:100%;height:auto;border-radius:8px}
.tags{margin-top:24px;display:flex;gap:8px;flex-wrap:wrap}
.tag{display:inline-block;padding:4px 12px;background:#e9ecef;border-radius:16px;font-size:13px;color:#555;text-decoration:none}
.back{display:inline-block;margin-top:32px;color:#2271b1;text-decoration:none;font-size:14px}
</style>
</head>
<body>
<div class="container">
<h1>{{.Title}}</h1>
<div class="meta">
{{if .Item.PublishedAt}}{{.Item.PublishedAt.Format "2006-01-02"}}{{end}}
{{range .Categories}}<a class="badge" href="/category/{{.Slug}}">{{.Name}}</a>{{end}}
</div>
<div class="content">{{.Item.Content | safeHTML}}</div>
{{if .Tags}}
<div class="tags">
{{range .Tags}}<a class="tag" href="/tag/{{.Slug}}">{{.Name}}</a>{{end}}
</div>
{{end}}
<a class="back" href="/">← 返回首页</a>
</div>
</body>
</html>`

const fallbackArchiveHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;color:#333;line-height:1.6;background:#f9f9f9}
.container{max-width:960px;margin:0 auto;padding:40px 20px}
h1{font-size:28px;margin-bottom:24px}
.items{display:grid;gap:20px}
.item{background:#fff;border-radius:8px;padding:20px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.item h2{font-size:18px;margin-bottom:6px}
.item h2 a{color:#2271b1;text-decoration:none}
.item h2 a:hover{text-decoration:underline}
.item .excerpt{color:#666;font-size:14px}
.item .meta{color:#999;font-size:13px;margin-top:6px}
.pagination{margin-top:32px;display:flex;gap:8px;justify-content:center}
.pagination a,.pagination span{padding:6px 14px;border-radius:4px;font-size:14px;text-decoration:none}
.pagination a{background:#fff;color:#2271b1;border:1px solid #ddd}
.pagination span{background:#2271b1;color:#fff}
.back{display:inline-block;margin-top:24px;color:#2271b1;text-decoration:none;font-size:14px}
</style>
</head>
<body>
<div class="container">
<h1>{{.Title}}</h1>
<div class="items">
{{range .Items}}
<div class="item">
<h2><a href="{{if $.BuildURL}}{{call $.BuildURL .Type .Slug}}{{else}}/{{.Slug}}{{end}}">{{.Title}}</a></h2>
{{if .Excerpt}}<p class="excerpt">{{.Excerpt}}</p>{{end}}
<div class="meta">{{if .PublishedAt}}{{.PublishedAt.Format "2006-01-02"}}{{end}}</div>
</div>
{{else}}
<p style="color:#999">暂无内容</p>
{{end}}
</div>
<a class="back" href="/">← 返回首页</a>
</div>
</body>
</html>`
