package googleidentity

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
)

type memoryOptions map[string]string

func (o memoryOptions) Get(key string) string { return o[key] }
func (o memoryOptions) GetDefault(key, fallback string) string {
	if value := o[key]; value != "" {
		return value
	}
	return fallback
}
func (o memoryOptions) Set(key, value string) error { o[key] = value; return nil }

type fakeAuthService struct {
	enabled  bool
	loginErr error
	verified user.VerifiedIdentity
	options  user.IdentityLoginOptions
	registry *user.ProviderRegistry
}

func (a *fakeAuthService) Providers() *user.ProviderRegistry {
	if a.registry == nil {
		a.registry = user.NewProviderRegistry()
	}
	return a.registry
}
func (a *fakeAuthService) ExternalLoginEnabled() bool { return a.enabled }
func (a *fakeAuthService) LoginVerifiedIdentityWithOptions(_ *gin.Context, verified user.VerifiedIdentity, options user.IdentityLoginOptions) (*user.IdentityResult, error) {
	a.verified = verified
	a.options = options
	if a.loginErr != nil {
		return nil, a.loginErr
	}
	return &user.IdentityResult{}, nil
}

type fakeOIDCFlow struct {
	authURL       string
	profile       googleProfile
	err           error
	challenge     loginChallenge
	exchangeCalls int
}

func (f *fakeOIDCFlow) AuthorizationURL(_ context.Context, _ providerConfig, challenge loginChallenge) (string, error) {
	f.challenge = challenge
	return f.authURL, f.err
}
func (f *fakeOIDCFlow) ExchangeAndVerify(_ context.Context, _ providerConfig, _ string, challenge loginChallenge) (googleProfile, error) {
	f.exchangeCalls++
	f.challenge = challenge
	return f.profile, f.err
}

func configuredTestPlugin(flow oidcFlow, auth *fakeAuthService) *Plugin {
	options := memoryOptions{
		optEnabled: "1", optClientID: "client-id", optClientSecret: "client-secret",
		optAutoRegister: "0",
	}
	p := &Plugin{options: options, siteURL: "https://site.example", auth: auth, flow: flow, secretCache: "client-secret"}
	p.active.Store(true)
	return p
}

func TestInvalidHostedDomainDoesNotSilentlyDisableRestriction(t *testing.T) {
	p := configuredTestPlugin(&fakeOIDCFlow{}, &fakeAuthService{enabled: true})
	p.options.(memoryOptions)[optHostedDomain] = "https://example.com"
	config := p.loadConfig()
	if config.HostedDomainValid || config.ready() {
		t.Fatalf("invalid hosted domain produced ready config: %#v", config)
	}
}

func TestProviderRegistrationIncludesLocalGoogleLogo(t *testing.T) {
	auth := &fakeAuthService{enabled: true}
	p := configuredTestPlugin(&fakeOIDCFlow{}, auth)
	p.syncProvider()
	provider, ok := auth.Providers().Get(providerID)
	if !ok || provider.BeginURL != startPath || provider.IconURL != logoPath+"?v="+pluginVersion {
		t.Fatalf("registered provider = %#v, %v", provider, ok)
	}
}

func TestGoogleLogoIsServedFromPluginAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := configuredTestPlugin(&fakeOIDCFlow{}, &fakeAuthService{enabled: true})
	router := gin.New()
	router.GET(logoPath, p.handleLogo)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, logoPath, nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || len(recorder.Body.Bytes()) == 0 {
		t.Fatalf("logo response = %d %q %d bytes", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.Len())
	}
}

func TestStartCreatesSignedChallengeWithPKCEInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flow := &fakeOIDCFlow{authURL: "https://accounts.example/authorize"}
	auth := &fakeAuthService{enabled: true}
	p := configuredTestPlugin(flow, auth)
	router := gin.New()
	router.GET(startPath, p.handleStart)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, startPath+"?return_to=%2Faccount", nil))
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != flow.authURL {
		t.Fatalf("start response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if flow.challenge.State == "" || flow.challenge.Nonce == "" || flow.challenge.Verifier == "" || flow.challenge.ReturnTo != "/account" {
		t.Fatalf("incomplete challenge: %#v", flow.challenge)
	}
	var stateCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == stateCookieName {
			stateCookie = cookie
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly || !stateCookie.Secure || stateCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie security attributes = %#v", stateCookie)
	}
	decoded, err := decodeChallenge(stateCookie.Value, "client-secret", time.Now())
	if err != nil || decoded.State != flow.challenge.State || decoded.Verifier != flow.challenge.Verifier {
		t.Fatalf("decodeChallenge() = %#v, %v", decoded, err)
	}
}

