// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"strings"
	"testing"
)

const homePage = "templates/pages/home.tmpl"

func TestTemplateScriptRules(t *testing.T) {
	cases := []struct {
		desc, content, rule string
	}{
		{"script element", `<script>alert(1)</script>`, RuleScriptTag},
		{"upper case", `<SCRIPT>alert(1)</SCRIPT>`, RuleScriptTag},
		{"external script", `<script src="/static/x.css"></script>`, RuleScriptTag},
		{"json-ld with src", `<script type="application/ld+json" src="/x.json"></script>`, RuleScriptTag},
		{"module script", `<script type="module">x</script>`, RuleScriptTag},
		{"split by an action", `<scr{{""}}ipt>alert(1)</script>`, RuleScriptTag},
		{"name from an action", `<scr{{"ipt"}}>alert(1)</script>`, RuleScriptDynamic},
		{"name from branches", `<{{if .A}}script{{else}}div{{end}}>x</div>`, RuleScriptDynamic},
		{"hidden in a comment", `<!-- <script>alert(1)</script> -->`, RuleScriptTag},
		{"hidden in raw text", `<textarea><script>alert(1)</script></textarea>`, RuleScriptTag},
		{"after plaintext", `{{if .A}}<plaintext>{{end}}<img src="/x.png" onerror="alert(1)">`, RuleScriptHandler},
		{"event handler", `<a href="/" onclick="go()">x</a>`, RuleScriptHandler},
		{"svg event handler", `<svg onload="go()"></svg>`, RuleScriptHandler},
		{"handler split by an action", `<a on{{""}}click="go()">x</a>`, RuleScriptHandler},
		{"handler from an action", `<a o{{"nclick"}}="go()">x</a>`, RuleScriptDynamic},
		{"handler from branches", `<a {{if .A}}on{{end}}{{if .B}}click{{end}}="go()">x</a>`, RuleScriptDynamic},
		{"handler from two actions", `<a {{"o"}}{{"nclick"}}="go()">x</a>`, RuleScriptDynamic},
		{"javascript URL", `<a href="javascript:alert(1)">x</a>`, RuleScriptURL},
		{"entity-encoded scheme", `<a href="&#106;avascript:alert(1)">x</a>`, RuleScriptURL},
		{"tab in scheme", `<a href="java&#9;script:alert(1)">x</a>`, RuleScriptURL},
		{"scheme split by an action", `<a href="java{{""}}script:alert(1)">x</a>`, RuleScriptURL},
		{"scheme split by branches", `<a href="{{if .A}}java{{else}}x{{end}}script:alert(1)">x</a>`, RuleScriptURL},
		{"vbscript", `<form action="vbscript:x"></form>`, RuleScriptURL},
		{"data URL link", `<a href="data:text/html,x">x</a>`, RuleScriptURL},
		{"data URL frame", `<iframe src="data:text/html,x"></iframe>`, RuleScriptURL},
		{"script URL in another attribute", `<svg><set attributeName="href" to="javascript:alert(1)"/></svg>`, RuleScriptURL},
		{"script URL in xlink:href", `<svg><a xlink:href="javascript:alert(1)">x</a></svg>`, RuleScriptURL},
		{"srcdoc", `<iframe srcdoc="x"></iframe>`, RuleScriptEmbed},
		{"object", `<object data="/x.svg"></object>`, RuleScriptEmbed},
		{"embed", `<embed src="/x.svg">`, RuleScriptEmbed},
		{"applet", `<applet code="x"></applet>`, RuleScriptEmbed},
		{"base", `<base href="/">`, RuleScriptBase},
		{"meta refresh", `<meta http-equiv="Refresh" content="0; url=/x">`, RuleScriptRefresh},
		{"CSS expression", `<div style="width: expression(alert(1))">x</div>`, RuleScriptCSS},
		{"CSS behavior", `<style>.a { behavior: url(x.htc) }</style>`, RuleScriptCSS},
		{"CSS binding", `<style>.a { -moz-binding: url(x.xml#x) }</style>`, RuleScriptCSS},
		{"CSS escape", `<style>.a { width: \65 xpression(1) }</style>`, RuleScriptCSS},
		{"CSS script URL", `<style>.a { background: url("javascript:alert(1)") }</style>`, RuleScriptCSS},
		{"preloaded script", `<link rel="preload" as="script" href="/x.css">`, ""},
		{"external preloaded script", `<link rel="modulepreload" href="https://fonts.googleapis.com/x.mjs">`, RuleScriptURL},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			report := check(t, withPage(t, tc.content))
			if tc.rule == "" {
				expectOK(t, report)
				return
			}
			expectProblem(t, report, tc.rule, homePage)
		})
	}
}

