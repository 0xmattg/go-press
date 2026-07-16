package user

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSafeReturnToRejectsExternalAndAuthPaths(t *testing.T) {
	for _, raw := range []string{"https://evil.example", "//evil.example", "/auth/provider/callback", "/login", "/logout", "/ok\r\nLocation: https://evil.example"} {
		if got := SafeReturnTo(raw, "/fallback"); got != "/fallback" {
			t.Errorf("SafeReturnTo(%q) = %q", raw, got)
		}
	}
	if got := SafeReturnTo("/products?page=2", "/"); got != "/products?page=2" {
		t.Fatalf("safe local path = %q", got)
	}
}

func TestPublicLoginPageListsRegisteredProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewProviderRegistry()
	if err := registry.Register(ProviderDescriptor{ID: "test", Label: "Test Identity", BeginURL: "/auth/test/start"}); err != nil {
		t.Fatal(err)
	}
	auth := NewPublicAuth(nil, nil, registry, NewRegistrationPolicy(optionMap{}, NewRBAC()), false, func() string { return "Example Site" })
	router := gin.New()
	auth.RegisterRoutes(router)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login?return_to=%2Faccount", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Continue with Test Identity") || !strings.Contains(body, "/auth/test/start?return_to=%2Faccount") {
		t.Fatalf("provider login page missing expected link: %s", body)
	}
}

func TestPublicLogoutRejectsCrossOriginRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := NewPublicAuth(nil, nil, NewProviderRegistry(), NewRegistrationPolicy(optionMap{}, NewRBAC()), false, nil)
	router := gin.New()
	auth.RegisterRoutes(router)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Host = "site.example"
	req.Header.Set("Origin", "https://evil.example")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestIdentityBindingRequiresAuthenticatedContextUser(t *testing.T) {
	auth := NewPublicAuth(nil, nil, nil, nil, false, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, err := auth.LinkVerifiedIdentity(c, VerifiedIdentity{}); err != ErrAuthenticationRequired {
		t.Fatalf("LinkVerifiedIdentity() error = %v", err)
	}
	if err := auth.UnlinkIdentity(c, 42); err != ErrAuthenticationRequired {
		t.Fatalf("UnlinkIdentity() error = %v", err)
	}
}

func TestPublicLoginPageOnlyDisplaysKnownErrorCodes(t *testing.T) {
	auth := NewPublicAuth(nil, nil, NewProviderRegistry(), NewRegistrationPolicy(optionMap{}, NewRBAC()), false, nil)
	router := gin.New()
	auth.RegisterRoutes(router)

	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "registration_disabled", want: "new registrations are currently disabled"},
		{query: "<script>alert(1)</script>", want: ""},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login?error="+url.QueryEscape(test.query), nil))
		if test.want != "" && !strings.Contains(recorder.Body.String(), test.want) {
			t.Fatalf("known error missing from response: %s", recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), test.query) {
			t.Fatalf("untrusted error code reflected in response: %s", recorder.Body.String())
		}
	}
}
