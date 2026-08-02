// Package commerceusdt is a satellite crypto payment gateway for GoPress
// Commerce. It collects USDT (ERC-20) via a per-order HD-derived deposit address
// and confirms payment by watching the chain (pull-based), settling idempotently
// through the core/commerce contracts. It never imports the commerce engine, so
// there is no plugin→plugin dependency. EVM-abstracted: Ethereum ships first,
// BSC/Polygon are preset additions.
package commerceusdt

import (
	"embed"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	corecommerce "go-press/core/commerce"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/pkg/logger"
)

//go:embed locales/*.json locales/admin/*.json
var localeFS embed.FS

// appHost is the narrow slice of the engine this satellite needs. Beyond the
// paypal set it also needs the i18n manager (to localize the on-screen deposit
// instructions to the request language) and the DB (to persist invoices,
// deposits, and the scan cursor — a webhookless watcher has no other state).
type appHost interface {
	OptionsStore() *option.Store
	HookBus() *hook.Bus
	PublicSiteURL() string
	I18nManager() *coreI18n.Manager
	Database() *gorm.DB
}

type optionStore interface {
	Get(string) string
	GetDefault(string, string) string
	Set(string, string) error
}

// Plugin is the USDT satellite gateway.
type Plugin struct {
	hooks      *hook.Bus
	options    optionStore
	i18n       *coreI18n.Manager
	db         *gorm.DB
	siteURL    string
	httpClient *http.Client
	filters    []hook.Handle
	watchStop  chan struct{}
}

// New constructs the plugin with a bounded HTTP client for chain RPC calls.
func New() *Plugin {
	return &Plugin{httpClient: &http.Client{Timeout: 20 * time.Second}}
}

func (p *Plugin) Name() string          { return pluginMeta.Slug }
func (p *Plugin) Version() string       { return pluginMeta.Version }
func (p *Plugin) Description() string   { return pluginMeta.Description }
func (p *Plugin) DefaultInactive() bool { return pluginMeta.DefaultInactive }
func (p *Plugin) LogoSVG() string       { return adminCardLogoSVG }

// Activate wires the gateway, storage, storefront locales, and the chain watcher.
func (p *Plugin) Activate(app plugin.App) {
	host, ok := app.(appHost)
	if !ok || host.HookBus() == nil || host.OptionsStore() == nil || host.Database() == nil {
		logger.Error("commerce-usdt: required host capabilities unavailable")
		return
	}
	p.hooks = host.HookBus()
	p.options = host.OptionsStore()
	p.i18n = host.I18nManager()
	p.db = host.Database()
	p.siteURL = host.PublicSiteURL()

	if err := p.autoMigrate(); err != nil {
		logger.Error("commerce-usdt: migration failed", "error", err)
	}
	for _, name := range tableBaseNames() {
		core.RegisterPluginTable(tableSlug, name)
	}

	// Storefront locale bundle (namespaced commerce-usdt.*) for the deposit rows.
	if p.i18n != nil {
		p.i18n.LoadLocalesFS(localeFS, "locales")
	}

	// Register the gateway via the core contract (no commerce-engine import).
	p.filters = append(p.filters, corecommerce.RegisterPaymentGateway(p.hooks, usdtGateway{p: p}))

	// Own chain-watcher goroutine (pull-based confirmation). Not core's Scheduler,
	// whose tickers start at boot while this default-inactive module activates later.
	p.startWatcher()

	logger.Info("commerce-usdt activated", "version", p.Version(), "chain", p.loadConfig().ChainID)
}

// Deactivate removes the gateway registration and stops the watcher.
func (p *Plugin) Deactivate(app plugin.App) {
	if p.hooks != nil {
		for _, h := range p.filters {
			p.hooks.RemoveFilter(h)
		}
	}
	p.filters = nil
	p.stopWatcher()
	logger.Info("commerce-usdt deactivated")
}

// t translates a storefront message id to the request language.
func (p *Plugin) t(c *gin.Context, key string) string {
	if p.i18n == nil {
		return key
	}
	return p.i18n.Translate(c, key)
}

// SettingsTemplatePath implements plugin.SettingsProvider.
func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", pluginSlug, "templates", "admin", "settings.tmpl")
}

// SettingsData implements plugin.SettingsDataProvider.
func (p *Plugin) SettingsData() map[string]interface{} {
	c := p.loadConfig()
	return map[string]interface{}{
		"Enabled":       c.Enabled,
		"Chain":         c.ChainID,
		"Network":       c.Network,
		"RPCURL":        c.RPCURL,
		"TokenContract": c.TokenContract,
		"Xpub":          c.Xpub,
		"Confirmations": c.Confirmations,
		"WindowMinutes": c.WindowMinutes,
		"USDRate":       formatRate(c.RateScaled),
		"Chains":        presetIDs(),
		"Ready":         c.ready(),
	}
}

// Compile-time contract checks.
var (
	_ plugin.Plugin                  = (*Plugin)(nil)
	_ plugin.DefaultInactiveProvider = (*Plugin)(nil)
	_ plugin.SettingsProvider        = (*Plugin)(nil)
	_ plugin.SettingsDataProvider    = (*Plugin)(nil)
	_ plugin.LogoProvider            = (*Plugin)(nil)
)
