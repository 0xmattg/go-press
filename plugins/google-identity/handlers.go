package googleidentity

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"

	"go-press/core/user"
	"go-press/pkg/logger"
)

func (p *Plugin) handleStart(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	config := p.loadConfig()
	if !p.active.Load() || p.auth == nil || !p.auth.ExternalLoginEnabled() || !config.ready() {
		p.redirectLoginError(c, "provider_unavailable")
		return
	}
	verifier := oauth2.GenerateVerifier()
	challenge, err := newLoginChallenge(c.Query("return_to"), verifier, time.Now())
	if err != nil {
		p.redirectLoginError(c, "provider_unavailable")
		return
	}
	encoded, err := encodeChallenge(challenge, config.ClientSecret)
	if err != nil {
		p.redirectLoginError(c, "provider_unavailable")
		return
	}
	flowContext, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	authURL, err := p.flow.AuthorizationURL(flowContext, config, challenge)
	if err != nil {
		logger.Error("google-identity: authorization endpoint unavailable", "error", err)
		p.redirectLoginError(c, "provider_unavailable")
		return
	}
	setChallengeCookie(c, encoded, strings.HasPrefix(config.RedirectURL, "https://"), time.Now())
	c.Redirect(http.StatusFound, authURL)
}

func (p *Plugin) handleCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	config := p.loadConfig()
	secure := strings.HasPrefix(config.RedirectURL, "https://")
	cookie, cookieErr := c.Cookie(stateCookieName)
	clearChallengeCookie(c, secure)
	if !p.active.Load() || p.auth == nil || !p.auth.ExternalLoginEnabled() || !config.ready() || cookieErr != nil {
		p.redirectLoginError(c, "provider_unavailable")
		return
	}
	challenge, err := decodeChallenge(cookie, config.ClientSecret, time.Now())
	if err != nil || len(c.Query("state")) > 512 || subtle.ConstantTimeCompare([]byte(challenge.State), []byte(c.Query("state"))) != 1 {
		p.redirectLoginError(c, "authentication_failed")
		return
	}
	if c.Query("error") != "" {
		p.redirectLoginErrorWithReturnTo(c, "authentication_failed", challenge.ReturnTo)
		return
	}
	code := c.Query("code")
	if code == "" || len(code) > 4096 {
		p.redirectLoginErrorWithReturnTo(c, "authentication_failed", challenge.ReturnTo)
		return
	}
	flowContext, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	profile, err := p.flow.ExchangeAndVerify(flowContext, config, code, challenge)
	if err != nil {
		logger.Info("google-identity: callback verification failed", "error", err)
		p.redirectLoginErrorWithReturnTo(c, "authentication_failed", challenge.ReturnTo)
		return
	}
	_, err = p.auth.LoginVerifiedIdentityWithOptions(c, user.VerifiedIdentity{
		Provider: providerID, Issuer: profile.Issuer, Subject: profile.Subject,
		Email: profile.Email, EmailVerified: profile.EmailVerified,
		DisplayName: profile.Name, AvatarURL: profile.Picture,
		ProfileJSON: profile.ProfileJSON, VerifiedAt: time.Now().UTC(),
	}, user.IdentityLoginOptions{AllowRegistration: config.AllowRegistration})
	if err != nil {
		p.redirectLoginErrorWithReturnTo(c, loginErrorCode(err), challenge.ReturnTo)
		return
	}
	c.Redirect(http.StatusSeeOther, user.SafeReturnTo(challenge.ReturnTo, "/"))
}

func loginErrorCode(err error) string {
	switch {
	case errors.Is(err, user.ErrRegistrationDisabled):
		return "registration_disabled"
	case errors.Is(err, user.ErrIdentityConflict), errors.Is(err, user.ErrEmailAlreadyInUse):
		return "identity_conflict"
	case errors.Is(err, user.ErrExternalLoginDisabled):
		return "provider_unavailable"
	default:
		return "authentication_failed"
	}
}

func (p *Plugin) redirectLoginError(c *gin.Context, code string) {
	p.redirectLoginErrorWithReturnTo(c, code, user.SafeReturnTo(c.Query("return_to"), "/"))
}

func (p *Plugin) redirectLoginErrorWithReturnTo(c *gin.Context, code, returnTo string) {
	query := url.Values{}
	query.Set("error", code)
	query.Set("return_to", user.SafeReturnTo(returnTo, "/"))
	c.Redirect(http.StatusSeeOther, "/login?"+query.Encode())
}
