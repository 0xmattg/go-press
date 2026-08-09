package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestMediaPaginationRendersAfterGrid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "pages", "media.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	gridIndex := strings.Index(source, `<div class="media-grid">`)
	paginationIndex := strings.Index(source, `<div class="media-list-toolbar">`)
	if gridIndex < 0 || paginationIndex < 0 || paginationIndex < gridIndex {
		t.Fatalf("media pagination must render after the media grid: grid=%d pagination=%d", gridIndex, paginationIndex)
	}
}

func TestBuildMediaPaginationPreservesQueryAndBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/media?kind=image&page=2&success=uploaded", nil)

	view := buildMediaPagination(c, 45, 2, 20, 20)
	if view.Page != 2 || view.PerPage != 20 || view.TotalPages != 3 || view.From != 21 || view.To != 40 {
		t.Fatalf("unexpected media pagination: %+v", view)
	}
	if view.FirstURL != "/admin/media?kind=image&page=1" ||
		view.PrevURL != "/admin/media?kind=image&page=1" ||
		view.NextURL != "/admin/media?kind=image&page=3" ||
		view.LastURL != "/admin/media?kind=image&page=3" {
		t.Fatalf("media pagination URLs did not preserve filters or strip flash state: %+v", view)
	}

	empty := buildMediaPagination(c, 0, 99, 20, 0)
	if empty.Page != 1 || empty.TotalPages != 1 || empty.From != 0 || empty.To != 0 {
		t.Fatalf("empty media pagination was not normalized: %+v", empty)
	}
}

func TestMediaReadHandlersRejectRoleWithoutPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		route string
		path  string
		call  func(*Handler, *gin.Context)
	}{
		{name: "page", route: "/admin/media", path: "/admin/media?page=2", call: func(h *Handler, c *gin.Context) { h.MediaList(c) }},
		{name: "picker JSON", route: "/admin/media/json", path: "/admin/media/json?page=2", call: func(h *Handler, c *gin.Context) { h.MediaJSON(c) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &Handler{svc: &Service{rbac: user.NewRBAC()}}
			router := gin.New()
			router.GET(tc.route, func(c *gin.Context) {
				c.Set("admin_role", user.RoleSubscriber)
				tc.call(handler, c)
			})

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != http.StatusFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
			}
			if location := recorder.Header().Get("Location"); location == "" {
				t.Fatal("permission denial must redirect without querying the media repository")
			}
		})
	}
}

func TestMediaTotalPages(t *testing.T) {
	for _, tc := range []struct {
		total   int64
		perPage int
		want    int
	}{
		{total: 0, perPage: 20, want: 0},
		{total: 1, perPage: 20, want: 1},
		{total: 20, perPage: 20, want: 1},
		{total: 21, perPage: 20, want: 2},
		{total: 45, perPage: 20, want: 3},
		{total: 45, perPage: 0, want: 0},
	} {
		if got := mediaTotalPages(tc.total, tc.perPage); got != tc.want {
			t.Errorf("mediaTotalPages(%d, %d) = %d, want %d", tc.total, tc.perPage, got, tc.want)
		}
	}
}
