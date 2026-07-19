package user

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	CtxKeyPublicUser    = "gopress_public_user"
	CtxKeyPublicSession = "gopress_public_session"
)

//go:embed templates/login.tmpl
var publicAuthTemplates embed.FS

type PublicAuth struct {
	broker        *IdentityBroker
	sessions      *SessionManager
	providers     *ProviderRegistry
	policy        *RegistrationPolicy
	secureCookies bool
	siteName      func() string
	template      *template.Template
}

// PublicUserView is the template-safe account projection. Credential hashes,
// metadata and administrative timestamps never cross the template boundary.
type PublicUserView struct {
	ID          uint
	Username    string
	Email       string
	DisplayName string
	AvatarURL   string
	Role        string
}

func NewPublicAuth(broker *IdentityBroker, sessions *SessionManager, providers *ProviderRegistry, policy *RegistrationPolicy, secureCookies bool, siteName func() string) *PublicAuth {
	tmpl := template.Must(template.ParseFS(publicAuthTemplates, "templates/login.tmpl"))
	return &PublicAuth{
		broker: broker, sessions: sessions, providers: providers, policy: policy,
		secureCookies: secureCookies, siteName: siteName, template: tmpl,
	}
}

func (a *PublicAuth) Providers() *ProviderRegistry { return a.providers }

func (a *PublicAuth) ExternalLoginEnabled() bool {
	return a != nil && a.policy != nil && a.policy.ExternalLoginEnabled()
}

// LinkVerifiedIdentity binds to the authenticated context user only. Provider
// routes never accept a user ID from URL or form input for this operation.
func (a *PublicAuth) LinkVerifiedIdentity(c *gin.Context, verified VerifiedIdentity) (*UserIdentity, error) {
	if a == nil || a.broker == nil || c == nil {
		return nil, ErrAuthenticationRequired
	}
	account := CurrentUser(c)
	if account == nil {
		return nil, ErrAuthenticationRequired
	}
	return a.broker.LinkIdentity(c.Request.Context(), account.ID, verified)
}

// UnlinkIdentity removes only an identity owned by the authenticated user.
func (a *PublicAuth) UnlinkIdentity(c *gin.Context, identityID uint) error {
	if a == nil || a.broker == nil || c == nil {
		return ErrAuthenticationRequired
	}
	account := CurrentUser(c)
	if account == nil {
		return ErrAuthenticationRequired
	}
	return a.broker.UnlinkIdentity(c.Request.Context(), account.ID, identityID)
}

// LoginVerifiedIdentity is the provider-facing completion point. Protocol
// plugins verify their assertion, then core applies policy and creates session.
func (a *PublicAuth) LoginVerifiedIdentity(c *gin.Context, verified VerifiedIdentity) (*IdentityResult, error) {
	return a.LoginVerifiedIdentityWithOptions(c, verified, IdentityLoginOptions{AllowRegistration: true})
}

func (a *PublicAuth) LoginVerifiedIdentityWithOptions(c *gin.Context, verified VerifiedIdentity, options IdentityLoginOptions) (*IdentityResult, error) {
	if c == nil {
		return nil, ErrInvalidIdentity
	}
	if a == nil || a.broker == nil || a.sessions == nil {
		return nil, ErrExternalLoginDisabled
	}
	result, err := a.broker.LoginOrRegisterWithOptions(c.Request.Context(), verified, options)
	if err != nil {
		return nil, err
	}
	identityID := result.Identity.ID
	token, err := a.sessions.Create(c.Request.Context(), result.User.ID, SessionMetadata{
		IdentityID: &identityID,
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
	if err != nil {
		return nil, err
	}
	a.setSessionCookie(c, token)
	return result, nil
}

func (a *PublicAuth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil || a.sessions == nil {
			c.Next()
			return
		}
		token, err := c.Cookie(PublicSessionCookie)
		if err != nil || token == "" {
			c.Next()
			return
		}
		account, session, err := a.sessions.Authenticate(c.Request.Context(), token)
		if err != nil {
			a.clearSessionCookie(c)
			c.Next()
			return
		}
		c.Set(CtxKeyPublicUser, account)
		c.Set(CtxKeyPublicSession, session)
		c.Next()
	}
}

func (a *PublicAuth) RegisterRoutes(r *gin.Engine) {
	r.GET("/login", a.loginPage)
	r.POST("/logout", a.logout)
}

func (a *PublicAuth) loginPage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	returnTo := SafeReturnTo(c.Query("return_to"), "/")
	providers := []providerView{}
	if a.policy != nil && a.policy.ExternalLoginEnabled() && a.providers != nil {
		for _, provider := range a.providers.All() {
			providers = append(providers, providerView{
				Label:   provider.Label,
				URL:     appendReturnTo(provider.BeginURL, returnTo),
				IconURL: provider.IconURL,
			})
		}
	}
	name := "GoPress"
	if a.siteName != nil && strings.TrimSpace(a.siteName()) != "" {
		name = strings.TrimSpace(a.siteName())
	}
	data := struct {
		SiteName  string
		ReturnTo  string
		Providers []providerView
		User      *PublicUserView
		Error     string
	}{SiteName: name, ReturnTo: returnTo, Providers: providers, User: CurrentUserView(c), Error: publicLoginError(c.Query("error"))}
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = a.template.ExecuteTemplate(c.Writer, "login", data)
}

