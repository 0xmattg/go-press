package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-press/core/content"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

// TestPageCRUDPermission verifies the standalone page type reuses the core
// content RBAC caps: a permissionless role is rejected for every mutating
// action, while roles that hold content caps are allowed. The page type is
// registered so mapResource collapses "page" to the "content" resource, exactly
// as the real admin does.
func TestPageCRUDPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := content.NewRegistry()
	reg.RegisterType(content.ContentTypeDef{
		Name:         "page",
		Hierarchical: true,
		Rewrite:      content.RewriteRule{Rootless: true},
	})
	handler := &Handler{svc: &Service{rbac: user.NewRBAC()}, registry: reg}

	cases := []struct {
		role string
		want int
	}{
		{role: user.RoleSubscriber, want: http.StatusFound},     // no content caps -> redirect
		{role: user.RoleEditor, want: http.StatusNoContent},     // full content caps -> allowed
		{role: user.RoleSuperAdmin, want: http.StatusNoContent}, // wildcard -> allowed
	}

	for _, action := range []string{"create", "update", "delete"} {
		for _, tc := range cases {
			name := action + "/" + tc.role
			t.Run(name, func(t *testing.T) {
				router := gin.New()
				router.POST("/admin/pages", func(c *gin.Context) {
					c.Set("admin_role", tc.role)
					if !handler.checkPermission(c, "page", action) {
						return
					}
					c.Status(http.StatusNoContent)
				})
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/pages", nil))
				if rec.Code != tc.want {
					t.Fatalf("%s: status=%d, want %d", name, rec.Code, tc.want)
				}
			})
		}
	}
}

// TestIsReservedPageSlug guards the front-end reachability check: page slugs
// that collide with a system route or an archive/taxonomy prefix are rejected,
// ordinary slugs are allowed.
func TestIsReservedPageSlug(t *testing.T) {
	reg := content.NewRegistry()
	reg.RegisterType(content.ContentTypeDef{Name: "post", HasArchive: true, Rewrite: content.RewriteRule{Slug: "blog"}})
	reg.RegisterType(content.ContentTypeDef{Name: "page", Rewrite: content.RewriteRule{Rootless: true}})
	reg.RegisterTaxonomy(content.TaxonomyDef{Name: "category"})
	h := &Handler{registry: reg}

	reserved := []string{"admin", "api", "blog", "category", "sitemap.xml", "Admin", "/blog/"}
	for _, s := range reserved {
		if !h.isReservedPageSlug(s) {
			t.Errorf("isReservedPageSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"about", "privacy", "terms", "our-team", ""} {
		if h.isReservedPageSlug(s) {
			t.Errorf("isReservedPageSlug(%q) = true, want false", s)
		}
	}
}
