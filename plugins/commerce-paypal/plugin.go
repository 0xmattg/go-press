// Package commercepaypal is a satellite payment gateway for GoPress Commerce.
// It implements PayPal (Orders v2) and registers itself purely through the
// core/commerce contracts on the hook bus — it never imports the commerce
// engine, so there is no plugin→plugin dependency and registration order is
// irrelevant (commerce reads gateways lazily at checkout).
package commercepaypal

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	corecommerce "go-press/core/commerce"
	"go-press/core/hook"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/pkg/logger"
)

// appHost is the narrow slice of the engine this satellite needs: the hook bus
// (to register the gateway + routes), the options store (credentials), and the
// public site URL (to build return/webhook URLs).
type appHost interface {
	OptionsStore() *option.Store
	HookBus() *hook.Bus
	PublicSiteURL() string
}

type optionStore interface {
	Get(string) string
	GetDefault(string, string) string
	Set(string, string) error
}

// Plugin is the PayPal satellite gateway.
type Plugin struct {
	hooks       *hook.Bus
	options     optionStore
	siteURL     string
	httpClient  *http.Client
	filters     []hook.Handle
	actions     []hook.Handle
	secretCache string
}

// New constructs the plugin with a bounded HTTP client for PayPal REST calls.
func New() *Plugin {
	return &Plugin{httpClient: &http.Client{Timeout: 20 * time.Second}}
}

func (p *Plugin) Name() string          { return pluginMeta.Slug }
func (p *Plugin) Version() string       { return pluginMeta.Version }
func (p *Plugin) Description() string   { return pluginMeta.Description }
func (p *Plugin) DefaultInactive() bool { return pluginMeta.DefaultInactive }
func (p *Plugin) LogoSVG() string       { return adminCardLogoSVG }
func (p *Plugin) siteBase() string      { return strings.TrimRight(p.siteURL, "/") }

// Activate registers the PayPal gateway and its webhook/return routes.
func (p *Plugin) Activate(app plugin.App) {
	host, ok := app.(appHost)
	if !ok || host.HookBus() == nil || host.OptionsStore() == nil {
		logger.Error("commerce-paypal: required host capabilities unavailable")
		return
	}
	p.hooks = host.HookBus()
	p.options = host.OptionsStore()
	p.siteURL = host.PublicSiteURL()
	p.secretCache = strings.TrimSpace(p.options.Get(optSecret))

	// Register the gateway via the core contract (no commerce-engine import).
	p.filters = append(p.filters, corecommerce.RegisterPaymentGateway(p.hooks, payPalGateway{p: p}))

	// Webhook + buyer-return routes. routes.register fires on every router
	// (re)build, so the routes appear/disappear with the module.
	p.actions = append(p.actions, p.hooks.AddAction("routes.register", func(_ context.Context, args ...interface{}) {
		if len(args) > 0 {
			if r, ok := args[0].(*gin.Engine); ok {
				p.registerRoutes(r)
			}
		}
	}, 10))

	logger.Info("commerce-paypal activated", "version", p.Version(), "sandbox", p.loadConfig().Sandbox)
}

// Deactivate removes the gateway registration and route handlers.
func (p *Plugin) Deactivate(app plugin.App) {
	if p.hooks == nil {
		return
	}
	for _, h := range p.filters {
		p.hooks.RemoveFilter(h)
	}
	for _, h := range p.actions {
		p.hooks.RemoveAction(h)
	}
	p.filters, p.actions = nil, nil
	logger.Info("commerce-paypal deactivated")
}

// SettingsTemplatePath implements plugin.SettingsProvider.
func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", pluginSlug, "templates", "admin", "settings.tmpl")
}

// SettingsData implements plugin.SettingsDataProvider.
func (p *Plugin) SettingsData() map[string]interface{} {
	c := p.loadConfig()
	return map[string]interface{}{
		"Enabled":          c.Enabled,
		"Sandbox":          c.Sandbox,
		"ClientID":         c.ClientID,
		"SecretConfigured": c.Secret != "",
		"WebhookID":        c.WebhookID,
		"WebhookURL":       p.siteBase() + webhookPath,
		"Ready":            c.ready(),
	}
}

// OnSettingsSave implements plugin.SettingsSaveProvider. The secret is a password
// field that renders empty; preserve the stored value when left blank.
func (p *Plugin) OnSettingsSave(settings map[string]string) {
	if p.options == nil {
		return
	}
	secret := strings.TrimSpace(settings[optSecret])
	if secret == "" && p.secretCache != "" {
		_ = p.options.Set(optSecret, p.secretCache)
	} else if secret != "" {
		p.secretCache = secret
	}
}

// Compile-time contract checks.
var (
	_ plugin.Plugin                  = (*Plugin)(nil)
	_ plugin.DefaultInactiveProvider = (*Plugin)(nil)
	_ plugin.SettingsProvider        = (*Plugin)(nil)
	_ plugin.SettingsDataProvider    = (*Plugin)(nil)
	_ plugin.SettingsSaveProvider    = (*Plugin)(nil)
	_ plugin.LogoProvider            = (*Plugin)(nil)
)
