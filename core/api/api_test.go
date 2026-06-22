package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
)

func TestListRejectsNonPublishedStatusBeforeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post", HasArchive: true})
	h := NewHandler(registry, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/content?type=post&status=all", nil)

	h.List(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListRejectsInternalContentTypeBeforeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "contact_message", HasArchive: false})
	h := NewHandler(registry, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/content?type=contact_message", nil)

	h.List(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCanExposeOnlyPublishedPublicContent(t *testing.T) {
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post", HasArchive: true})
	registry.RegisterType(content.ContentTypeDef{Name: "contact_message", HasArchive: false})
	h := NewHandler(registry, nil)

	now := time.Now()
	future := now.Add(time.Hour)
	tests := []struct {
		name string
		item *content.Content
		want bool
	}{
		{"published", &content.Content{Type: "post", Status: content.StatusPublished, PublishedAt: &now}, true},
		{"draft", &content.Content{Type: "post", Status: content.StatusDraft}, false},
		{"scheduled", &content.Content{Type: "post", Status: content.StatusPublished, PublishedAt: &future}, false},
		{"internal", &content.Content{Type: "contact_message", Status: content.StatusPublished}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.canExpose(tt.item, ""); got != tt.want {
				t.Fatalf("canExpose() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterRoutesSkipsInternalContentTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name:       "post",
		HasArchive: true,
		Rewrite:    content.RewriteRule{Slug: "blog"},
	})
	registry.RegisterType(content.ContentTypeDef{
		Name:       "contact_message",
		HasArchive: false,
		Rewrite:    content.RewriteRule{Slug: "messages"},
	})

	r := gin.New()
	h := NewHandler(registry, nil)
	h.RegisterRoutes(r.Group("/api/v1"))

	for _, route := range r.Routes() {
		if strings.Contains(route.Path, "/messages") {
			t.Fatalf("internal content route was registered: %s", route.Path)
		}
	}
}

func TestToDTOSanitizesHistoricalContent(t *testing.T) {
	dto := toDTO(&content.Content{
		Content: `<p>Safe</p><script>alert(1)</script>`,
	})

	if strings.Contains(strings.ToLower(dto.Content), "<script") {
		t.Fatalf("DTO contains executable markup: %s", dto.Content)
	}
	if !strings.Contains(dto.Content, "<p>Safe</p>") {
		t.Fatalf("DTO lost safe markup: %s", dto.Content)
	}
}
