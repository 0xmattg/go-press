package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"go-press/core/comment"
	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type fakeAdminComments struct {
	moderated bool
	page      *comment.Page
}

func (f *fakeAdminComments) AdminList(string, int, int) (*comment.Page, error) {
	if f.page != nil {
		return f.page, nil
	}
	return &comment.Page{}, nil
}

func (f *fakeAdminComments) Moderate(_ context.Context, id uint, status string) (*comment.Comment, error) {
	f.moderated = true
	return &comment.Comment{ID: id, Status: status}, nil
}

func (f *fakeAdminComments) DeleteByContentIDs(*gorm.DB, []uint) error { return nil }

func TestCommentModerationPermissionRejectsSubscriberAndAllowsEditor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name       string
		role       string
		wantCalled bool
	}{
		{name: "subscriber rejected", role: user.RoleSubscriber, wantCalled: false},
		{name: "editor allowed", role: user.RoleEditor, wantCalled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comments := &fakeAdminComments{}
			handler := &Handler{svc: &Service{rbac: user.NewRBAC(), comments: comments}}
			router := gin.New()
			router.POST("/admin/comments/:id/status", func(c *gin.Context) {
				c.Set("admin_role", tc.role)
				handler.CommentStatusUpdate(c)
			})
			form := url.Values{"status": {comment.StatusApproved}}
			req := httptest.NewRequest(http.MethodPost, "/admin/comments/7/status", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
			}
			if comments.moderated != tc.wantCalled {
				t.Fatalf("moderated = %v, want %v", comments.moderated, tc.wantCalled)
			}
		})
	}
}

func TestLoadTemplatesRegistersEveryAdminPage(t *testing.T) {
	handler := NewHandler(
		&Service{rbac: user.NewRBAC()},
		content.NewRegistry(),
		filepath.Join("templates"),
	)
	paths, err := filepath.Glob(filepath.Join("templates", "pages", "*.tmpl"))
	if err != nil {
		t.Fatalf("glob admin templates: %v", err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if name == "login" {
			continue
		}
		if handler.templates[name] == nil {
			t.Errorf("admin template %q is not registered", name)
		}
	}
}

func TestAdminCommentRowsDistinguishContentAndReplies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name: "post", Rewrite: content.RewriteRule{Slug: "blog"},
	})
	bus := hook.New()
	bus.AddFilter(HookContentPermalinkPrefix, func(_ interface{}, _ ...interface{}) interface{} {
		return "/es"
	}, 10)
	handler := &Handler{registry: registry, hooks: bus}
	target := content.Content{ID: 3, Type: "post", Slug: "field-guide", Title: "A Field Guide"}
	parentID := uint(9)
	rows := handler.adminCommentRows(ctx, []comment.Comment{
		{ID: 10, Target: target},
		{ID: 11, ParentID: &parentID, Parent: &comment.Comment{ID: parentID, Body: "test\ncomment"}, Target: target},
	})

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].IsReply || rows[0].ContextText != target.Title || rows[0].ContextURL != "/es/blog/field-guide" {
		t.Fatalf("unexpected top-level row: %+v", rows[0])
	}
	if !rows[1].IsReply || rows[1].ContextText != "test comment" || rows[1].ContextURL != "/es/blog/field-guide#comment-9" {
		t.Fatalf("unexpected reply row: %+v", rows[1])
	}
	if rows[1].TargetURL != "/es/blog/field-guide" {
		t.Fatalf("reply target URL = %q", rows[1].TargetURL)
	}
}

func TestCommentListRendersLinkedContentAndReplyContexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name: "post", Rewrite: content.RewriteRule{Slug: "blog"},
	})
	parentID := uint(9)
	target := content.Content{ID: 3, Type: "post", Slug: "field-guide", Title: "A Field Guide"}
	comments := &fakeAdminComments{page: &comment.Page{
		Items: []comment.Comment{
			{ID: 10, Body: "top level", Target: target},
			{ID: 11, Body: "reply", ParentID: &parentID, Parent: &comment.Comment{ID: parentID, Body: "test comment"}, Target: target},
		},
		Total: 2, Page: 1, PerPage: 20, TotalPages: 1,
	}}
	handler := NewHandler(
		&Service{rbac: user.NewRBAC(), comments: comments},
		registry,
		filepath.Join("templates"),
	)
	router := gin.New()
	router.GET("/admin/comments", func(c *gin.Context) {
		c.Set("admin_role", user.RoleEditor)
		handler.CommentList(c)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/comments", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`href="/blog/field-guide"`,
		`href="/blog/field-guide#comment-9"`,
		`class="admin-pagination"`,
		`name="page"`,
		`A Field Guide`,
		`test comment`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered comments missing %q", want)
		}
	}
}

func TestBuildCommentPaginationPreservesStatusAndBounds(t *testing.T) {
	result := &comment.Page{
		Items: make([]comment.Comment, 20),
		Total: 45, Page: 2, PerPage: 20, TotalPages: 3,
	}
	view := buildCommentPagination(result, comment.StatusApproved)
	if view.Page != 2 || view.TotalPages != 3 || view.From != 21 || view.To != 40 {
		t.Fatalf("unexpected pagination values: %+v", view)
	}
	if view.FirstURL != "/admin/comments?page=1&status=approved" ||
		view.PrevURL != "/admin/comments?page=1&status=approved" ||
		view.NextURL != "/admin/comments?page=3&status=approved" ||
		view.LastURL != "/admin/comments?page=3&status=approved" {
		t.Fatalf("pagination did not preserve status: %+v", view)
	}
}
