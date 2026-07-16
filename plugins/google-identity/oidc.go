package googleidentity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const googleIssuer = "https://accounts.google.com"

var (
	errMissingIDToken  = errors.New("missing id token")
	errInvalidNonce    = errors.New("invalid nonce")
	errUnverifiedEmail = errors.New("unverified email")
	errHostedDomain    = errors.New("hosted domain mismatch")
)

type googleProfile struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	HostedDomain  string
	Locale        string
	ProfileJSON   string
}

type googleClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	HostedDomain  string `json:"hd"`
	Locale        string `json:"locale"`
	Nonce         string `json:"nonce"`
}

type oidcFlow interface {
	AuthorizationURL(context.Context, providerConfig, loginChallenge) (string, error)
	ExchangeAndVerify(context.Context, providerConfig, string, loginChallenge) (googleProfile, error)
}

type standardOIDCFlow struct {
	mu       sync.RWMutex
	provider *oidc.Provider
}

func newStandardOIDCFlow() *standardOIDCFlow { return &standardOIDCFlow{} }

func (f *standardOIDCFlow) AuthorizationURL(ctx context.Context, config providerConfig, challenge loginChallenge) (string, error) {
	provider, err := f.getProvider(ctx)
	if err != nil {
		return "", err
	}
	oauthConfig := oauthConfig(provider, config)
	options := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", challenge.Nonce),
		oauth2.S256ChallengeOption(challenge.Verifier),
	}
	if config.HostedDomain != "" {
		options = append(options, oauth2.SetAuthURLParam("hd", config.HostedDomain))
	}
	return oauthConfig.AuthCodeURL(challenge.State, options...), nil
}

func (f *standardOIDCFlow) ExchangeAndVerify(ctx context.Context, config providerConfig, code string, challenge loginChallenge) (googleProfile, error) {
	provider, err := f.getProvider(ctx)
	if err != nil {
		return googleProfile{}, err
	}
	oauthConfig := oauthConfig(provider, config)
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(challenge.Verifier))
	if err != nil {
		return googleProfile{}, err
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return googleProfile{}, errMissingIDToken
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return googleProfile{}, err
	}
	if err := idToken.VerifyAccessToken(token.AccessToken); err != nil {
		return googleProfile{}, err
	}
	var claims googleClaims
	if err := idToken.Claims(&claims); err != nil {
		return googleProfile{}, err
	}
	return verifiedGoogleProfile(idToken.Issuer, claims, config, challenge.Nonce)
}

func verifiedGoogleProfile(issuer string, claims googleClaims, config providerConfig, expectedNonce string) (googleProfile, error) {
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		return googleProfile{}, errInvalidNonce
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return googleProfile{}, errors.New("missing subject")
	}
	if !claims.EmailVerified || strings.TrimSpace(claims.Email) == "" {
		return googleProfile{}, errUnverifiedEmail
	}
	if config.HostedDomain != "" && !strings.EqualFold(claims.HostedDomain, config.HostedDomain) {
		return googleProfile{}, errHostedDomain
	}
	profileData, err := json.Marshal(map[string]string{
		"name": claims.Name, "picture": claims.Picture, "hd": claims.HostedDomain, "locale": claims.Locale,
	})
	if err != nil {
		return googleProfile{}, err
	}
	if issuer == "accounts.google.com" {
		issuer = googleIssuer
	}
	return googleProfile{
		Issuer: issuer, Subject: claims.Subject, Email: claims.Email,
		EmailVerified: claims.EmailVerified, Name: claims.Name, Picture: claims.Picture,
		HostedDomain: claims.HostedDomain, Locale: claims.Locale, ProfileJSON: string(profileData),
	}, nil
}

func (f *standardOIDCFlow) getProvider(ctx context.Context) (*oidc.Provider, error) {
	f.mu.RLock()
	provider := f.provider
	f.mu.RUnlock()
	if provider != nil {
		return provider, nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	discovered, err := oidc.NewProvider(discoveryCtx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover google oidc provider: %w", err)
	}
	f.mu.Lock()
	if f.provider == nil {
		f.provider = discovered
	}
	provider = f.provider
	f.mu.Unlock()
	return provider, nil
}

func oauthConfig(provider *oidc.Provider, config providerConfig) oauth2.Config {
	return oauth2.Config{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: config.RedirectURL,
		Scopes: []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}
}
