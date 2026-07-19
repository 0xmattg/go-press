// Package googleidentity provides Google OpenID Connect login for GoPress.
// Protocol verification stays in this plugin; core receives only a verified,
// provider-neutral identity assertion.
package googleidentity

import (
	"context"
	_ "embed"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"go-press/core/hook"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/core/user"
	"go-press/pkg/logger"
)

const (
	PluginName    = "google-identity"
	pluginVersion = "1.0.1"
	providerID    = "google"

	startPath    = "/auth/google/start"
	callbackPath = "/auth/google/callback"
	logoPath     = "/auth/google/assets/google-g-logo.png"

	optEnabled      = "plugin_google-identity_enabled"
	optClientID     = "plugin_google-identity_client_id"
	optClientSecret = "plugin_google-identity_client_secret"
	optHostedDomain = "plugin_google-identity_hosted_domain"
	optAutoRegister = "plugin_google-identity_auto_register"
)

//go:embed static/google-g-logo.png
var googleLogo []byte

type appHost interface {
	plugin.PublicAuthHost
	OptionsStore() *option.Store
	HookBus() *hook.Bus
}

type authService interface {
	Providers() *user.ProviderRegistry
	ExternalLoginEnabled() bool
	LoginVerifiedIdentityWithOptions(*gin.Context, user.VerifiedIdentity, user.IdentityLoginOptions) (*user.IdentityResult, error)
}

type optionStore interface {
	Get(string) string
	GetDefault(string, string) string
	Set(string, string) error
}

type Plugin struct {
	host        appHost
	auth        authService
	options     optionStore
	siteURL     string
	flow        oidcFlow
	hookHandles []hook.Handle
	secretCache string
	active      atomic.Bool
}

func New() *Plugin { return &Plugin{flow: newStandardOIDCFlow()} }

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return pluginVersion }
func (p *Plugin) Description() string {
	return "Google OpenID Connect 登录与注册，支持 PKCE、nonce 和 Workspace 域限制。"
}

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", PluginName, "templates", "admin", "settings.tmpl")
}

func (p *Plugin) SettingsData() map[string]interface{} {
	config := p.loadConfig()
	return map[string]interface{}{
		"GoogleEnabled":          config.Enabled,
		"GoogleClientID":         config.ClientID,
		"GoogleSecretConfigured": config.ClientSecret != "",
		"GoogleHostedDomain":     config.HostedDomain,
		"GoogleAutoRegister":     config.AllowRegistration,
		"GoogleCallbackURL":      config.RedirectURL,
		"GoogleReady":            config.ready(),
	}
}

func (p *Plugin) OnSettingsSave(settings map[string]string) {
	if p.options == nil {
		return
	}
	secret := strings.TrimSpace(settings[optClientSecret])
	if secret == "" && p.secretCache != "" {
		// Password fields intentionally render empty. Preserve the previous
		// credential when an administrator saves unrelated settings.
		_ = p.options.Set(optClientSecret, p.secretCache)
	} else if secret != "" {
		p.secretCache = secret
	}
	p.syncProvider()
}

func (p *Plugin) Activate(app plugin.App) {
	host, ok := app.(appHost)
	if !ok || host.PublicAuthenticator() == nil || host.OptionsStore() == nil || host.HookBus() == nil {
		logger.Error("google-identity: required public auth capabilities are unavailable")
		return
	}
	p.host = host
	p.auth = host.PublicAuthenticator()
	p.options = host.OptionsStore()
	p.siteURL = host.PublicSiteURL()
	p.secretCache = strings.TrimSpace(p.options.Get(optClientSecret))
	p.hookHandles = p.hookHandles[:0]
	p.active.Store(true)
	p.syncProvider()

	p.hookHandles = append(p.hookHandles, host.HookBus().AddAction("routes.register", func(_ context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		router, ok := args[0].(*gin.Engine)
		if !ok {
			return
		}
		router.GET(startPath, p.handleStart)
		router.GET(callbackPath, p.handleCallback)
		router.GET(logoPath, p.handleLogo)
	}, 20))
	logger.Info("google-identity plugin activated", "configured", p.loadConfig().ready())
}

func (p *Plugin) Deactivate(app plugin.App) {
	p.active.Store(false)
	if p.auth != nil && p.auth.Providers() != nil {
		p.auth.Providers().Unregister(providerID)
	}
	if p.host != nil {
		for _, handle := range p.hookHandles {
			p.host.HookBus().RemoveAction(handle)
		}
	}
	p.hookHandles = p.hookHandles[:0]
	logger.Info("google-identity plugin deactivated")
}

func (p *Plugin) syncProvider() {
	if p.auth == nil || p.auth.Providers() == nil {
		return
	}
	p.auth.Providers().Unregister(providerID)
	if !p.active.Load() || !p.loadConfig().ready() {
		return
	}
	if err := p.auth.Providers().Register(user.ProviderDescriptor{
		ID: providerID, Label: "Google", BeginURL: startPath,
		IconURL: logoPath + "?v=" + pluginVersion, Priority: 10,
	}); err != nil {
		logger.Error("google-identity: provider registration failed", "error", err)
	}
}
