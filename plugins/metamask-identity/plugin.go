// Package metamaskidentity provides Sign-In with Ethereum for GoPress.
// Wallet protocol verification stays in this plugin; core receives only a
// verified, provider-neutral identity assertion.
package metamaskidentity

import (
	"context"
	"embed"
	"html/template"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	"go-press/core/hook"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/core/user"
	"go-press/pkg/logger"
)

const (
	PluginName    = "metamask-identity"
	pluginVersion = "1.0.1"
	storageSlug   = "metamask_identity"
	providerID    = "ethereum"

	startPath     = "/auth/metamask/start"
	challengePath = "/auth/metamask/challenge"
	verifyPath    = "/auth/metamask/verify"
	assetBasePath = "/auth/metamask/assets/"

	optEnabled      = "plugin_metamask-identity_enabled"
	optChainID      = "plugin_metamask-identity_chain_id"
	optAutoRegister = "plugin_metamask-identity_auto_register"
)

//go:embed templates/public/signin.tmpl static/*
var publicFiles embed.FS

type appHost interface {
	plugin.PublicAuthHost
	Database() *gorm.DB
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
}

type Plugin struct {
	host        appHost
	auth        authService
	options     optionStore
	repo        challengeStore
	siteURL     string
	page        *template.Template
	limiter     *requestLimiter
	hookHandles []hook.Handle
	active      atomic.Bool
	now         func() time.Time
}

func New() *Plugin {
	return &Plugin{
		page:    template.Must(template.ParseFS(publicFiles, "templates/public/signin.tmpl")),
		limiter: newRequestLimiter(),
		now:     time.Now,
	}
}

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return pluginVersion }
func (p *Plugin) Description() string {
	return "MetaMask 与 EIP-4361 Sign-In with Ethereum 登录和注册。"
}

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", PluginName, "templates", "admin", "settings.tmpl")
}

func (p *Plugin) SettingsData() map[string]interface{} {
	config := p.loadConfig()
	return map[string]interface{}{
		"MetaMaskEnabled":      config.Enabled,
		"MetaMaskChainID":      config.ChainID,
		"MetaMaskChainIDValid": config.ChainIDValid,
		"MetaMaskChainIDHex":   config.ChainIDHex,
		"MetaMaskAutoRegister": config.AllowRegistration,
		"MetaMaskOrigin":       config.Origin,
		"MetaMaskDomain":       config.Domain,
		"MetaMaskReady":        config.ready(),
	}
}

func (p *Plugin) OnSettingsSave(_ map[string]string) { p.syncProvider() }

func (p *Plugin) Activate(app plugin.App) {
	host, ok := app.(appHost)
	if !ok || host.PublicAuthenticator() == nil || host.Database() == nil || host.OptionsStore() == nil || host.HookBus() == nil {
		logger.Error("metamask-identity: required public auth capabilities are unavailable")
		return
	}
	p.host = host
	p.auth = host.PublicAuthenticator()
	p.options = host.OptionsStore()
	p.siteURL = host.PublicSiteURL()
	repo := newChallengeRepository(host.Database())
	if err := repo.AutoMigrate(); err != nil {
		logger.Error("metamask-identity: challenge table migration failed", "error", err)
		return
	}
	p.repo = repo
	core.RegisterPluginTable(storageSlug, "challenges")
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
		router.POST(challengePath, p.handleChallenge)
		router.POST(verifyPath, p.handleVerify)
		router.GET(assetBasePath+"signin.js", p.handleJavaScript)
		router.GET(assetBasePath+"signin.css", p.handleStylesheet)
		router.GET(assetBasePath+"metamask-fox.svg", p.handleLogo)
	}, 20))
	logger.Info("metamask-identity plugin activated", "configured", p.loadConfig().ready())
}

func (p *Plugin) Deactivate(_ plugin.App) {
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
	logger.Info("metamask-identity plugin deactivated")
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
		ID: providerID, Label: "MetaMask", BeginURL: startPath,
		IconURL: assetBasePath + "metamask-fox.svg", Priority: 20,
	}); err != nil {
		logger.Error("metamask-identity: provider registration failed", "error", err)
	}
}

func (p *Plugin) nowUTC() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func (p *Plugin) available(config providerConfig) bool {
	return p != nil && p.active.Load() && p.auth != nil && p.auth.ExternalLoginEnabled() && p.repo != nil && config.ready()
}

var _ plugin.Plugin = (*Plugin)(nil)
var _ plugin.SettingsProvider = (*Plugin)(nil)
var _ plugin.SettingsDataProvider = (*Plugin)(nil)
var _ plugin.SettingsSaveProvider = (*Plugin)(nil)

func trimmed(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
