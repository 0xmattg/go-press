// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Layout fallbacks: when a theme has a base layout but no template for a
// page, BaseTheme renders core's generic content block inside the theme's own
// layout, so the page keeps the theme's header, footer, styles and SEO tags.
// The standalone fallbacks in fallback.go remain for themes without a layout
// and for a layout fallback that fails to render.

package theme

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"text/template/parse"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/assets"
	"github.com/0xmattg/go-press/pkg/logger"
)

// LayoutFallbacks holds one template per kind of page, each the theme's
// layout and partials with core's generic "content" block. A nil field means
// that kind has no layout fallback.
type LayoutFallbacks struct {
	Single   *template.Template
	Archive  *template.Template
	Taxonomy *template.Template
	NotFound *template.Template
}

// LoadLayoutFallbacks composes the layout fallbacks for a theme from
// templates/layouts/base.tmpl and templates/partials/, read the way
// LoadPageBundle reads them. It returns nil without an error when the theme
// has no base layout. It returns an error when the layout needs a template
// that only the theme's pages define: core's block supplies "content" and
// nothing else, so such a layout cannot be completed.
func LoadLayoutFallbacks(t Theme) (*LayoutFallbacks, error) {
	tplFS := TemplatesFS(t)
	if tplFS == nil {
		return nil, nil
	}
	const layoutPath = "layouts/base.tmpl"
	layoutSet, err := assets.ParseFiles(template.New("").Funcs(t.TemplateFuncs()), tplFS, layoutPath)
	if err != nil {
		// No layout (or one that does not parse): nothing to wrap.
		return nil, nil
	}
	partials, err := assets.TemplateNames(tplFS, "partials")
	if err != nil {
		return nil, err
	}
	if len(partials) > 0 {
		if layoutSet, err = assets.ParseFiles(layoutSet, tplFS, partials...); err != nil {
			return nil, err
		}
	}
	if layoutSet.Lookup("base") == nil {
		return nil, fmt.Errorf("theme %q: layouts/base.tmpl does not define \"base\"", t.Name())
	}

	out := &LayoutFallbacks{}
	for _, kind := range []struct {
		dst  **template.Template
		body string
	}{
		{&out.Single, layoutSingleContent},
		{&out.Archive, layoutArchiveContent},
		{&out.Taxonomy, layoutTaxonomyContent},
		{&out.NotFound, layoutNotFoundContent},
	} {
		set, err := layoutSet.Clone()
		if err != nil {
			return nil, err
		}
		if set, err = set.Parse(kind.body); err != nil {
			return nil, fmt.Errorf("theme %q: layout fallback: %w", t.Name(), err)
		}
		if missing := missingTemplates(set); len(missing) > 0 {
			return nil, fmt.Errorf("theme %q: the layout uses templates only its pages define: %s", t.Name(), strings.Join(missing, ", "))
		}
		*kind.dst = set
	}
	return out, nil
}

// missingTemplates lists the names that a {{template}} action in the set
// refers to but no template in the set defines.
func missingTemplates(set *template.Template) []string {
	seen := map[string]bool{}
	var missing []string
	var walk func(parse.Node)
	walk = func(n parse.Node) {
		switch n := n.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, child := range n.Nodes {
				walk(child)
			}
		case *parse.IfNode:
			walk(n.List)
			walk(n.ElseList)
		case *parse.RangeNode:
			walk(n.List)
			walk(n.ElseList)
		case *parse.WithNode:
			walk(n.List)
			walk(n.ElseList)
		case *parse.TemplateNode:
			if !seen[n.Name] && set.Lookup(n.Name) == nil {
				missing = append(missing, n.Name)
			}
			seen[n.Name] = true
		}
	}
	for _, tmpl := range set.Templates() {
		if tmpl.Tree != nil {
			walk(tmpl.Tree.Root)
		}
	}
	return missing
}

// renderLayoutFallback renders a layout fallback into a buffer and writes it
// only when it rendered completely, so a failure never leaves half a page.
// It reports whether it wrote the response.
func (b *BaseTheme) renderLayoutFallback(c *gin.Context, tmpl *template.Template, data gin.H, status int) bool {
	if tmpl == nil {
		return false
	}
	data["Ctx"] = c
	data["FallbackText"] = b.fallbackText(c)
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		logger.Error("BaseTheme: layout fallback render error", "error", err)
		return false
	}
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_, _ = c.Writer.Write(buf.Bytes())
	return true
}

