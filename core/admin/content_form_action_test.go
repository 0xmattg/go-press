package admin

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// A create that fails is shown again with the submitted values. Its form must
// still post to /new: the view carries ID 0 because nothing was saved, and
// posting to /0/edit would lose the editor's input on the next attempt.
func TestContentFormPostsToEditOnlyForSavedItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, ctx := gin.CreateTestContext(httptest.NewRecorder())
	h := NewHandler(&Service{rbac: user.NewRBAC()}, content.NewRegistry(), filepath.Join("templates"))
	tmpl, err := template.New("content_form_action").Funcs(h.funcMap).ParseFiles(filepath.Join("templates", "pages", "content_form.tmpl"))
	if err != nil {
		t.Fatalf("parse content form template: %v", err)
	}
	typeDef := &content.ContentTypeDef{Name: "post", Label: "Post", Supports: []string{"title"}, Rewrite: content.RewriteRule{Slug: "blog"}}

	render := func(item interface{}) string {
		t.Helper()
		data := gin.H{
			"Ctx": ctx, "Title": "Post", "TypeDef": typeDef, "TypeName": "post", "Slug": "posts",
			"Item": item, "HookItem": &content.Content{Type: "post"}, "BackURL": "/admin/posts",
			"AdminLanguage": defaultAdminLanguage,
		}
		var out bytes.Buffer
		if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
			t.Fatalf("execute content form template: %v", err)
		}
		return out.String()
	}

	tests := []struct {
		name   string
		item   interface{}
		action string
	}{
		{"new form", nil, `action="/admin/posts/new"`},
		{"failed create re-rendered", DynamicContentView{Title: "Unsaved", Slug: "unsaved"}, `action="/admin/posts/new"`},
		{"existing item", DynamicContentView{ID: 42, Title: "Saved", Slug: "saved"}, `action="/admin/posts/42/edit"`},
	}
	for _, tt := range tests {
		if rendered := render(tt.item); !strings.Contains(rendered, tt.action) {
			t.Errorf("%s: form action missing %s", tt.name, tt.action)
		}
	}
}
