package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/user"
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

// A permitted role must still be rejected when the state-changing request comes
// from a different origin (CSRF guard), even with a valid session cookie.
func TestRequirePermissionRejectsCrossOriginPost(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/extension", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// The same guard applies to the primary AuthMiddleware.
func TestAuthMiddlewareRejectsCrossOriginPost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	r := gin.New()
	r.POST("/admin/x", AuthMiddleware(auth), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/x", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAuthMiddlewareRejectsExistingTokenAfterAccountDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := adminUserTestRepository(t)
	auth := user.NewAuth("test-secret", 1, repository)
	account, err := repository.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	account.Role = user.RoleSuperAdmin
	if err := repository.Update(account); err != nil {
		t.Fatal(err)
	}
	token, err := auth.GenerateToken(account)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	account.IsActive = false
	if err := repository.Update(account); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/admin/x", AuthMiddleware(auth), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: adminCookieName, Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "/admin/login" {
		t.Fatalf("redirect = %q, want /admin/login", location)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == adminCookieName && cookie.MaxAge < 0 && cookie.Value == "" {
			return
		}
	}
	t.Fatal("disabled account response did not clear the admin token cookie")
}
