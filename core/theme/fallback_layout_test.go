// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
)

// layoutTheme is an InitBaseFS theme with the given template files.
func layoutTheme(t *testing.T, files map[string]string) *fsTestTheme {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["templates/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	th := &fsTestTheme{}
	th.InitBaseFS(nil, "", fsys, fsTestThemeTOML, nil)
	return th
}

var layoutFiles = map[string]string{
	"layouts/base.tmpl":    `{{define "base"}}<html><body>{{template "header" .}}<main>{{template "content" .}}</main></body></html>{{end}}`,
	"partials/header.tmpl": `{{define "header"}}<header>THEME</header>{{end}}`,
	"pages/home.tmpl":      `{{define "content"}}home{{end}}`,
}

func loadFallbacks(t *testing.T, files map[string]string) *LayoutFallbacks {
	t.Helper()
	fallbacks, err := LoadLayoutFallbacks(coreFuncsTheme{layoutTheme(t, files)})
	if err != nil {
		t.Fatal(err)
	}
	return fallbacks
}

// coreFuncsTheme gives a test theme BaseTheme's template functions, as a
// real theme has them.
type coreFuncsTheme struct{ *fsTestTheme }

func (t coreFuncsTheme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }

func testContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, w
}

func TestLayoutFallbacksWrapCoreBlocksInTheThemeLayout(t *testing.T) {
	fallbacks := loadFallbacks(t, layoutFiles)
	if fallbacks == nil || fallbacks.Single == nil || fallbacks.Archive == nil || fallbacks.Taxonomy == nil || fallbacks.NotFound == nil {
		t.Fatalf("fallbacks = %+v", fallbacks)
	}
	b := &BaseTheme{}
	published := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

	c, w := testContext("/terms")
	ok := b.renderLayoutFallback(c, fallbacks.Single, gin.H{"ContentType": "page", "Item": map[string]interface{}{
		"Title": "Terms <of use>", "Excerpt": "", "ImageURL": "", "PublishedAt": &published,
		"Content": `<h2>Accounts</h2><p>Body</p><script>alert(1)</script>`,
	}}, http.StatusOK)
	html := w.Body.String()
	if !ok || w.Code != http.StatusOK || !strings.HasPrefix(html, "<html><body><header>THEME</header><main>") || !strings.HasSuffix(html, "</main></body></html>") {
		t.Fatalf("single = %v %d %s", ok, w.Code, html)
	}
	for _, want := range []string{`class="gp-page gp-page--single"`, "Terms &lt;of use&gt;", "<h2>Accounts</h2><p>Body</p>", "<style>"} {
		if !strings.Contains(html, want) {
			t.Errorf("single lacks %q", want)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "<time") {
		t.Errorf("single: unsanitized body, or a date on a page: %s", html)
	}

	// Other types show their date and terms.
	c, w = testContext("/blog/hello")
	b.renderLayoutFallback(c, fallbacks.Single, gin.H{"ContentType": "post",
		"Item":       map[string]interface{}{"Title": "Hello", "Excerpt": "Short", "ImageURL": "/uploads/a.png", "PublishedAt": &published, "Content": "<p>x</p>"},
		"Categories": []map[string]interface{}{{"Name": "News", "URL": "/category/news"}},
		"Tags":       []map[string]interface{}{{"Name": "go", "URL": "/tag/go"}},
	}, http.StatusOK)
	for _, want := range []string{`<time datetime="2026-11-21">`, `<p class="gp-page__summary">Short</p>`, `<img src="/uploads/a.png" alt="Hello">`,
		`<a href="/category/news">News</a>`, `<a href="/tag/go">#go</a>`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("post lacks %q", want)
		}
	}

	c, w = testContext("/blog?page=2")
	b.renderLayoutFallback(c, fallbacks.Archive, gin.H{"Title": "Blog",
		"Items":           []map[string]interface{}{{"Title": "Hello", "URL": "/blog/hello", "Excerpt": "Short", "PublishedAt": &published}},
		"Pagination":      &content.PaginatedResult{Page: 2, TotalPages: 3},
		"FallbackPrevURL": "/blog", "FallbackNextURL": "/blog?page=3",
	}, http.StatusOK)
	for _, want := range []string{`class="gp-page gp-page--archive"`, `<h1 class="gp-page__title">Blog</h1>`, `<a href="/blog/hello">Hello</a>`,
		`aria-label="Pagination"`, `<a href="/blog" rel="prev">Previous</a>`, "Page 2 of 3", `<a href="/blog?page=3" rel="next">Next</a>`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("archive lacks %q", want)
		}
	}
	c, w = testContext("/blog")
	b.renderLayoutFallback(c, fallbacks.Archive, gin.H{"Title": "Blog", "Items": []map[string]interface{}{}, "Pagination": &content.PaginatedResult{Page: 1, TotalPages: 0}}, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Nothing has been published here yet.") || strings.Contains(w.Body.String(), "gp-page__pager\"") {
		t.Errorf("empty archive: %s", w.Body.String())
	}

	c, w = testContext("/category/news")
	b.renderLayoutFallback(c, fallbacks.Taxonomy, gin.H{"TaxLabel": "Category", "TermName": "News",
		"Items": []map[string]interface{}{{"Title": "Hello", "DetailURL": "/blog/hello", "TypeLabel": "Post", "PublishedAt": &published}},
	}, http.StatusOK)
	for _, want := range []string{`class="gp-page gp-page--taxonomy"`, `<p class="gp-page__kicker">Category</p>`, `<h1 class="gp-page__title">News</h1>`,
		`<a href="/blog/hello">Hello</a>`, "Post · <time"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("taxonomy lacks %q", want)
		}
	}
}

