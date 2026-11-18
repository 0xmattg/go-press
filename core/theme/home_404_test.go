// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// bundlePage parses a page bundle whose base template prints body.
func bundlePage(t *testing.T, body string) *template.Template {
	t.Helper()
	return template.Must(template.New("").Parse(`{{define "base"}}` + body + `{{end}}`))
}

// serve runs b.ServeHTTP for a GET of path.
func serve(b *BaseTheme, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	b.ServeHTTP(c)
	return w
}

func TestHomeUsesPageBundlesInOrder(t *testing.T) {
	b := &BaseTheme{PageTemplates: map[string]*template.Template{
		"index": bundlePage(t, "index"),
		"home":  bundlePage(t, "home {{.Title}} {{if .Ctx}}ctx{{end}}"),
	}}
	if w := serve(b, "/"); w.Code != http.StatusOK || w.Body.String() != "home Home ctx" {
		t.Fatalf("home = %d %q", w.Code, w.Body.String())
	}
	b.PageTemplates["front-page"] = bundlePage(t, "front")
	if w := serve(b, "/"); w.Body.String() != "front" {
		t.Fatalf("front-page should win, got %q", w.Body.String())
	}
	delete(b.PageTemplates, "front-page")
	delete(b.PageTemplates, "home")
	if w := serve(b, "/"); w.Body.String() != "index" {
		t.Fatalf("index fallback, got %q", w.Body.String())
	}
}

func TestHomeFallsBackToClassicTemplates(t *testing.T) {
	classic := template.Must(template.New("").Parse(`{{define "home.tmpl"}}{{end}}{{define "base"}}classic{{end}}`))
	b := &BaseTheme{Templates: &TemplateEngine{templates: classic}}
	if w := serve(b, "/"); w.Code != http.StatusOK || w.Body.String() != "classic" {
		t.Fatalf("classic home = %d %q", w.Code, w.Body.String())
	}
}

func TestHomeWithoutTemplatesDoesNotPanic(t *testing.T) {
	b := &BaseTheme{}
	if w := serve(b, "/"); w.Code != http.StatusInternalServerError {
		t.Fatalf("home without templates = %d %q", w.Code, w.Body.String())
	}
}

func TestNotFoundUsesPageBundle(t *testing.T) {
	b := &BaseTheme{PageTemplates: map[string]*template.Template{
		NotFoundPageName: bundlePage(t, "missing: {{.Title}} {{if .Ctx}}ctx{{end}}"),
	}}
	w := serve(b, "/no/such/page")
	if w.Code != http.StatusNotFound || w.Body.String() != "missing: Page Not Found ctx" {
		t.Fatalf("404 = %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
}

func TestNotFoundWithoutTemplatesIsPlainText(t *testing.T) {
	b := &BaseTheme{}
	w := serve(b, "/no/such/page")
	if w.Code != http.StatusNotFound || w.Body.String() != "404 - Page Not Found" {
		t.Fatalf("404 = %d %q", w.Code, w.Body.String())
	}
}
