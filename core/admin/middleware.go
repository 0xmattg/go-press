package admin

import (
	"net/http"

	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates the admin JWT token from the cookie.
func AuthMiddleware(auth *user.Auth) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		token, err := c.Cookie("admin_token")
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		claims, err := auth.ParseToken(token)
		if err != nil {
			c.SetCookie("admin_token", "", -1, "/admin", "", false, true)
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		c.Set("admin_user_id", claims.UserID)
		c.Set("admin_username", claims.Username)
		c.Set("admin_role", claims.Role)
		c.Next()
	}
}

// RequirePermission protects extension-owned admin routes with the same JWT
// and RBAC rules used by core admin handlers.
func RequirePermission(auth *user.Auth, rbac *user.RBAC, resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		token, err := c.Cookie("admin_token")
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		claims, err := auth.ParseToken(token)
		if err != nil {
			c.SetCookie("admin_token", "", -1, "/admin", "", false, true)
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		if rbac == nil || !rbac.Can(claims.Role, resource, action) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Set("admin_user_id", claims.UserID)
		c.Set("admin_username", claims.Username)
		c.Set("admin_role", claims.Role)
		c.Next()
	}
}
