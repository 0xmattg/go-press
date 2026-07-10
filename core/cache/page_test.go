package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreI18n "go-press/core/i18n"

	"github.com/gin-gonic/gin"
)

func TestPageCacheUsesRequestContextLanguageBeforeCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mgr := NewManager(NewMemoryCache(0), nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/es/" {
			c.Set(coreI18n.CtxKeyLang, "es")
			c.Request.URL.Path = "/"
		} else {
			c.Set(coreI18n.CtxKeyLang, "en")
		}
		c.Next()
	})
	router.Use(PageCacheMiddleware(mgr, time.Minute))
	router.NoRoute(func(c *gin.Context) {
		lang, _ := c.Get(coreI18n.CtxKeyLang)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "%s home", lang)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "en home" {
		t.Fatalf("GET / = (%d, %q), want (200, %q)", rec.Code, rec.Body.String(), "en home")
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/es/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "es home" {
		t.Fatalf("first GET /es/ without cookie = (%d, %q), want (200, %q)", rec.Code, rec.Body.String(), "es home")
	}
	if got := rec.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("first GET /es/ X-Cache = %q, want MISS", got)
	}
}
