package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/cache"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// trustedProxyTestRouter builds the real application router and adds a probe
// route that echoes the resolved client IP, so the assertions cover the same
// c.ClientIP() that the admin login throttle and API rate limiter use.
func trustedProxyTestRouter(t *testing.T, proxies []string) *gin.Engine {
	t.Helper()
	registry := content.NewRegistry()
	e := &Engine{
		Config:   &config.Config{Server: config.ServerConfig{TrustedProxies: proxies}},
		Hooks:    hook.New(),
		Cache:    cache.NewNoopManager(),
		Registry: registry,
		Auth:     user.NewAuth("trusted-proxy-test-secret", 1, nil),
		Sitemap:  rewrite.NewSitemapGenerator("https://example.test", registry, content.NewRepository(nil), rewrite.NewEngine(registry)),
	}
	r := e.SetupRouter()
	t.Cleanup(e.apiLimiter.Stop)
	r.GET("/__client_ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

func resolvedClientIP(t *testing.T, r *gin.Engine, remoteAddr, forwardedFor string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/__client_ip", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestRouterIgnoresSpoofedForwardedForByDefault(t *testing.T) {
	r := trustedProxyTestRouter(t, nil)
	tests := []struct {
		name, remoteAddr, forwardedFor, want string
	}{
		{"direct client spoofing the header", "203.0.113.9:5555", "6.6.6.6", "203.0.113.9"},
		{"same-host proxy appending to a spoofed header", "127.0.0.1:40000", "6.6.6.6, 203.0.113.9", "203.0.113.9"},
		{"same-host proxy overwriting the header", "127.0.0.1:40000", "203.0.113.9", "203.0.113.9"},
		{"same-host IPv6 proxy", "[::1]:40000", "203.0.113.9", "203.0.113.9"},
		{"same-host proxy without the header", "127.0.0.1:40000", "", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvedClientIP(t, r, tt.remoteAddr, tt.forwardedFor); got != tt.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRouterHonorsConfiguredProxyChain(t *testing.T) {
	r := trustedProxyTestRouter(t, []string{"127.0.0.1", " 198.51.100.0/24 ", ""})

	// CDN edge 198.51.100.7 -> local proxy -> app: the edge is trusted, so the
	// client it reports is used, while a value spoofed before it is not.
	if got := resolvedClientIP(t, r, "127.0.0.1:40000", "6.6.6.6, 203.0.113.9, 198.51.100.7"); got != "203.0.113.9" {
		t.Fatalf("ClientIP() through trusted CDN = %q, want 203.0.113.9", got)
	}
	// A peer outside the configured list still cannot speak for others.
	if got := resolvedClientIP(t, r, "192.0.2.50:5555", "6.6.6.6"); got != "192.0.2.50" {
		t.Fatalf("ClientIP() from untrusted peer = %q, want 192.0.2.50", got)
	}
}

func TestRouterFallsBackToLoopbackOnInvalidTrustedProxies(t *testing.T) {
	r := trustedProxyTestRouter(t, []string{"not-an-ip"})

	if got := resolvedClientIP(t, r, "203.0.113.9:5555", "6.6.6.6"); got != "203.0.113.9" {
		t.Fatalf("ClientIP() with invalid config = %q, want direct peer 203.0.113.9", got)
	}
	if got := resolvedClientIP(t, r, "127.0.0.1:40000", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("ClientIP() via loopback with invalid config = %q, want 203.0.113.9", got)
	}
}