// fallbackMessages are the texts of core's generic blocks. A site or theme
// translation of "gopress.fallback.<key>" takes precedence; otherwise
// Chinese requests get the zh texts and every other language the en texts.
var fallbackMessages = map[string]map[string]string{
	"en": {
		"empty":           "Nothing has been published here yet.",
		"back_home":       "Back to the home page",
		"not_found_title": "Page not found",
		"not_found_text":  "The page you are looking for does not exist or has moved.",
		"previous":        "Previous",
		"next":            "Next",
		"page_of":         "Page %d of %d",
		"pagination":      "Pagination",
	},
	"zh": {
		"empty":           "这里还没有发布内容。",
		"back_home":       "返回首页",
		"not_found_title": "页面不存在",
		"not_found_text":  "你要找的页面不存在，或者已经移动。",
		"previous":        "上一页",
		"next":            "下一页",
		"page_of":         "第 %d 页，共 %d 页",
		"pagination":      "分页",
	},
}

// fallbackText returns the texts of core's generic blocks for the request.
func (b *BaseTheme) fallbackText(c *gin.Context) map[string]string {
	lang := ""
	if c != nil {
		if v, ok := c.Get("current_lang"); ok {
			lang, _ = v.(string)
		}
	}
	var translate func(string) string
	if b.App != nil {
		if mgr := b.App.I18nManager(); mgr != nil {
			if lang == "" {
				lang = mgr.DefaultLang()
			}
			translate = func(id string) string { return mgr.Translate(c, id) }
		}
	}
	builtin := fallbackMessages["en"]
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		builtin = fallbackMessages["zh"]
	}
	out := make(map[string]string, len(builtin))
	for key, text := range builtin {
		out[key] = text
		if translate != nil {
			id := "gopress.fallback." + key
			if translated := translate(id); translated != "" && translated != id {
				out[key] = translated
			}
		}
	}
	return out
}

// layoutFallbackStyle styles the generic blocks. It sets no colors, fonts or
// backgrounds: the text inherits them from the theme, so the block fits light
// and dark themes alike. The top padding clears the fixed headers many
// themes use. Theme CSS can restyle every gp-page class.
const layoutFallbackStyle = `<style>
.gp-page{box-sizing:border-box;max-width:760px;margin:0 auto;padding:clamp(96px,12vw,136px) 20px clamp(48px,7vw,88px);line-height:1.7;overflow-wrap:anywhere}
.gp-page *,.gp-page *::before,.gp-page *::after{box-sizing:border-box}
.gp-page__header{margin:0 0 28px}
.gp-page__kicker,.gp-page__meta{margin:0;font-size:.875em;opacity:.7}
.gp-page__title{margin:.2em 0 0;font-size:clamp(28px,4.5vw,44px);line-height:1.15}
.gp-page__summary{margin:12px 0 0;font-size:1.1em;opacity:.85}
.gp-page__meta{margin-top:10px}
.gp-page__cover{margin:0 0 28px}
.gp-page img{max-width:100%;height:auto;border-radius:8px}
.gp-page__body>:first-child{margin-top:0}
.gp-page__body h2,.gp-page__body h3,.gp-page__body h4{margin:1.6em 0 .5em;line-height:1.25}
.gp-page__body p,.gp-page__body ul,.gp-page__body ol,.gp-page__body blockquote,.gp-page__body pre,.gp-page__body table{margin:0 0 1em}
.gp-page__body ul,.gp-page__body ol{padding-left:1.4em}
.gp-page__body ul{list-style:disc}
.gp-page__body ol{list-style:decimal}
.gp-page__body li+li{margin-top:.35em}
.gp-page__body pre,.gp-page__body table{display:block;max-width:100%;overflow:auto}
.gp-page__body blockquote{padding-left:1em;border-left:3px solid rgba(127,127,127,.35)}
.gp-page a{color:inherit;text-decoration:underline;text-underline-offset:3px}
.gp-page a:focus-visible{outline:2px solid currentColor;outline-offset:2px;border-radius:2px}
.gp-page__terms{margin:28px 0 0;padding:0;list-style:none;display:flex;flex-wrap:wrap;gap:8px 16px;font-size:.875em}
.gp-page__list{margin:0;padding:0;list-style:none}
.gp-page__entry{padding:20px 0;border-top:1px solid rgba(127,127,127,.25)}
.gp-page__entry:last-child{border-bottom:1px solid rgba(127,127,127,.25)}
.gp-page__entry h2{margin:0;font-size:1.25em;line-height:1.3}
.gp-page__entry p{margin:6px 0 0;opacity:.85}
.gp-page__empty{margin:0;opacity:.75}
.gp-page__pager{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px 20px;margin-top:28px}
.gp-page__pager span{opacity:.7;font-size:.875em}
</style>`

