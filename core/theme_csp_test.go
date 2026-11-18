// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/hook"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// packageTestTheme is a test theme that reports itself as a theme package.
type packageTestTheme struct {
	*activationTheme
}

func (t packageTestTheme) PackageInfo() coreTheme.PackageInfo {
	return coreTheme.PackageInfo{Slug: t.name, Version: "1.0.0", Digest: "h1:test", Source: coreTheme.PackageSourceInstalled}
}

func cspPackageTheme(external ...coreTheme.ExternalConfig) coreTheme.Theme {
	return packageTestTheme{&activationTheme{name: "aurora", cfg: &coreTheme.FileConfig{External: external}}}
}

func TestThemePackageCSPFollowsTheDeclaration(t *testing.T) {
	if p := themePackageCSP(&activationTheme{name: "built-in", cfg: &coreTheme.FileConfig{}}); p != nil {
		t.Fatal("a theme built into GoPress must get no policy")
	}
	p := themePackageCSP(cspPackageTheme(
		coreTheme.ExternalConfig{Host: "fonts.googleapis.com", Kinds: []string{"style"}},
		coreTheme.ExternalConfig{Host: "fonts.gstatic.com", Kinds: []string{"font"}},
		coreTheme.ExternalConfig{Host: "*.cdn.example.com", Kinds: []string{"style", "font"}},
		coreTheme.ExternalConfig{Host: "images.example.com", Kinds: []string{"image"}},
	))
	want := "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://*.cdn.example.com; " +
		"font-src 'self' data: https://fonts.gstatic.com https://*.cdn.example.com; " +
		"img-src 'self' data: https:; " +
		"media-src 'self' https:; " +
		"frame-src 'self' https:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'"
	if got := p.header(CSPSources{}); got != want {
		t.Fatalf("policy\n got %s\nwant %s", got, want)
	}
}

func TestCSPPluginSourcesAreChecked(t *testing.T) {
	p := themePackageCSP(cspPackageTheme())
	got := p.header(CSPSources{
		Script: []string{
			"https://js.example.com", "'sha256-B2yPHKaXnvFWtRChIbabYmUBFZdVfKKXHbWtWidDVF8='", "'self'",
			// Refused: each would let other scripts run.
			"'unsafe-inline'", "'unsafe-eval'", "'unsafe-hashes'", "'strict-dynamic'", "*", "https:", "data:",
			"'nonce-abc'", "'none'", "https://evil.example; script-src *", "https://a.example https://b.example",
			"http://plain.example.com", "javascript:",
		},
		Style:   []string{"https://styles.example.com", "'unsafe-eval'"},
		Image:   []string{"blob:"},
		Frame:   []string{"https://pay.example.com/checkout/"},
		Connect: []string{"https://api.example.com:8443", "http://127.0.0.1:8132", "wss://socket.example.com"},
	})
	directives := map[string]string{}
	for _, part := range strings.Split(got, "; ") {
		name, sources, _ := strings.Cut(part, " ")
		directives[name] = sources
	}
	for name, want := range map[string]string{
		"script-src":  "'self' https://js.example.com 'sha256-B2yPHKaXnvFWtRChIbabYmUBFZdVfKKXHbWtWidDVF8='",
		"style-src":   "'self' 'unsafe-inline' https://styles.example.com",
		"img-src":     "'self' data: https: blob:",
		"frame-src":   "'self' https: https://pay.example.com/checkout/",
		"connect-src": "'self' https://api.example.com:8443 http://127.0.0.1:8132",
		"font-src":    "'self' data:",
		"object-src":  "'none'",
	} {
		if directives[name] != want {
			t.Errorf("%s = %q, want %q", name, directives[name], want)
		}
	}
	if len(directives) != 10 {
		t.Fatalf("policy has %d directives, want 10 (a source must not add one):\n%s", len(directives), got)
	}
}

func TestCSPMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := &Engine{Hooks: hook.New()}
	e.Hooks.AddFilter(hook.FrontendCSP, func(value interface{}, args ...interface{}) interface{} {
		sources := value.(CSPSources)
		if c, ok := args[0].(*gin.Context); ok && c.Request.URL.Path == "/checkout/pay" {
			sources.Script = append(sources.Script, "https://pay.example.com")
		}
		return sources
	}, 10)
	router := gin.New()
	router.Use(e.contentSecurityPolicyMiddleware())
	router.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "page") })
	router.GET("/signin", func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'none'")
		c.String(http.StatusOK, "own policy")
	})
	get := func(path string) string {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Header().Get("Content-Security-Policy")
	}

	if got := get("/"); got != "" {
		t.Fatalf("a built-in theme got a policy: %s", got)
	}

	e.setThemeCSP(cspPackageTheme())
	for path, want := range map[string]bool{
		"/": true, "/blog/hello": true, "/administrator-notes": true, "/static/css/style.css": true,
		"/admin": false, "/admin/themes": false, "/api/v1/posts": false, "/swagger/index.html": false,
	} {
		if got := get(path) != ""; got != want {
			t.Errorf("%s: policy present = %v, want %v", path, got, want)
		}
	}
	if got := get("/checkout/pay"); !strings.Contains(got, "script-src 'self' https://pay.example.com;") {
		t.Errorf("plugin source missing on its page: %s", got)
	}
	if got := get("/"); strings.Contains(got, "pay.example.com") {
		t.Errorf("plugin source leaked to another page: %s", got)
	}
	if got := get("/signin"); got != "default-src 'none'" {
		t.Errorf("a handler's own policy was replaced: %s", got)
	}

	// Switching back to a built-in theme drops the policy.
	e.setThemeCSP(&activationTheme{name: "built-in", cfg: &coreTheme.FileConfig{}})
	if got := get("/"); got != "" {
		t.Fatalf("policy kept after switching to a built-in theme: %s", got)
	}

	// No hook bus (partial engines in tests) is fine.
	bare := &Engine{}
	bare.setThemeCSP(cspPackageTheme())
	if got := bare.ThemeContentSecurityPolicy(&gin.Context{Request: httptest.NewRequest(http.MethodGet, "/", nil)}); !strings.HasPrefix(got, "default-src 'self'") {
		t.Fatalf("policy without hooks = %q", got)
	}
}
