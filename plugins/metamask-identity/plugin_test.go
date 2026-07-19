package metamaskidentity

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	siwe "github.com/signinwithethereum/siwe-go"

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

type fakeAuthService struct {
	mu       sync.Mutex
	enabled  bool
	loginErr error
	verified user.VerifiedIdentity
	options  user.IdentityLoginOptions
	calls    int
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
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.verified = verified
	a.options = options
	if a.loginErr != nil {
		return nil, a.loginErr
	}
	return &user.IdentityResult{}, nil
}

type memoryChallengeStore struct {
	mu         sync.Mutex
	nextID     uint
	byToken    map[string]*walletChallenge
	consumeErr error
}

func newMemoryChallengeStore() *memoryChallengeStore {
	return &memoryChallengeStore{byToken: make(map[string]*walletChallenge)}
}

func (s *memoryChallengeStore) Create(_ context.Context, challenge *walletChallenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	copy := *challenge
	copy.ID = s.nextID
	challenge.ID = copy.ID
	s.byToken[copy.TokenHash] = &copy
	return nil
}

func (s *memoryChallengeStore) FindActiveByToken(_ context.Context, tokenHash string, now time.Time) (*walletChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.byToken[tokenHash]
	if !ok || challenge.UsedAt != nil || !challenge.ExpiresAt.After(now) {
		return nil, errChallengeUnavailable
	}
	copy := *challenge
	return &copy, nil
}

func (s *memoryChallengeStore) Consume(_ context.Context, id uint, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumeErr != nil {
		return s.consumeErr
	}
	for _, challenge := range s.byToken {
		if challenge.ID == id && challenge.UsedAt == nil && challenge.ExpiresAt.After(now) {
			usedAt := now
			challenge.UsedAt = &usedAt
			return nil
		}
	}
	return errChallengeUnavailable
}

func (s *memoryChallengeStore) DeleteStale(_ context.Context, _ time.Time) error { return nil }

func configuredPlugin(store challengeStore, auth *fakeAuthService, now time.Time) *Plugin {
	p := New()
	p.options = memoryOptions{
		optEnabled: "1", optChainID: "1", optAutoRegister: "1",
	}
	p.siteURL = "https://site.example"
	p.repo = store
	p.auth = auth
	p.now = func() time.Time { return now }
	p.active.Store(true)
	return p
}

func TestConfigRejectsInvalidChainIDAndSiteURL(t *testing.T) {
	p := New()
	p.options = memoryOptions{optEnabled: "1", optChainID: "0"}
	p.siteURL = "javascript:alert(1)"
	config := p.loadConfig()
	if config.ready() || config.ChainIDValid || config.SiteURLValid {
		t.Fatalf("invalid config became ready: %#v", config)
	}
}

func TestProviderRegistrationUsesGenericRegistryAndLocalIcon(t *testing.T) {
	auth := &fakeAuthService{enabled: true}
	p := configuredPlugin(newMemoryChallengeStore(), auth, time.Now())
	p.syncProvider()
	provider, ok := auth.Providers().Get(providerID)
	if !ok || provider.BeginURL != startPath || provider.IconURL != assetBasePath+"metamask-fox.svg" {
		t.Fatalf("registered provider = %#v, %v", provider, ok)
	}
	p.active.Store(false)
	p.syncProvider()
	if _, ok := auth.Providers().Get(providerID); ok {
		t.Fatal("inactive plugin left provider registered")
	}
}

func TestStartPageRendersCSPChainAndEncodedLoginReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := configuredPlugin(newMemoryChallengeStore(), &fakeAuthService{enabled: true}, time.Now())
	router := gin.New()
	router.GET(startPath, p.handleStart)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, startPath+"?return_to=%2Faccount%3Ftab%3Dwallet", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("start page status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		!strings.Contains(body, `data-chain-id="1"`) ||
		!strings.Contains(body, `signin.js?v=1.0.1`) ||
		!strings.Contains(body, `/login?return_to=%2Faccount%3Ftab%3Dwallet`) {
		t.Fatalf("start page security/config output missing: %s", body)
	}
}

func TestSignInScriptSelectsMetaMaskWithoutFallingBackToPhantom(t *testing.T) {
	script, err := publicFiles.ReadFile("static/signin.js")
	if err != nil {
		t.Fatal(err)
	}
	content := string(script)
	for _, required := range []string{
		`eip6963:announceProvider`,
		`eip6963:requestProvider`,
		`candidate.info.rdns.toLowerCase() === "io.metamask"`,
		`provider.isPhantom !== true`,
		`injected.providers.find(isMetaMaskProvider)`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("MetaMask provider selection is missing %q", required)
		}
	}
}

func TestBuildAndVerifyWalletChallenge(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	key, address := testWallet(t)
	config := providerConfig{
		Enabled: true, ChainID: 1, ChainIDHex: "0x1", ChainIDValid: true,
		Scheme: "https", Origin: "https://site.example", Domain: "site.example", URI: "https://site.example", SiteURLValid: true,
	}
	challenge, response, err := buildWalletChallenge(config, address, "/account", now)
	if err != nil {
		t.Fatal(err)
	}
	signature := signSIWE(t, key, response.Message)
	verifiedAddress, err := verifyWalletChallenge(context.Background(), challenge, response.Message, signature, now.Add(time.Second))
	if err != nil {
		t.Fatalf("verifyWalletChallenge() error = %v", err)
	}
	if verifiedAddress != strings.ToLower(address) || challenge.ReturnTo != "/account" {
		t.Fatalf("verified address/return path = %q %q", verifiedAddress, challenge.ReturnTo)
	}
	if challenge.TokenHash == response.Token || challenge.NonceHash == "" || challenge.MessageHash == "" {
		t.Fatalf("challenge secrets were not hashed: %#v", challenge)
	}
}

