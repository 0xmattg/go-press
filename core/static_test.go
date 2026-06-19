package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/config"
)

func TestServeStaticHeadUploadReturnsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uploadDir := t.TempDir()
	filePath := filepath.Join(uploadDir, "2026", "05", "icon.png")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{Config: &config.Config{CMS: config.CMSConfig{UploadDir: uploadDir}}}
	router := gin.New()
	router.HEAD("/static/*filepath", engine.serveStatic)

	req := httptest.NewRequest(http.MethodHead, "/static/uploads/2026/05/icon.png", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q, want immutable upload cache", cc)
	}
}

func TestServeStaticMissingDoesNotUseImmutableCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := &Engine{Config: &config.Config{CMS: config.CMSConfig{UploadDir: t.TempDir()}}}
	router := gin.New()
	router.HEAD("/static/*filepath", engine.serveStatic)

	req := httptest.NewRequest(http.MethodHead, "/static/uploads/missing.png", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
}
