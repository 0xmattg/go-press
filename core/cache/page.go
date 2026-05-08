package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	pageCachePrefix = "page:"
	defaultPageTTL  = 10 * time.Minute
)

// cachedResponse stores a cached HTTP response.
type cachedResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

// PageCacheMiddleware returns a Gin middleware that caches full page responses.
// Only GET requests with 200 status for anonymous users are cached.
// Skips /admin, /api, and requests with query strings by default.
func PageCacheMiddleware(mgr *Manager, ttl time.Duration) gin.HandlerFunc {
	if ttl <= 0 {
		ttl = defaultPageTTL
	}

	return func(c *gin.Context) {
		// Only cache GET requests
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		// Skip admin, API, and health paths
		if strings.HasPrefix(path, "/admin") ||
			strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/static/") ||
			path == "/health" {
			c.Next()
			return
		}

		// Skip if user is authenticated (has JWT cookie/header)
		if _, err := c.Cookie("jwt_token"); err == nil {
			c.Next()
			return
		}

		// Generate cache key from URL
		key := pageCacheKey(c.Request)

		// Try to serve from cache
		if data, ok := mgr.Get(key); ok {
			var resp cachedResponse
			if decodeCachedResponse(data, &resp) {
				c.Data(resp.StatusCode, resp.ContentType, resp.Body)
				c.Header("X-Cache", "HIT")
				c.Abort()
				return
			}
		}

		// Cache miss — capture response
		w := &responseCapture{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = w
		c.Header("X-Cache", "MISS")

		c.Next()

		// Only cache successful HTML responses
		if w.Status() == http.StatusOK {
			ct := w.Header().Get("Content-Type")
			if strings.Contains(ct, "text/html") {
				body := w.body.Bytes()
				data := encodeCachedResponse(w.Status(), ct, body)
				mgr.Set(key, data, ttl)
			}
		}
	}
}

// InvalidatePageCache removes all page cache entries.
func InvalidatePageCache(mgr *Manager) {
	mgr.DeleteByPrefix(pageCachePrefix)
}

// InvalidatePageCacheByPath removes a specific page from cache.
func InvalidatePageCacheByPath(mgr *Manager, path string) {
	// Remove with and without trailing slash
	mgr.Delete(pageCachePrefix + hashKey(path))
	if strings.HasSuffix(path, "/") {
		mgr.Delete(pageCachePrefix + hashKey(strings.TrimSuffix(path, "/")))
	} else {
		mgr.Delete(pageCachePrefix + hashKey(path+"/"))
	}
}

func pageCacheKey(r *http.Request) string {
	raw := r.URL.Path
	if r.URL.RawQuery != "" {
		raw += "?" + r.URL.RawQuery
	}
	// Include language cookie in cache key so each language gets its own cache entry
	for _, ck := range r.Cookies() {
		if ck.Name == "gopress_lang" && ck.Value != "" {
			raw += "#lang=" + ck.Value
			break
		}
	}
	return pageCachePrefix + hashKey(raw)
}

func hashKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:16]) // 128-bit, sufficient for cache keys
}

// Simple binary encoding: [4 bytes status][2 bytes ct_len][ct_bytes][body_bytes]
func encodeCachedResponse(status int, contentType string, body []byte) []byte {
	ctBytes := []byte(contentType)
	ctLen := len(ctBytes)
	buf := make([]byte, 4+2+ctLen+len(body))
	buf[0] = byte(status >> 24)
	buf[1] = byte(status >> 16)
	buf[2] = byte(status >> 8)
	buf[3] = byte(status)
	buf[4] = byte(ctLen >> 8)
	buf[5] = byte(ctLen)
	copy(buf[6:6+ctLen], ctBytes)
	copy(buf[6+ctLen:], body)
	return buf
}

func decodeCachedResponse(data []byte, resp *cachedResponse) bool {
	if len(data) < 6 {
		return false
	}
	resp.StatusCode = int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	ctLen := int(data[4])<<8 | int(data[5])
	if len(data) < 6+ctLen {
		return false
	}
	resp.ContentType = string(data[6 : 6+ctLen])
	resp.Body = data[6+ctLen:]
	return true
}

// responseCapture wraps gin.ResponseWriter to capture the response body.
type responseCapture struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseCapture) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *responseCapture) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