// The generic blocks use only core template functions and the data the
// matching BaseTheme renderer provides, plus FallbackText (and, for archives,
// FallbackPrevURL and FallbackNextURL).

const layoutSingleContent = `{{define "content"}}` + layoutFallbackStyle + `
<article class="gp-page gp-page--single">
<header class="gp-page__header">
<h1 class="gp-page__title">{{.Item.Title}}</h1>
{{with .Item.Excerpt}}<p class="gp-page__summary">{{.}}</p>{{end}}
{{if ne .ContentType "page"}}{{with .Item.PublishedAt}}<p class="gp-page__meta"><time datetime="{{.Format "2006-01-02"}}">{{.Format "2006-01-02"}}</time></p>{{end}}{{end}}
</header>
{{with .Item.ImageURL}}<figure class="gp-page__cover"><img src="{{.}}" alt="{{$.Item.Title}}"></figure>{{end}}
<div class="gp-page__body">{{safeHTML .Item.Content}}</div>
{{if ne .ContentType "page"}}{{if or .Categories .Tags}}<ul class="gp-page__terms">{{range .Categories}}<li><a href="{{langPrefixURL $.Ctx .URL}}">{{.Name}}</a></li>{{end}}{{range .Tags}}<li><a href="{{langPrefixURL $.Ctx .URL}}">#{{.Name}}</a></li>{{end}}</ul>{{end}}{{end}}
</article>
{{end}}`

const layoutArchiveContent = `{{define "content"}}` + layoutFallbackStyle + `
<section class="gp-page gp-page--archive">
<header class="gp-page__header"><h1 class="gp-page__title">{{.Title}}</h1></header>
{{if .Items}}<ul class="gp-page__list">
{{range .Items}}<li class="gp-page__entry">
<h2>{{if .URL}}<a href="{{langPrefixURL $.Ctx .URL}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}</h2>
{{with .PublishedAt}}<p class="gp-page__meta"><time datetime="{{.Format "2006-01-02"}}">{{.Format "2006-01-02"}}</time></p>{{end}}
{{with .Excerpt}}<p>{{.}}</p>{{end}}
</li>{{end}}
</ul>{{else}}<p class="gp-page__empty">{{.FallbackText.empty}}</p>{{end}}
{{with .Pagination}}{{if gt .TotalPages 1}}<nav class="gp-page__pager" aria-label="{{$.FallbackText.pagination}}">
{{with $.FallbackPrevURL}}<a href="{{.}}" rel="prev">{{$.FallbackText.previous}}</a>{{else}}<span></span>{{end}}
<span>{{printf $.FallbackText.page_of .Page .TotalPages}}</span>
{{with $.FallbackNextURL}}<a href="{{.}}" rel="next">{{$.FallbackText.next}}</a>{{else}}<span></span>{{end}}
</nav>{{end}}{{end}}
</section>
{{end}}`

const layoutTaxonomyContent = `{{define "content"}}` + layoutFallbackStyle + `
<section class="gp-page gp-page--taxonomy">
<header class="gp-page__header">{{with .TaxLabel}}<p class="gp-page__kicker">{{.}}</p>{{end}}<h1 class="gp-page__title">{{.TermName}}</h1></header>
{{if .Items}}<ul class="gp-page__list">
{{range .Items}}<li class="gp-page__entry">
<h2>{{if .DetailURL}}<a href="{{langPrefixURL $.Ctx .DetailURL}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}</h2>
<p class="gp-page__meta">{{.TypeLabel}}{{with .PublishedAt}} · <time datetime="{{.Format "2006-01-02"}}">{{.Format "2006-01-02"}}</time>{{end}}</p>
{{with .Excerpt}}<p>{{.}}</p>{{end}}
</li>{{end}}
</ul>{{else}}<p class="gp-page__empty">{{.FallbackText.empty}}</p>{{end}}
</section>
{{end}}`

const layoutNotFoundContent = `{{define "content"}}` + layoutFallbackStyle + `
<section class="gp-page gp-page--not-found">
<header class="gp-page__header"><p class="gp-page__kicker">404</p><h1 class="gp-page__title">{{.FallbackText.not_found_title}}</h1><p class="gp-page__summary">{{.FallbackText.not_found_text}}</p></header>
<p><a href="{{langPrefixURL .Ctx "/"}}">{{.FallbackText.back_home}}</a></p>
</section>
{{end}}`