func TestCallbackRejectsInvalidStateBeforeTokenExchange(t *testing.T) {
	flow := &fakeOIDCFlow{}
	p := configuredTestPlugin(flow, &fakeAuthService{enabled: true})
	challenge, cookie := signedTestChallenge(t, "client-secret", "/account")
	recorder := serveCallback(p, cookie, "wrong-state", "code")
	if recorder.Code != http.StatusSeeOther || !strings.Contains(recorder.Header().Get("Location"), "authentication_failed") {
		t.Fatalf("callback response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if flow.exchangeCalls != 0 || challenge.State == "wrong-state" {
		t.Fatalf("token exchange ran for invalid state")
	}
}

func TestCallbackPassesProviderRegistrationPolicyToCore(t *testing.T) {
	flow := &fakeOIDCFlow{profile: googleProfile{
		Issuer: googleIssuer, Subject: "google-subject", Email: "user@example.com",
		EmailVerified: true, Name: "Example User", ProfileJSON: `{}`,
	}}
	auth := &fakeAuthService{enabled: true, loginErr: user.ErrRegistrationDisabled}
	p := configuredTestPlugin(flow, auth)
	challenge, cookie := signedTestChallenge(t, "client-secret", "/account")
	recorder := serveCallback(p, cookie, challenge.State, "authorization-code")
	if recorder.Code != http.StatusSeeOther || !strings.Contains(recorder.Header().Get("Location"), "registration_disabled") {
		t.Fatalf("callback response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if auth.options.AllowRegistration {
		t.Fatal("provider registration setting was not enforced")
	}
	if auth.verified.Provider != providerID || auth.verified.Subject != "google-subject" {
		t.Fatalf("verified identity = %#v", auth.verified)
	}
}

func TestCallbackCompletesLoginAndUsesSafeReturnPath(t *testing.T) {
	flow := &fakeOIDCFlow{profile: googleProfile{
		Issuer: googleIssuer, Subject: "google-subject", Email: "user@example.com",
		EmailVerified: true, Name: "Example User", ProfileJSON: `{}`,
	}}
	auth := &fakeAuthService{enabled: true}
	p := configuredTestPlugin(flow, auth)
	p.options.(memoryOptions)[optAutoRegister] = "1"
	challenge, cookie := signedTestChallenge(t, "client-secret", "/account?tab=profile")
	recorder := serveCallback(p, cookie, challenge.State, "authorization-code")
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/account?tab=profile" {
		t.Fatalf("callback response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if !auth.options.AllowRegistration {
		t.Fatal("provider registration setting was not forwarded")
	}
}

func TestVerifiedGoogleProfileRequiresNonceVerifiedEmailAndHostedDomain(t *testing.T) {
	base := googleClaims{Subject: "sub", Email: "user@example.com", EmailVerified: true, Nonce: "nonce", HostedDomain: "example.com"}
	config := providerConfig{HostedDomain: "example.com"}
	if _, err := verifiedGoogleProfile(googleIssuer, base, config, "wrong"); !errors.Is(err, errInvalidNonce) {
		t.Fatalf("nonce error = %v", err)
	}
	unverified := base
	unverified.EmailVerified = false
	if _, err := verifiedGoogleProfile(googleIssuer, unverified, config, "nonce"); !errors.Is(err, errUnverifiedEmail) {
		t.Fatalf("unverified email error = %v", err)
	}
	wrongDomain := base
	wrongDomain.HostedDomain = "other.example"
	if _, err := verifiedGoogleProfile(googleIssuer, wrongDomain, config, "nonce"); !errors.Is(err, errHostedDomain) {
		t.Fatalf("hosted domain error = %v", err)
	}
	if _, err := verifiedGoogleProfile(googleIssuer, base, config, "nonce"); err != nil {
		t.Fatalf("valid profile error = %v", err)
	}
}

func TestSignedChallengeRejectsTamperingAndExpiry(t *testing.T) {
	challenge, err := newLoginChallenge("/", "verifier", time.Now().Add(-stateLifetime-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeChallenge(challenge, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeChallenge(encoded, "secret", time.Now()); !errors.Is(err, errInvalidState) {
		t.Fatalf("expired state error = %v", err)
	}
	tampered := encoded[:len(encoded)-1] + "A"
	if _, err := decodeChallenge(tampered, "secret", time.Now()); !errors.Is(err, errInvalidState) {
		t.Fatalf("tampered state error = %v", err)
	}
}

func TestSettingsSavePreservesBlankClientSecret(t *testing.T) {
	options := memoryOptions{optClientSecret: ""}
	p := &Plugin{options: options, secretCache: "existing-secret"}
	p.OnSettingsSave(map[string]string{optClientSecret: ""})
	if options[optClientSecret] != "existing-secret" {
		t.Fatalf("secret = %q", options[optClientSecret])
	}
}

func TestSettingsTemplateParsesWithAdminLayout(t *testing.T) {
	funcs := template.FuncMap{
		"T":              func(interface{}, string, ...interface{}) string { return "" },
		"X":              func(interface{}, string, string, ...interface{}) string { return "" },
		"jsT":            func(interface{}, string, ...interface{}) template.JS { return "" },
		"safeHTML":       func(string) template.HTML { return "" },
		"roleDisplayFor": func(interface{}, string) string { return "" },
	}
	if _, err := template.New("").Funcs(funcs).ParseFiles(
		"../../core/admin/templates/layouts/admin.tmpl",
		"../../"+New().SettingsTemplatePath(),
	); err != nil {
		t.Fatalf("parse settings template: %v", err)
	}
}

func signedTestChallenge(t *testing.T, secret, returnTo string) (loginChallenge, *http.Cookie) {
	t.Helper()
	challenge, err := newLoginChallenge(returnTo, "pkce-verifier", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeChallenge(challenge, secret)
	if err != nil {
		t.Fatal(err)
	}
	return challenge, &http.Cookie{Name: stateCookieName, Value: encoded, Path: "/auth/google"}
}

func serveCallback(p *Plugin, cookie *http.Cookie, state, code string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(callbackPath, p.handleCallback)
	query := url.Values{"state": {state}, "code": {code}}
	request := httptest.NewRequest(http.MethodGet, callbackPath+"?"+query.Encode(), nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
