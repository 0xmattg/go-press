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

func TestPageCacheSkipsPublicAuthenticatedCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := NewManager(NewMemoryCache(0), nil)
	router := gin.New()
	router.Use(PageCacheMiddleware(mgr, time.Minute))
	renders := 0
	router.GET("/account", func(c *gin.Context) {
		renders++
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "render %d", renders)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/account", nil))
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/account", nil))
	if renders != 1 {
		t.Fatalf("anonymous renders = %d, want 1", renders)
	}

	recorder = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(&http.Cookie{Name: "gopress_user_session", Value: "session-token"})
	router.ServeHTTP(recorder, req)
	if renders != 2 || recorder.Header().Get("X-Cache") == "HIT" {
		t.Fatalf("authenticated request used anonymous cache: renders=%d X-Cache=%q", renders, recorder.Header().Get("X-Cache"))
	}
}

func TestPageCacheHonorsRequestBypassBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := NewManager(NewMemoryCache(0), nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.Request.Header.Get("X-Private") == "1" {
			BypassPageCache(c)
		}
		c.Next()
	})
	router.Use(PageCacheMiddleware(mgr, time.Minute))
	renders := 0
	router.GET("/store", func(c *gin.Context) {
		renders++
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "render %d", renders)
	})

	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/store", nil))
	private := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/store", nil)
	req.Header.Set("X-Private", "1")
	router.ServeHTTP(private, req)
	if renders != 2 || private.Header().Get("X-Cache") != "BYPASS" || private.Body.String() != "render 2" {
		t.Fatalf("private request reused public cache: renders=%d X-Cache=%q body=%q", renders, private.Header().Get("X-Cache"), private.Body.String())
	}
}

func TestPageCacheDoesNotStorePrivateResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cacheControl := range []string{"private", "no-store", "no-cache"} {
		t.Run(cacheControl, func(t *testing.T) {
			mgr := NewManager(NewMemoryCache(0), nil)
			router := gin.New()
			router.Use(PageCacheMiddleware(mgr, time.Minute))
			renders := 0
			router.GET("/private", func(c *gin.Context) {
				renders++
				c.Header("Cache-Control", cacheControl)
				c.Header("Content-Type", "text/html; charset=utf-8")
				c.String(http.StatusOK, "render %d", renders)
			})

			for range 2 {
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/private", nil))
			}
			if renders != 2 {
				t.Fatalf("Cache-Control %q response was stored", cacheControl)
			}
		})
	}
}

func TestPageCacheBypassesQueryStrings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := NewManager(NewMemoryCache(0), nil)
	router := gin.New()
	router.Use(PageCacheMiddleware(mgr, time.Minute))
	renders := 0
	router.GET("/complete", func(c *gin.Context) {
		renders++
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "render %d", renders)
	})

	for range 2 {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/complete?key=secret", nil))
		if recorder.Header().Get("X-Cache") != "BYPASS" {
			t.Fatalf("query response X-Cache = %q", recorder.Header().Get("X-Cache"))
		}
	}
	if renders != 2 {
		t.Fatalf("query response was cached: renders=%d", renders)
	}
}
