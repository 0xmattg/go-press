package admin

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/user"
)

func TestEnsurePublishedAtForPublishedSetsMissingTime(t *testing.T) {
	item := &content.Content{Status: content.StatusPublished}

	ensurePublishedAtForPublished(item)

	if item.PublishedAt == nil {
		t.Fatal("PublishedAt should be set for published content")
	}
}

func TestEnsurePublishedAtForPublishedPreservesExistingTime(t *testing.T) {
	item := &content.Content{Status: content.StatusPublished}
	ensurePublishedAtForPublished(item)
	first := item.PublishedAt

	ensurePublishedAtForPublished(item)

	if item.PublishedAt != first {
		t.Fatal("PublishedAt should not be replaced when already set")
	}
}

func TestEnsurePublishedAtForPublishedLeavesDraftUnset(t *testing.T) {
	item := &content.Content{Status: content.StatusDraft}

	ensurePublishedAtForPublished(item)

	if item.PublishedAt != nil {
		t.Fatal("PublishedAt should remain nil for draft content")
	}
}

func TestAdminDateTimeInputRoundTripsInLocalTime(t *testing.T) {
	svc := &Service{siteTimezone: "Asia/Shanghai"}

	parsed, err := svc.ParseAdminDateTimeInput("2026-05-25T07:56")
	if err != nil {
		t.Fatalf("parse admin datetime input: %v", err)
	}

	if got := parsed.Location(); got != time.UTC {
		t.Fatalf("expected UTC storage location, got %v", got)
	}
	if got := parsed.Format("2006-01-02 15:04"); got != "2026-05-24 23:56" {
		t.Fatalf("unexpected UTC instant: %s", got)
	}
	if got := svc.FormatAdminDateTimeInput(parsed); got != "2026-05-25T07:56" {
		t.Fatalf("datetime-local value should round trip, got %q", got)
	}
	if got := svc.FormatAdminDateTime(parsed); got != "2026-05-25 07:56" {
		t.Fatalf("admin display time should use local time, got %q", got)
	}
}

func TestContentFormHookReceivesStableCoreArgs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, ctx := gin.CreateTestContext(httptest.NewRecorder())

	h := NewHandler(
		&Service{rbac: user.NewRBAC()},
		content.NewRegistry(),
		filepath.Join("templates"),
	)

	var captured []interface{}
	bus := hook.New()
	bus.AddFilter(hook.AdminContentFormFields, func(value interface{}, args ...interface{}) interface{} {
		captured = append([]interface{}{}, args...)
		return template.HTML(`<div id="hook-output"></div>`)
	}, 10)
	h.SetHookBus(bus)

	tmpl, err := template.New("content_form_test").Funcs(h.funcMap).ParseFiles(filepath.Join("templates", "pages", "content_form.tmpl"))
	if err != nil {
		t.Fatalf("parse content form template: %v", err)
	}

	typeDef := &content.ContentTypeDef{
		Name:     "post",
		Label:    "Post",
		Supports: []string{"title"},
		Rewrite:  content.RewriteRule{Slug: "blog"},
	}
	item := &content.Content{ID: 42, Type: "post", Title: "SEO Post", Slug: "seo-post"}
	view := DynamicContentView{ID: item.ID, Title: item.Title, Slug: item.Slug}
	data := gin.H{
		"Ctx":           ctx,
		"Title":         "Edit Post",
		"TypeDef":       typeDef,
		"TypeName":      "post",
		"Slug":          "posts",
		"Item":          view,
		"HookItem":      item,
		"BackURL":       "/admin/posts",
		"BackQuery":     "",
		"AdminLanguage": defaultAdminLanguage,
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatalf("execute content form template: %v", err)
	}
	if !strings.Contains(out.String(), `id="hook-output"`) {
		t.Fatal("expected hook output to render")
	}
	if len(captured) != 3 || captured[0] != ctx || captured[1] != item || captured[2] != typeDef {
		t.Fatalf("unexpected hook args: %#v", captured)
	}
}