func TestTemplateMarkupThatPasses(t *testing.T) {
	cases := map[string]string{
		"json-ld":              `<script type="application/ld+json">{"@type": "Thing", "name": "{{.Title}}"}</script>`,
		"conditional attr":     `<input type="checkbox" {{if .A}}checked{{end}} {{if .B}}disabled{{end}}>`,
		"action as attribute":  `<option value="1"{{if .A}} selected{{end}}>x</option>`,
		"data attribute":       `<div data-{{.Name}}="1" data-gp-toggle="menu">x</div>`,
		"dynamic URL":          `<a href="{{.URL}}">x</a><a href="mailto:{{.Email}}">m</a><a href="tel:{{.Phone}}">t</a>`,
		"relative with colon":  `<a href="/search?q=a:b">x</a>`,
		"data image":           `<img src="data:image/png;base64,AAAA" alt="">`,
		"scroll behavior":      `<style>html { scroll-behavior: smooth; overscroll-behavior: none; }</style>`,
		"selector string":      `<style>a[href^="javascript:"]::after { content: ""; }</style>`,
		"link to another site": `<a href="https://example.com/">x</a>`,
		"plain words":          `<p title="Use JavaScript wisely">only text mentions script tags</p>`,
		"action host":          `<img src="https://{{.Host}}/a.png" alt="">`,
		"safe HTML":            `{{safeHTML .Body}}{{embedHTML .Embed}}`,
	}
	for desc, content := range cases {
		t.Run(desc, func(t *testing.T) {
			expectOK(t, check(t, withPage(t, content)))
		})
	}
}

func TestStaticFileScriptRules(t *testing.T) {
	cases := []struct {
		desc, path, content, rule string
	}{
		{"svg script", "static/a.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, RuleScriptTag},
		{"svg handler", "static/a.svg", `<svg onload="alert(1)"></svg>`, RuleScriptHandler},
		{"svg foreignObject", "static/a.svg", `<svg><foreignObject><p>x</p></foreignObject></svg>`, RuleScriptSVG},
		{"svg entity", "static/a.svg", `<!DOCTYPE svg [<!ENTITY x "javascript:alert(1)">]><svg><a href="&x;">x</a></svg>`, RuleScriptSVG},
		{"svg stylesheet", "static/a.svg", `<?xml-stylesheet href="x.xsl" type="text/xsl"?><svg></svg>`, RuleScriptSVG},
		{"svg script URL", "static/a.svg", `<svg><a xlink:href="javascript:alert(1)">x</a></svg>`, RuleScriptURL},
		{"html script", "static/page.html", `<p>x</p><script>alert(1)</script>`, RuleScriptTag},
		{"css expression", "static/css/x.css", `.a { width: expression(alert(1)); }`, RuleScriptCSS},
		{"demo svg", "demo/logo.svg", `<svg onload="alert(1)"></svg>`, RuleScriptHandler},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			report := check(t, sample(t, map[string]string{tc.path: tc.content}))
			expectProblem(t, report, tc.rule, tc.path)
		})
	}
	ok := `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><style>.a{fill:red}</style><rect class="a" width="1" height="1"/></svg>`
	expectOK(t, check(t, sample(t, map[string]string{"static/ok.svg": ok})))
}