func TestNotFoundUsesTheLayoutFallback(t *testing.T) {
	b := &BaseTheme{LayoutFallbacks: loadFallbacks(t, layoutFiles)}
	w := serve(b, "/no/such/page")
	html := w.Body.String()
	if w.Code != http.StatusNotFound || !strings.Contains(html, "<header>THEME</header>") || !strings.Contains(html, "gp-page--not-found") ||
		!strings.Contains(html, "Page not found") || !strings.Contains(html, `<a href="/">Back to the home page</a>`) {
		t.Fatalf("404 = %d %s", w.Code, html)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}

	// Without a layout fallback, still plain text.
	if w := serve(&BaseTheme{}, "/no/such/page"); w.Body.String() != "404 - Page Not Found" {
		t.Fatalf("plain 404 = %q", w.Body.String())
	}
}

func TestLayoutFallbacksNeedALayoutThatTakesOnlyContent(t *testing.T) {
	th := layoutTheme(t, map[string]string{"pages/home.tmpl": `{{define "content"}}x{{end}}`})
	if fallbacks, err := LoadLayoutFallbacks(coreFuncsTheme{th}); fallbacks != nil || err != nil {
		t.Fatalf("no layout = %v, %v; want nil, nil", fallbacks, err)
	}

	// The layout needs "sidebar", which only the theme's pages define.
	files := map[string]string{
		"layouts/base.tmpl": `{{define "base"}}{{template "content" .}}{{if .X}}{{template "sidebar" .}}{{end}}{{end}}`,
		"pages/home.tmpl":   `{{define "content"}}x{{end}}{{define "sidebar"}}s{{end}}`,
	}
	th = layoutTheme(t, files)
	if fallbacks, err := LoadLayoutFallbacks(coreFuncsTheme{th}); fallbacks != nil || err == nil || !strings.Contains(err.Error(), "sidebar") {
		t.Fatalf("layout needing a page block = %v, %v; want an error naming it", fallbacks, err)
	}
}

func TestLayoutFallbackWritesNothingWhenItFails(t *testing.T) {
	fallbacks := loadFallbacks(t, layoutFiles)
	c, w := testContext("/terms")
	// No Item: the single block cannot render.
	if (&BaseTheme{}).renderLayoutFallback(c, fallbacks.Single, gin.H{}, http.StatusOK) {
		t.Fatal("a failed render reported success")
	}
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatalf("a failed render wrote %q", w.Body.String())
	}
}

// i18nApp is an App that only provides the i18n manager.
type i18nApp struct {
	App
	mgr *coreI18n.Manager
}

func (a i18nApp) I18nManager() *coreI18n.Manager { return a.mgr }

func TestFallbackTextFollowsTheRequestLanguage(t *testing.T) {
	b := &BaseTheme{}
	c, _ := testContext("/")
	if got := b.fallbackText(c)["not_found_title"]; got != "Page not found" {
		t.Fatalf("default = %q", got)
	}
	c.Set("current_lang", "zh-CN")
	if got := b.fallbackText(c)["not_found_title"]; got != "页面不存在" {
		t.Fatalf("zh = %q", got)
	}

	// A site or theme translation of the namespaced key wins.
	mgr := coreI18n.NewManager("en")
	mgr.AddMessages("en", []*goi18n.Message{{ID: "gopress.fallback.back_home", Other: "Home sweet home"}})
	b = &BaseTheme{App: i18nApp{mgr: mgr}}
	c, _ = testContext("/")
	text := b.fallbackText(c)
	if text["back_home"] != "Home sweet home" || text["empty"] != "Nothing has been published here yet." {
		t.Fatalf("translated = %v", text)
	}

	var out bytes.Buffer
	for key := range fallbackMessages["en"] {
		if fallbackMessages["zh"][key] == "" {
			out.WriteString(key + " ")
		}
	}
	if out.Len() > 0 {
		t.Fatalf("zh lacks: %s", out.String())
	}
}
