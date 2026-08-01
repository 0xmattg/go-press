package core

import (
	"html/template"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	coreTheme "go-press/core/theme"
)

type shellTestTheme struct {
	templateDir string
}

func (t shellTestTheme) Name() string                    { return "shell-test" }
func (t shellTestTheme) Version() string                 { return "1.0.0" }
func (t shellTestTheme) Description() string             { return "" }
func (t shellTestTheme) Author() string                  { return "" }
func (t shellTestTheme) Setup(coreTheme.App)             {}
func (t shellTestTheme) ServeHTTP(*gin.Context)          {}
func (t shellTestTheme) TemplateFuncs() template.FuncMap { return template.FuncMap{} }
func (t shellTestTheme) TemplateDir() string             { return t.templateDir }
func (t shellTestTheme) StaticDir() string               { return "" }

func TestRenderNamespacedInActiveThemePrefersThemeOverride(t *testing.T) {
	themeDir := t.TempDir()
	defaultDir := filepath.Join(t.TempDir(), "shop-module")
	writeShellTestFile(t, filepath.Join(themeDir, "layouts", "base.tmpl"),
		`{{define "base"}}layout:{{template "content" .}}{{end}}`)
	writeShellTestFile(t, filepath.Join(themeDir, "shop-module", "basket.tmpl"),
		`{{define "content"}}theme-override:{{.Marker}}{{end}}`)
	writeShellTestFile(t, filepath.Join(defaultDir, "basket.tmpl"),
		`{{define "content"}}extension-default{{end}}`)

	engine := newShellTestEngine(themeDir)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/basket", nil)

	err := engine.RenderNamespacedInActiveTheme(c, "shop-module", "basket", defaultDir, gin.H{"Marker": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != "layout:theme-override:ok" {
		t.Fatalf("rendered body = %q", got)
	}
}

func TestRenderInActiveThemeDerivesNamespaceWithoutBusinessKnowledge(t *testing.T) {
	themeDir := t.TempDir()
	defaultDir := filepath.Join(t.TempDir(), "event-booking")
	writeShellTestFile(t, filepath.Join(themeDir, "layouts", "base.tmpl"),
		`{{define "base"}}{{template "content" .}}{{end}}`)
	writeShellTestFile(t, filepath.Join(themeDir, "event-booking", "schedule.tmpl"),
		`{{define "content"}}theme schedule{{end}}`)
	writeShellTestFile(t, filepath.Join(defaultDir, "schedule.tmpl"),
		`{{define "content"}}default schedule{{end}}`)

	engine := newShellTestEngine(themeDir)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/schedule", nil)

	if err := engine.RenderInActiveTheme(c, "schedule", defaultDir, nil); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != "theme schedule" {
		t.Fatalf("rendered body = %q", got)
	}
}

func TestRenderNamespacedInActiveThemeFallsBackToExtensionTemplate(t *testing.T) {
	themeDir := t.TempDir()
	defaultDir := filepath.Join(t.TempDir(), "booking")
	writeShellTestFile(t, filepath.Join(themeDir, "layouts", "base.tmpl"),
		`{{define "base"}}{{template "content" .}}{{end}}`)
	writeShellTestFile(t, filepath.Join(defaultDir, "calendar.tmpl"),
		`{{define "content"}}extension calendar{{end}}`)

	engine := newShellTestEngine(themeDir)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/calendar", nil)

	if err := engine.RenderNamespacedInActiveTheme(c, "booking", "calendar", defaultDir, nil); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != "extension calendar" {
		t.Fatalf("rendered body = %q", got)
	}
}

func TestRenderNamespacedInActiveThemeRejectsUnsafePathComponents(t *testing.T) {
	engine := newShellTestEngine(t.TempDir())
	for _, tt := range []struct {
		name      string
		namespace string
		fragment  string
	}{
		{name: "namespace traversal", namespace: "../private", fragment: "page"},
		{name: "namespace separator", namespace: "module/pages", fragment: "page"},
		{name: "fragment traversal", namespace: "module", fragment: "../secret"},
		{name: "fragment extension", namespace: "module", fragment: "page.tmpl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/", nil)
			err := engine.RenderNamespacedInActiveTheme(c, tt.namespace, tt.fragment, t.TempDir(), nil)
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("error = %v, want invalid path component", err)
			}
		})
	}
}

func newShellTestEngine(templateDir string) *Engine {
	const slug = "shell-test"
	return &Engine{
		themes:          map[string]coreTheme.Theme{slug: shellTestTheme{templateDir: templateDir}},
		activeThemeName: slug,
	}
}

func writeShellTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