func TestWalletChallengeRejectsTamperingWrongDomainAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	key, address := testWallet(t)
	config := providerConfig{Enabled: true, ChainID: 1, ChainIDValid: true, Scheme: "https", Domain: "site.example", URI: "https://site.example", SiteURLValid: true}
	challenge, response, err := buildWalletChallenge(config, address, "/", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWalletChallenge(context.Background(), challenge, response.Message+" ", signSIWE(t, key, response.Message), now); !errors.Is(err, errInvalidWalletProof) {
		t.Fatalf("tampered message error = %v", err)
	}

	parsed, err := siwe.ParseMessage(response.Message)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Domain = "evil.example"
	wrongDomainMessage := parsed.String()
	wrongDomain := *challenge
	wrongDomain.MessageHash = hashValue(wrongDomainMessage)
	if _, err := verifyWalletChallenge(context.Background(), &wrongDomain, wrongDomainMessage, signSIWE(t, key, wrongDomainMessage), now); !errors.Is(err, errInvalidWalletProof) {
		t.Fatalf("wrong domain error = %v", err)
	}
	if _, err := verifyWalletChallenge(context.Background(), challenge, response.Message, signSIWE(t, key, response.Message), now.Add(challengeLifetime)); !errors.Is(err, errInvalidWalletProof) {
		t.Fatalf("expired message error = %v", err)
	}
}

func TestVerifyHandlerConsumesChallengeOnceAndForwardsIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	store := newMemoryChallengeStore()
	auth := &fakeAuthService{enabled: true}
	p := configuredPlugin(store, auth, now)
	key, address := testWallet(t)
	challenge, response, err := buildWalletChallenge(p.loadConfig(), address, "/account", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), challenge); err != nil {
		t.Fatal(err)
	}
	payload := verifyRequest{ChallengeToken: response.Token, Message: response.Message, Signature: signSIWE(t, key, response.Message)}

	first := serveJSON(p.handleVerify, verifyPath, "https://site.example", payload)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"redirect_url":"/account"`) {
		t.Fatalf("first verify = %d %s", first.Code, first.Body.String())
	}
	second := serveJSON(p.handleVerify, verifyPath, "https://site.example", payload)
	if second.Code != http.StatusUnauthorized || !strings.Contains(second.Body.String(), "invalid_challenge") {
		t.Fatalf("replayed verify = %d %s", second.Code, second.Body.String())
	}
	if auth.calls != 1 || auth.verified.Provider != providerID || auth.verified.Issuer != "eip155:1" || auth.verified.Subject != strings.ToLower(address) || !auth.options.AllowRegistration {
		t.Fatalf("core identity call = calls:%d identity:%#v options:%#v", auth.calls, auth.verified, auth.options)
	}
}

func TestVerifyHandlerRejectsCrossOriginBeforeChallengeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	p := configuredPlugin(newMemoryChallengeStore(), &fakeAuthService{enabled: true}, now)
	recorder := serveJSON(p.handleVerify, verifyPath, "https://evil.example", verifyRequest{})
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "invalid_origin") {
		t.Fatalf("cross-origin response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestVerifyHandlerUsesCoreRegistrationPolicyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	store := newMemoryChallengeStore()
	auth := &fakeAuthService{enabled: true, loginErr: user.ErrRegistrationDisabled}
	p := configuredPlugin(store, auth, now)
	key, address := testWallet(t)
	challenge, response, err := buildWalletChallenge(p.loadConfig(), address, "/products", now)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Create(context.Background(), challenge)
	payload := verifyRequest{ChallengeToken: response.Token, Message: response.Message, Signature: signSIWE(t, key, response.Message)}
	recorder := serveJSON(p.handleVerify, verifyPath, "https://site.example", payload)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "registration_disabled") || !strings.Contains(recorder.Body.String(), "%2Fproducts") {
		t.Fatalf("registration response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestChallengeHandlerRequiresJSONAndSameOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	p := configuredPlugin(newMemoryChallengeStore(), &fakeAuthService{enabled: true}, now)
	_, address := testWallet(t)

	crossOrigin := serveJSON(p.handleChallenge, challengePath, "https://evil.example", challengeRequest{Address: address})
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin challenge status = %d", crossOrigin.Code)
	}

	router := gin.New()
	router.POST(challengePath, p.handleChallenge)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, challengePath, strings.NewReader(`{"address":"`+address+`"}`))
	request.Header.Set("Origin", "https://site.example")
	request.Header.Set("Content-Type", "text/plain")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_request") {
		t.Fatalf("non-JSON challenge = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRequestLimiterRejectsBeyondLimit(t *testing.T) {
	limiter := newRequestLimiter()
	now := time.Now()
	if !limiter.Allow("ip:test", 1, time.Minute, now) || limiter.Allow("ip:test", 1, time.Minute, now) {
		t.Fatal("limiter did not enforce fixed window")
	}
	if !limiter.Allow("ip:test", 1, time.Minute, now.Add(time.Minute)) {
		t.Fatal("limiter did not reset after window")
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

func testWallet(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey).Hex()
}

func signSIWE(t *testing.T, key *ecdsa.PrivateKey, messageText string) string {
	t.Helper()
	message, err := siwe.ParseMessage(messageText)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(message.EIP191Hash().Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	signature[64] += 27
	return hexutil.Encode(signature)
}

func serveJSON(handler gin.HandlerFunc, path, origin string, payload interface{}) *httptest.ResponseRecorder {
	data, _ := json.Marshal(payload)
	router := gin.New()
	router.POST(path, handler)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	router.ServeHTTP(recorder, request)
	return recorder
}
