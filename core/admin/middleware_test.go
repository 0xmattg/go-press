package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
)

func TestRequirePermissionRejectsInsufficientRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "reader", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	r := gin.New()
	r.POST("/admin/extension", RequirePermission(auth, user.NewRBAC(), "plugin", "update"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/extension", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequirePermissionAllowsMatchingRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	r := gin.New()
	r.POST("/admin/extension", RequirePermission(auth, user.NewRBAC(), "content", "create"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/extension", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
