// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"strings"
	"testing"
)

// withManifest replaces a line of the sample theme.toml (or appends when old
// is empty).
func withManifest(t *testing.T, old, replacement string) map[string]string {
	t.Helper()
	files := sample(t, nil)
	manifest := files["theme.toml"]
	if old == "" {
		files["theme.toml"] = manifest + "\n" + replacement + "\n"
		return files
	}
	if !strings.Contains(manifest, old) {
		t.Fatalf("theme.toml has no %q", old)
	}
	files["theme.toml"] = strings.Replace(manifest, old, replacement, 1)
	return files
}

func TestManifestRules(t *testing.T) {
	cases := []struct {
		desc, old, replacement, rule string
	}{
		{"missing slug", `slug = "minimal-sample"`, ``, RuleManifestSlug},
		{"bad slug", `slug = "minimal-sample"`, `slug = "Minimal_Sample"`, RuleManifestSlug},
		{"missing name", `name = "Minimal Sample"`, ``, RuleManifestName},
		{"missing version", `version = "1.0.0"`, ``, RuleManifestVersion},
		{"bad version", `version = "1.0.0"`, `version = "one"`, RuleManifestVersion},
		{"missing core", `core = ">=0.7.0"`, ``, RuleManifestCore},
		{"bad core", `core = ">=0.7.0"`, `core = "newest"`, RuleManifestCore},
		{"bad plugin slug", ``, "[[requires.plugins]]\nslug = \"Bad_Slug\"", RuleManifestRequires},
		{"bad plugin version", ``, "[[requires.plugins]]\nslug = \"multilang\"\nversion = \"soon\"", RuleManifestRequires},
		{"reserved setting key", `key = "hero_title"`, `key = "plugin_shop_currency"`, RuleManifestSettings},
		{"script kind", `kinds = ["style"]`, `kinds = ["script"]`, RuleExternalInvalid},
		{"bad host", `host = "fonts.googleapis.com"`, `host = "https://fonts.googleapis.com/"`, RuleExternalInvalid},
		{"no purpose", `purpose = "Web font stylesheet"`, ``, RuleExternalInvalid},
		{"missing page template", `template = "single"`, `template = "landing"`, RuleManifestPageTemplate},
		{"missing screenshot", `author = "GoPress"`, "author = \"GoPress\"\nscreenshot = \"screenshot.png\"", RuleManifestScreenshot},
		{"syntax", `[theme]`, `[theme`, RuleManifestSyntax},
		{"external image default", "translatable = true", "translatable = true\n\n[[settings]]\nkey = \"hero_image\"\ntype = \"image\"\ndefault = \"https://cdn.example.com/hero.png\"", RuleExternalUndeclared},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			expectProblem(t, check(t, withManifest(t, tc.old, tc.replacement)), tc.rule, "theme.toml")
		})
	}

	t.Run("missing manifest", func(t *testing.T) {
		expectProblem(t, check(t, sample(t, map[string]string{"theme.toml": ""})), RuleManifestMissing, "theme.toml")
	})
	t.Run("unknown key", func(t *testing.T) {
		report := check(t, withManifest(t, `author = "GoPress"`, "author = \"GoPress\"\ncolour = \"red\""))
		expectOK(t, report)
		if !hasWarning(report, RuleManifestUnknown) {
			t.Fatalf("warnings = %v", report.Warnings)
		}
	})
	t.Run("reserved slug", func(t *testing.T) {
		report := check(t, sample(t, nil), Options{Reserved: func(slug string) bool { return slug == "minimal-sample" }})
		expectProblem(t, report, RuleManifestReserved, "theme.toml")
	})
	t.Run("core version", func(t *testing.T) {
		expectProblem(t, check(t, sample(t, nil), Options{CoreVersion: "0.6.9"}), RuleManifestCoreUnmet, "theme.toml")
		expectOK(t, check(t, sample(t, nil), Options{CoreVersion: "0.7.22"}))
	})
	t.Run("identity", func(t *testing.T) {
		report := check(t, withManifest(t, `version = "1.0.0"`, `version = "2.1.0-beta.1"`))
		expectOK(t, report)
		if report.ID.Slug != "minimal-sample" || report.ID.Version != "2.1.0-beta.1" || report.ID.Kind != Kind {
			t.Fatalf("id = %+v", report.ID)
		}
	})
}

