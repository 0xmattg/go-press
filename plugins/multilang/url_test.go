package multilang

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-press/core/admin"
	"go-press/core/menu"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestRewriteItemURLSkipsNonPageLinks(t *testing.T) {
	p := &Plugin{}
	tests := []string{
		"https://github.com/0xmattg/go-press",
		"http://example.com",
		"//cdn.example.com/app.js",
		"mailto:hello@example.com",
		"tel:+15550100",
		"#features",
		"?preview=1",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			if got := p.rewriteItemURL(item, "en"); got != rawURL {
				t.Fatalf("rewriteItemURL(%q) = %q, want unchanged", rawURL, got)
			}
		})
	}
}

func TestRewriteItemURLPrefixesLocalLinks(t *testing.T) {
	p := &Plugin{}
	tests := map[string]string{
		"/about": "en/about",
		"about":  "en/about",
		"/":      "en/",
	}

	for rawURL, wantSuffix := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			want := "/" + wantSuffix
			if got := p.rewriteItemURL(item, "en"); got != want {
				t.Fatalf("rewriteItemURL(%q) = %q, want %q", rawURL, got, want)
			}
		})
	}
}

func TestSiteOptionTranslationRouteRejectsSubscriber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "reader", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	p := &Plugin{}
	r := gin.New()
	r.POST(
		"/admin/plugins/multi-language/site-option-translate",
		admin.RequirePermission(auth, user.NewRBAC(), "plugin", "update"),
		p.handleSiteOptionTranslationSave,
	)
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/multi-language/site-option-translate", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
