package metamaskidentity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
	"go-press/pkg/logger"
)

const (
	maxJSONBody     = 16 * 1024
	requestWindow   = 5 * time.Minute
	requestsPerIP   = 30
	requestsPerAddr = 10
)

type challengeRequest struct {
	Address  string `json:"address"`
	ReturnTo string `json:"return_to"`
}

type verifyRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Message        string `json:"message"`
	Signature      string `json:"signature"`
}

func (p *Plugin) handleStart(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	config := p.loadConfig()
	if !p.available(config) {
		p.redirectLoginError(c, "provider_unavailable", c.Query("return_to"))
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	returnTo := user.SafeReturnTo(c.Query("return_to"), "/")
	if err := p.page.ExecuteTemplate(c.Writer, "metamask-signin", map[string]interface{}{
		"Domain": config.Domain, "ChainID": config.ChainID, "ChainIDHex": config.ChainIDHex,
		"ReturnTo":     returnTo,
		"LoginURL":     "/login?return_to=" + url.QueryEscape(returnTo),
		"ChallengeURL": challengePath, "VerifyURL": verifyPath, "AssetBaseURL": assetBasePath,
		"AssetVersion": pluginVersion,
	}); err != nil {
		logger.Error("metamask-identity: render sign-in page failed", "error", err)
	}
}

func (p *Plugin) handleChallenge(c *gin.Context) {
	p.prepareJSON(c)
	config := p.loadConfig()
	if !p.available(config) {
		p.respondError(c, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	now := p.nowUTC()
	if !requestOriginMatches(c.Request, config) {
		p.respondError(c, http.StatusForbidden, "invalid_origin")
		return
	}
	if !p.limiter.Allow("ip:"+c.ClientIP(), requestsPerIP, requestWindow, now) {
		p.respondError(c, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var request challengeRequest
	if err := decodeJSONBody(c, &request); err != nil || len(strings.TrimSpace(request.Address)) > 64 || len(request.ReturnTo) > 2048 {
		p.respondError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	challenge, response, err := buildWalletChallenge(config, request.Address, request.ReturnTo, now)
	if err != nil {
		p.respondError(c, http.StatusBadRequest, "invalid_wallet")
		return
	}
	if !p.limiter.Allow("address:"+challenge.Address, requestsPerAddr, requestWindow, now) {
		p.respondError(c, http.StatusTooManyRequests, "rate_limited")
		return
	}
	_ = p.repo.DeleteStale(c.Request.Context(), now)
	if err := p.repo.Create(c.Request.Context(), challenge); err != nil {
		logger.Error("metamask-identity: create challenge failed", "error", err)
		p.respondError(c, http.StatusInternalServerError, "challenge_failed")
		return
	}
	c.JSON(http.StatusCreated, response)
}

func (p *Plugin) handleVerify(c *gin.Context) {
	p.prepareJSON(c)
	config := p.loadConfig()
	if !p.available(config) {
		p.respondError(c, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	now := p.nowUTC()
	if !requestOriginMatches(c.Request, config) {
		p.respondError(c, http.StatusForbidden, "invalid_origin")
		return
	}
	if !p.limiter.Allow("ip:"+c.ClientIP(), requestsPerIP, requestWindow, now) {
		p.respondError(c, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var request verifyRequest
	if err := decodeJSONBody(c, &request); err != nil || len(request.ChallengeToken) > 128 || len(request.Message) > 8192 || len(request.Signature) > 2048 {
		p.respondError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	challenge, err := p.repo.FindActiveByToken(c.Request.Context(), hashValue(request.ChallengeToken), now)
	if err != nil {
		p.respondError(c, http.StatusUnauthorized, "invalid_challenge")
		return
	}
	address, err := verifyWalletChallenge(c.Request.Context(), challenge, request.Message, request.Signature, now)
	if err != nil {
		p.respondError(c, http.StatusUnauthorized, "invalid_signature")
		return
	}
	if err := p.repo.Consume(c.Request.Context(), challenge.ID, now); err != nil {
		p.respondError(c, http.StatusUnauthorized, "invalid_challenge")
		return
	}
	profile, _ := json.Marshal(map[string]interface{}{
		"address": address, "chain_id": challenge.ChainID, "standard": "eip-4361", "wallet_interface": "eip-1193",
	})
	_, err = p.auth.LoginVerifiedIdentityWithOptions(c, user.VerifiedIdentity{
		Provider: providerID, Issuer: "eip155:" + strconv.Itoa(challenge.ChainID), Subject: address,
		DisplayName: shortAddress(address), ProfileJSON: string(profile), VerifiedAt: now,
	}, user.IdentityLoginOptions{AllowRegistration: config.AllowRegistration})
	if err != nil {
		code := loginErrorCode(err)
		c.JSON(loginErrorStatus(err), gin.H{"error": code, "redirect_url": loginErrorURL(code, challenge.ReturnTo)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"redirect_url": user.SafeReturnTo(challenge.ReturnTo, "/")})
}

func (p *Plugin) handleJavaScript(c *gin.Context) {
	p.serveAsset(c, "static/signin.js", "text/javascript; charset=utf-8")
}
func (p *Plugin) handleStylesheet(c *gin.Context) {
	p.serveAsset(c, "static/signin.css", "text/css; charset=utf-8")
}
func (p *Plugin) handleLogo(c *gin.Context) {
	p.serveAsset(c, "static/metamask-fox.svg", "image/svg+xml")
}

func (p *Plugin) serveAsset(c *gin.Context, name, contentType string) {
	data, err := publicFiles.ReadFile(name)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, contentType, data)
}

func (p *Plugin) prepareJSON(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/json; charset=utf-8")
}

func (p *Plugin) respondError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"error": code})
}

func decodeJSONBody(c *gin.Context, target interface{}) error {
	if c == nil || c.Request == nil || !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		return errors.New("JSON request required")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBody)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func requestOriginMatches(request *http.Request, config providerConfig) bool {
	if request == nil || !config.SiteURLValid {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	parsed, err := url.Parse(origin)
	return err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == "" &&
		strings.EqualFold(parsed.Scheme, config.Scheme) && strings.EqualFold(parsed.Host, config.Domain)
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

func loginErrorStatus(err error) int {
	if errors.Is(err, user.ErrRegistrationDisabled) || errors.Is(err, user.ErrExternalLoginDisabled) {
		return http.StatusForbidden
	}
	if errors.Is(err, user.ErrIdentityConflict) || errors.Is(err, user.ErrEmailAlreadyInUse) {
		return http.StatusConflict
	}
	return http.StatusUnauthorized
}

func loginErrorURL(code, returnTo string) string {
	query := url.Values{"error": {code}, "return_to": {user.SafeReturnTo(returnTo, "/")}}
	return "/login?" + query.Encode()
}

func (p *Plugin) redirectLoginError(c *gin.Context, code, returnTo string) {
	c.Redirect(http.StatusSeeOther, loginErrorURL(code, returnTo))
}