func TestFileRules(t *testing.T) {
	cases := []struct {
		desc, path, content, rule string
	}{
		{"script file", "static/js/main.js", "alert(1)", RuleScriptFile},
		{"module script", "static/app.mjs", "x", RuleScriptFile},
		{"unknown type", "static/tool.exe", "MZ", RuleFileType},
		{"go file", "theme.go", "package x", RuleFileType},
		{"root file", "notes.txt", "x", RuleFilePlace},
		{"unknown directory", "assets/a.css", "a{}", RuleFilePlace},
		{"template outside templates", "static/x.tmpl", "x", RuleFilePlace},
		{"toml outside demo", "static/x.toml", "a = 1", RuleFilePlace},
		{"locale in a subdirectory", "locales/zh/x.json", "{}", RuleFilePlace},
		{"locale of objects", "locales/zh.json", `{"a": {"b": "c"}}`, RuleFileJSON},
		{"broken locale", "locales/admin/zh-CN.json", `{"a": `, RuleFileJSON},
		{"broken demo toml", "demo/data/seed.toml", "[[posts]\n", RuleFileTOML},
		{"other signature", "signatures/other.json", "{}", RuleFilePlace},
		{"admin template", "templates/admin/theme_settings.tmpl", "x", RuleTemplateAdmin},
		{"classic template", "templates/index.tmpl", "x", RuleTemplatePlace},
		{"nested page", "templates/pages/blog/list.tmpl", "x", RuleTemplatePlace},
		{"html template", "templates/pages/x.html", "x", RuleTemplatePlace},
		{"png that is not", "static/a.png", "<html>", RuleFileContent},
		{"font that is not", "static/f.woff2", "wOFF", RuleFileContent},
		{"binary css", "static/a.css", "a\x00b", RuleFileContent},
		{"latin-1 text", "README.md", "caf\xe9", RuleFileContent},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			expectProblem(t, check(t, sample(t, map[string]string{tc.path: tc.content})), tc.rule, tc.path)
		})
	}

	ok := map[string]string{
		"README.md":                 "# Minimal",
		"LICENSE":                   "MIT",
		"screenshot.png":            "\x89PNG\r\n\x1a\nrest",
		"static/img/a.jpg":          "\xff\xd8\xff\xe0",
		"static/img/a.gif":          "GIF89a",
		"static/img/a.webp":         "RIFF\x00\x00\x00\x00WEBPVP8 ",
		"static/img/a.avif":         "\x00\x00\x00\x1cftypavif",
		"static/fonts/a.woff2":      "wOF2",
		"static/fonts/a.ttf":        "\x00\x01\x00\x00",
		"static/favicon.ico":        "\x00\x00\x01\x00",
		"locales/admin/en.json":     `{"hero_title": "Hero title"}`,
		"demo/data/seed.toml":       "[[posts]]\ntitle = \"Hello\"\n",
		"signatures/publisher.json": "{}",
	}
	report := check(t, sample(t, ok))
	expectOK(t, report)

	t.Run("unused layout", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{"templates/layouts/wide.tmpl": `{{define "wide"}}{{end}}`}))
		expectOK(t, report)
		if !hasWarning(report, RuleTemplateUnused) {
			t.Fatalf("warnings = %v", report.Warnings)
		}
	})
}