func (a *PublicAuth) logout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !sameOriginRequest(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if token, err := c.Cookie(PublicSessionCookie); err == nil && token != "" && a.sessions != nil {
		err = a.sessions.Revoke(c.Request.Context(), token)
		if err != nil && !errors.Is(err, ErrSessionNotFound) {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	}
	a.clearSessionCookie(c)
	c.Redirect(http.StatusSeeOther, SafeReturnTo(c.PostForm("return_to"), "/"))
}

func (a *PublicAuth) setSessionCookie(c *gin.Context, token *SessionToken) {
	maxAge := int(time.Until(token.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: PublicSessionCookie, Value: token.Token, Path: "/", MaxAge: maxAge,
		Expires: token.ExpiresAt, HttpOnly: true, Secure: a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *PublicAuth) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: PublicSessionCookie, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func CurrentUser(c *gin.Context) *User {
	if c == nil {
		return nil
	}
	account, _ := c.Get(CtxKeyPublicUser)
	user, _ := account.(*User)
	return user
}

func CurrentSession(c *gin.Context) *UserSession {
	if c == nil {
		return nil
	}
	value, _ := c.Get(CtxKeyPublicSession)
	session, _ := value.(*UserSession)
	return session
}

func CurrentUserView(c *gin.Context) *PublicUserView {
	account := CurrentUser(c)
	if account == nil {
		return nil
	}
	return &PublicUserView{
		ID: account.ID, Username: account.Username, Email: account.EmailValue(),
		DisplayName: account.DisplayName, AvatarURL: account.AvatarURL, Role: account.Role,
	}
}

func IsLoggedIn(c *gin.Context) bool { return CurrentUser(c) != nil }

func LoginURL(c *gin.Context) string {
	returnTo := "/"
	if c != nil && c.Request != nil {
		returnTo = SafeReturnTo(c.Request.URL.RequestURI(), "/")
	}
	return "/login?return_to=" + url.QueryEscape(returnTo)
}

func LogoutURL() string { return "/logout" }

func publicLoginError(code string) string {
	switch strings.TrimSpace(code) {
	case "authentication_failed":
		return "Sign-in could not be completed. Please try again."
	case "registration_disabled":
		return "This account is not registered and new registrations are currently disabled."
	case "identity_conflict":
		return "This identity cannot be used with the requested account."
	case "provider_unavailable":
		return "This sign-in method is temporarily unavailable."
	default:
		return ""
	}
}

// SafeReturnTo accepts only same-site absolute paths and rejects scheme-relative
// paths, auth endpoints and control characters.
func SafeReturnTo(raw, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" || !strings.HasPrefix(fallback, "/") || strings.HasPrefix(fallback, "//") {
		fallback = "/"
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return fallback
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return fallback
	}
	if parsed.Path == "/login" || parsed.Path == "/logout" || strings.HasPrefix(parsed.Path, "/auth/") {
		return fallback
	}
	return parsed.RequestURI()
}

type providerView struct {
	Label   string
	URL     string
	IconURL string
}

func appendReturnTo(beginURL, returnTo string) string {
	parsed, err := url.Parse(beginURL)
	if err != nil {
		return beginURL
	}
	query := parsed.Query()
	query.Set("return_to", SafeReturnTo(returnTo, "/"))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sameOriginRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		return err == nil && strings.EqualFold(parsed.Host, r.Host)
	}
	return true
}
