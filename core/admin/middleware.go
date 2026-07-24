package admin

import (
	"net/http"

	"go-press/core/user"
	"go-press/pkg/middleware"

	"github.com/gin-gonic/gin"
)

const (
	adminCookieName   = "admin_token"
	adminCookiePath   = "/admin"
	adminCookieMaxAge = 86400
)

// writeAdminCookie stores the admin session token with hardened attributes:
// HttpOnly (no JS access), SameSite=Lax (CSRF mitigation), and Secure derived
// from the site scheme so HTTPS deployments never leak the token over plaintext.
func writeAdminCookie(c *gin.Context, token string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminCookieName, token, adminCookieMaxAge, adminCookiePath, "", secure, true)
}

// clearAdminCookie expires the admin session cookie using the same attributes
// it was written with, so browsers reliably drop it.
func clearAdminCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminCookieName, "", -1, adminCookiePath, "", secure, true)
}

// rejectCrossOrigin enforces the same-origin CSRF guard for state-changing
// admin requests. It returns true (and aborts) when the request must be blocked.
func rejectCrossOrigin(c *gin.Context) bool {
	if middleware.IsStateChangingMethod(c.Request.Method) && !middleware.IsSameOrigin(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return true
	}
	return false
}

// AuthMiddleware validates the admin JWT token from the cookie.
func AuthMiddleware(auth *user.Auth) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if rejectCrossOrigin(c) {
			return
		}
		secure := auth.SecureCookies()
		token, err := c.Cookie(adminCookieName)
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		claims, err := auth.ActiveClaims(token)
		if err != nil {
			clearAdminCookie(c, secure)
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
		if rejectCrossOrigin(c) {
			return
		}
		secure := auth.SecureCookies()
		token, err := c.Cookie(adminCookieName)
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}
		claims, err := auth.ActiveClaims(token)
		if err != nil {
			clearAdminCookie(c, secure)
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
