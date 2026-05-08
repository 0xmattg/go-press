package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

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
		if len(c.Request.URL.Path) > 7 && c.Request.URL.Path[:8] == "/static/" {
			isUpload := len(c.Request.URL.Path) > 16 && c.Request.URL.Path[:16] == "/static/uploads/"
			if isUpload || c.Query("v") != "" {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
				c.Header("Expires", time.Now().Add(365*24*time.Hour).Format(time.RFC1123))
			} else {
				c.Header("Cache-Control", "public, max-age=86400")
				c.Header("Expires", time.Now().Add(24*time.Hour).Format(time.RFC1123))
			}
		}
		c.Next()
	}
}