func TestExternalResourceRules(t *testing.T) {
	declared := `
[[external]]
host = "*.example.org"
kinds = ["image", "frame"]
purpose = "Images and embeds"
`
	files := func(page string, extra map[string]string) map[string]string {
		changes := map[string]string{homePage: `{{define "content"}}` + page + `{{end}}`}
		for k, v := range extra {
			changes[k] = v
		}
		f := sample(t, changes)
		f["theme.toml"] += declared
		return f
	}
	cases := []struct {
		desc, page, rule string
		extra            map[string]string
	}{
		{"undeclared image", `<img src="https://cdn.example.com/a.png" alt="">`, RuleExternalUndeclared, nil},
		{"undeclared second srcset URL", `<img srcset="/a.png 1x, https://cdn.example.com/b.png 2x" alt="">`, RuleExternalUndeclared, nil},
		{"undeclared stylesheet", `<link rel="stylesheet" href="//cdn.example.com/x.css">`, RuleExternalUndeclared, nil},
		{"undeclared video", `<video src="https://cdn.example.com/a.mp4" poster="/p.png"></video>`, RuleExternalUndeclared, nil},
		{"undeclared background", `<div style="background-image: url('https://cdn.example.com/a.png')">x</div>`, RuleExternalUndeclared, nil},
		{"wrong kind", `<img src="https://fonts.gstatic.com/a.png" alt="">`, RuleExternalKind, nil},
		{"wildcard misses the apex", `<img src="https://example.org/a.png" alt="">`, RuleExternalUndeclared, nil},
		{"wildcard covers a subdomain", `<img src="https://img.example.org/a.png" alt=""><iframe src="https://video.example.org/e/1"></iframe>`, "", nil},
		{"wrong kind under the wildcard", `<video src="https://img.example.org/a.mp4"></video>`, RuleExternalKind, nil},
		{"link is not a load", `<a href="https://cdn.example.com/">x</a><form action="https://cdn.example.com/f"></form>`, "", nil},
		{"preconnect is not a load", `<link rel="preconnect" href="https://cdn.example.com">`, "", nil},
		{"css url", ``, RuleExternalUndeclared, map[string]string{"static/css/style.css": `.a { background: url(https://cdn.example.com/a.png); }`}},
		{"css import", ``, RuleExternalUndeclared, map[string]string{"static/css/style.css": `@import "https://cdn.example.com/x.css";`}},
		{"declared font", ``, "", map[string]string{"static/css/style.css": `@font-face { font-family: X; src: url(https://fonts.gstatic.com/x.woff2) format("woff2"); }`}},
		{"font host used for an image", ``, RuleExternalKind, map[string]string{"static/css/style.css": `.a { background: url(https://fonts.gstatic.com/x.png); }`}},
		{"import of a declared stylesheet", ``, "", map[string]string{"static/css/style.css": `@import url("https://fonts.googleapis.com/css2?family=Inter");`}},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			report := check(t, files(tc.page, tc.extra))
			if tc.rule == "" {
				expectOK(t, report)
				return
			}
			expectProblem(t, report, tc.rule, "")
		})
	}

	report := check(t, files(`<img src="http://img.example.org/a.png" alt="">`, nil))
	expectOK(t, report)
	if !hasWarning(report, RuleExternalInsecure) {
		t.Fatalf("plain http was not warned about: %v", report.Warnings)
	}
}

func TestProblemsPointAtTheTemplateLine(t *testing.T) {
	files := sample(t, map[string]string{
		"templates/partials/footer.tmpl": "{{define \"footer\"}}\n<p>a</p>\n<a onclick=\"x()\">b</a>\n{{end}}",
		"templates/layouts/base.tmpl":    strings.Replace(sample(t, nil)["templates/layouts/base.tmpl"], "</body>", `{{template "footer" .}}</body>`, 1),
	})
	report := check(t, files)
	for _, p := range report.Problems {
		if p.Rule == RuleScriptHandler {
			if p.Path != "templates/partials/footer.tmpl" || p.Line != 3 {
				t.Fatalf("problem at %s:%d", p.Path, p.Line)
			}
			return
		}
	}
	t.Fatalf("no handler problem in %v", report.Problems)
}
