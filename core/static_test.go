// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/frontend"
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

func TestServeStaticSvgUploadIsSandboxedAndDownloaded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uploadDir := t.TempDir()
	filePath := filepath.Join(uploadDir, "2026", "05", "logo.svg")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	if err := os.WriteFile(filePath, []byte(svg), 0644); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{Config: &config.Config{CMS: config.CMSConfig{UploadDir: uploadDir}}}
	router := gin.New()
	router.GET("/static/*filepath", engine.serveStatic)

	req := httptest.NewRequest(http.MethodGet, "/static/uploads/2026/05/logo.svg", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "attachment" {
		t.Fatalf("content-disposition = %q, want attachment", cd)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("csp = %q, want sandbox directive", csp)
	}
	if xcto := rec.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Fatalf("x-content-type-options = %q, want nosniff", xcto)
	}
}

func TestServeStaticImageUploadStaysInline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uploadDir := t.TempDir()
	filePath := filepath.Join(uploadDir, "2026", "05", "photo.png")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{Config: &config.Config{CMS: config.CMSConfig{UploadDir: uploadDir}}}
	router := gin.New()
	router.GET("/static/*filepath", engine.serveStatic)

	req := httptest.NewRequest(http.MethodGet, "/static/uploads/2026/05/photo.png", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "" {
		t.Fatalf("content-disposition = %q, want empty for images", cd)
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

func TestServeStaticFrontendScript(t *testing.T) {
	want, err := fs.ReadFile(frontend.FS(), frontend.ScriptName)
	if err != nil {
		t.Fatal(err)
	}
	// The active theme ships files in static/gopress/; core's directory
	// still wins, and its missing names do not fall back to the theme.
	files := assetTestFiles()
	files["static/gopress/ui.js"] = &fstest.MapFile{Data: []byte("theme copy")}
	files["static/gopress/extra.js"] = &fstest.MapFile{Data: []byte("theme extra")}
	e := newAssetTestEngine(newAssetTestTheme(files), "release")

	t.Run("versioned", func(t *testing.T) {
		rec := serveStaticForTest(e, frontend.ScriptURL(), nil)
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
			t.Fatalf("status = %d, body starts %q", rec.Code, firstLine(rec.Body.String()))
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
			t.Fatalf("content-type = %q", ct)
		}
		if xcto := rec.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
			t.Fatalf("x-content-type-options = %q", xcto)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Fatalf("cache-control = %q", cc)
		}
		if rec.Header().Get("ETag") == "" {
			t.Fatal("no ETag")
		}
	})
	t.Run("unversioned", func(t *testing.T) {
		rec := serveStaticForTest(e, "/static/gopress/ui.js", nil)
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
			t.Fatalf("status = %d", rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
			t.Fatalf("cache-control = %q", cc)
		}
	})
	t.Run("head", func(t *testing.T) {
		router := gin.New()
		router.HEAD("/static/*filepath", e.serveStatic)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/static/gopress/ui.js", nil))
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %d bytes", rec.Code, rec.Body.Len())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
			t.Fatalf("content-type = %q", ct)
		}
	})
	for _, target := range []string{"/static/gopress/extra.js", "/static/gopress/", "/static/gopress/../css/style.css"} {
		t.Run("reserved "+target, func(t *testing.T) {
			rec := serveStaticForTest(e, target, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("cache-control = %q", cc)
			}
			if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "javascript") {
				t.Fatalf("content-type = %q on a 404", ct)
			}
		})
	}
	t.Run("theme files elsewhere", func(t *testing.T) {
		if rec := serveStaticForTest(e, "/static/css/style.css", nil); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// A ".." segment is refused before any lookup. Before, /static/admin/ and
// /static/uploads/ answered 400 for an existing file outside their
// directory and 404 for a missing one, which revealed what files exist.
func TestServeStaticRefusesDotDotSegments(t *testing.T) {
	files := assetTestFiles()
	files["static/css/a..b.css"] = &fstest.MapFile{Data: []byte("a{}")}
	e := newAssetTestEngine(newAssetTestTheme(files), "release")
	e.Config.CMS.UploadDir = t.TempDir()

	// The tests run in core/, so engine.go exists three levels above
	// core/admin/static.
	for _, target := range []string{
		"/static/admin/../../../engine.go",
		"/static/admin/../../../no-such-file.go",
		"/static/uploads/../../core/engine.go",
		`/static/admin/..\..\..\engine.go`,
		"/static/admin/%2e%2e/%2e%2e/%2e%2e/engine.go",
		"/static/css/../css/style.css",
		"/static/gopress/../gopress/ui.js",
	} {
		t.Run(target, func(t *testing.T) {
			rec := serveStaticForTest(e, target, nil)
			if rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status = %d, cache-control = %q, want an uncached 404", rec.Code, rec.Header().Get("Cache-Control"))
			}
		})
	}
	for _, target := range []string{"/static/css/style.css", "/static/css/a..b.css", "/static/gopress/ui.js"} {
		if rec := serveStaticForTest(e, target, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
	}
}