func TestTemplateRules(t *testing.T) {
	t.Run("unknown function", func(t *testing.T) {
		report := check(t, withPage(t, "\n<p>{{productCard .}}</p>"))
		expectProblem(t, report, RuleTemplateParse, homePage)
		for _, p := range report.Problems {
			if p.Rule == RuleTemplateParse && (p.Line != 2 || !strings.Contains(p.Message, "productCard")) {
				t.Fatalf("problem = %+v", p)
			}
		}
	})
	t.Run("syntax", func(t *testing.T) {
		expectProblem(t, check(t, withPage(t, "{{if .A}}x")), RuleTemplateParse, homePage)
	})
	t.Run("escaping", func(t *testing.T) {
		expectProblem(t, check(t, withPage(t, `{{if .A}}<a href="{{else}}<b>{{end}}x`)), RuleTemplateEscape, homePage)
	})
	t.Run("escaping in a partial", func(t *testing.T) {
		partial := `{{define "header"}}<header class="{{if .A}}x{{else}}"{{end}}"></header>{{end}}`
		expectProblem(t, check(t, sample(t, map[string]string{"templates/partials/header.tmpl": partial})), RuleTemplateEscape, "templates/partials/header.tmpl")
	})
	t.Run("all core functions", func(t *testing.T) {
		page := `<p>{{T .Ctx "x"}} {{seoHead .SEO}} {{renderHook "x" .}} {{responsiveImage "/a.png" "a" "" "" "lazy"}}</p>
{{range taxonomyTerms .Ctx "category"}}{{.Name}}{{end}}{{with contentBySlug .Ctx "page" "about"}}{{.Title}}{{end}}
{{if canCurrentUser .Ctx "post" "create"}}<a href="{{buildURL "post" "x"}}">x</a>{{end}}`
		expectOK(t, check(t, withPage(t, page)))
	})
	t.Run("no layout", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{"templates/layouts/base.tmpl": ""}))
		expectProblem(t, report, RuleTemplateLayout, "templates/layouts/base.tmpl")
	})
	t.Run("no pages", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{homePage: "", "templates/pages/single.tmpl": "", "templates/pages/404.tmpl": ""}))
		expectProblem(t, report, RuleTemplatePages, "templates/pages/")
	})
	t.Run("no home page", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{homePage: ""}))
		expectProblem(t, report, RuleTemplateHome, "templates/pages/")
		if hasProblem(report, RuleTemplatePages, "") {
			t.Fatalf("a package with pages also reported %s: %v", RuleTemplatePages, report.Problems)
		}
	})
	t.Run("front-page or index is a home page", func(t *testing.T) {
		for _, name := range []string{"front-page", "index"} {
			files := sample(t, map[string]string{homePage: ""})
			files["templates/pages/"+name+".tmpl"] = `{{define "content"}}<p>home</p>{{end}}`
			expectOK(t, check(t, files))
		}
	})
	t.Run("no base template", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{"templates/layouts/base.tmpl": `{{define "layout"}}<p>x</p>{{end}}`}))
		expectProblem(t, report, RuleTemplateBase, "templates/layouts/base.tmpl")
	})
	t.Run("broken partial still scans pages", func(t *testing.T) {
		report := check(t, sample(t, map[string]string{
			"templates/partials/header.tmpl": `{{define "header"}}{{nope}}{{end}}`,
			homePage:                         `{{define "content"}}<a onclick="x()">x</a>{{end}}`,
		}))
		expectProblem(t, report, RuleTemplateParse, "templates/partials/header.tmpl")
		expectProblem(t, report, RuleScriptHandler, homePage)
	})
	t.Run("broken page still scans its text", func(t *testing.T) {
		report := check(t, withPage(t, `{{nope}}<script>x</script>`))
		expectProblem(t, report, RuleTemplateParse, homePage)
		expectProblem(t, report, RuleScriptTag, homePage)
	})
	t.Run("recursive template", func(t *testing.T) {
		page := `{{template "tree" .}}{{end}}{{define "tree"}}<ul>{{range .Children}}<li>{{.Title}}{{template "tree" .}}</li>{{end}}</ul>`
		expectOK(t, check(t, withPage(t, page)))
	})
}
