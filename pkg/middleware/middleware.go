package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// IsSameOrigin reports whether a state-changing request originates from the
// same host as the request target, based on the Origin (preferred) or Referer
// header. Requests that carry neither header are treated as same-origin so that
// non-browser clients and server-to-server calls keep working; browsers always
// send at least one of these headers on cross-site POST/PUT/DELETE, so a forged
// cross-site request is reliably rejected. This is a defense-in-depth CSRF
// control that complements SameSite cookies without requiring per-form tokens.
func IsSameOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return false
		}
		return strings.EqualFold(parsed.Host, r.Host)
	}
	return true
}

// IsStateChangingMethod reports whether the HTTP method mutates state and should
// therefore be subject to CSRF/same-origin enforcement.
func IsStateChangingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func Logger() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health", "/static"},
	})
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

func CacheControl() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
