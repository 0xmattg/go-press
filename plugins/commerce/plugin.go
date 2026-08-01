package commerce

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/admin"
	corecommerce "go-press/core/commerce"
	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/plugin"
	"go-press/core/user"
	"go-press/pkg/logger"
)

// Plugin is the GoPress Commerce engine — the opt-in e-commerce foundation
// (products, cart, checkout, orders) plus the registries satellite payment /
// shipping plugins plug into. It ships disabled by default (DefaultInactive).
type Plugin struct {
	engine        *core.Engine
	repo          *Repository
	actions       []hook.Handle
	filters       []hook.Handle
	grants        []user.CapabilityGrant
	sweepStop     chan struct{}    // stops the stale-reservation sweeper on Deactivate
	reconcileStop chan struct{}    // stops pull-based payment reconciliation on Deactivate
	trackThrottle *attemptThrottle // rate-limits the guest /order-tracking lookup
}

// New constructs the commerce plugin.
func New() *Plugin {
	return &Plugin{trackThrottle: newAttemptThrottle(10, 5*time.Minute)}
}

// Name/Version are single-sourced from plugin.toml.
func (p *Plugin) Name() string    { return pluginMeta.Slug }
func (p *Plugin) Version() string { return pluginMeta.Version }

func (p *Plugin) Description() string { return pluginMeta.Description }

// DefaultInactive marks commerce as an opt-in module: registered but disabled
// until the operator enables it (core/plugin.DefaultInactiveProvider).
func (p *Plugin) DefaultInactive() bool { return pluginMeta.DefaultInactive }

// Activate wires storage, content types, RBAC, admin nav, and the payment
// settler. Every registration is tracked for clean removal in Deactivate.
func (p *Plugin) Activate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		return
	}
	p.engine = e
	p.loadStorefrontLocales()

	// 1. Storage.
	p.repo = NewRepository(e.DB)
	if err := p.repo.AutoMigrate(); err != nil {
		logger.Error("commerce: table migration failed", "error", err)
	}
	for _, name := range tableBaseNames() {
		core.RegisterPluginTable(pluginSlug, name)
	}

	// 2. Content types: register now (initial theme activation may precede this
	// plugin's load) and again on every content.register_types fire so the
	// product type survives theme switches.
	registerProductTypes(e.Registry)
	p.actions = append(p.actions, e.Hooks.AddAction(hook.ContentRegisterTypes,
		func(_ context.Context, args ...interface{}) {
			if len(args) > 0 {
				if reg, ok := args[0].(*content.Registry); ok {
					registerProductTypes(reg)
				}
			}
		}, 10))

	// 3. RBAC: grant commerce capabilities to the existing editor role
	// (superadmin already has them via the *.* wildcard).
	for _, capability := range [][2]string{
		{"shop_order", "read"}, {"shop_order", "update"}, {"shop_order", "refund"},
		{"coupon", "create"}, {"coupon", "read"}, {"coupon", "update"}, {"coupon", "delete"},
		{"commerce_settings", "read"}, {"commerce_settings", "update"},
	} {
		p.grants = append(p.grants, e.RBAC.GrantCapability(user.RoleEditor, capability[0], capability[1]))
	}

	// 4. Admin nav: contribute the Commerce module section.
	p.filters = append(p.filters, e.Hooks.AddFilter(hook.AdminNavItems, p.contributeNav, 10))

	// 4b. Product admin fields: inject the commerce meta box into the product
	// edit form and persist it to product_data + product_lookup on save.
	p.filters = append(p.filters, e.Hooks.AddFilter(hook.AdminContentFormFields, p.renderMetaBox, 50))
	p.actions = append(p.actions, e.Hooks.AddAction(hook.AdminContentSaved, p.saveFields, 50))

	// 5. Payment settler: the idempotent order-state-machine settlement all
	// gateways funnel through (offline mark-paid, PayPal webhook, crypto poll).
	p.filters = append(p.filters, corecommerce.SetSettler(e.Hooks, orderSettler{p: p}))

	// 5b. Built-in offline bank-transfer gateway — zero external deps, so a fresh
	// store closes the order loop immediately (buyer sees bank details, admin
	// marks paid). Satellite gateways (PayPal) register themselves the same way.
	p.filters = append(p.filters, corecommerce.RegisterPaymentGateway(e.Hooks, offlineGateway{p: p}))

	// 5c. Confirmation email + stale-reservation release: order side effects that
	// listen on status changes / run on a schedule.
	p.actions = append(p.actions, e.Hooks.AddAction(hookOrderStatusChanged, p.onOrderStatusChanged, 10))
	p.registerReservationSweeper()
	p.registerPaymentReconciler()

	// 5d. Seed bridge: after a demo/seed import, build product_data from the
	// _commerce_* meta on product content (so seeded demo products get prices).
	// Also run once now, so enabling Commerce AFTER a demo import still prices the
	// already-seeded products. Existing product_data rows are never overwritten:
	// activation and repeated demo imports must not reset live price or stock.
	p.actions = append(p.actions, e.Hooks.AddAction(hook.SeedCompleted,
		func(_ context.Context, _ ...interface{}) { p.syncProductsFromSeed() }, 10))
	p.syncProductsFromSeed()

	// 6. Storefront: public cart routes + add-to-cart / mini-cart render slots.
	// routes.register fires on every router (re)build, so cart routes appear
	// with the module and survive theme/plugin toggles.
	p.actions = append(p.actions, e.Hooks.AddAction("middleware.early", func(_ context.Context, args ...interface{}) {
		if len(args) > 0 {
			if r, ok := args[0].(*gin.Engine); ok {
				r.Use(commerceCachePolicy())
			}
		}
	}, 10))
	p.actions = append(p.actions, e.Hooks.AddAction("routes.register", func(_ context.Context, args ...interface{}) {
		if len(args) > 0 {
			if r, ok := args[0].(*gin.Engine); ok {
				p.registerStorefrontRoutes(r)
				p.registerOrderAdminRoutes(r)
			}
		}
	}, 10))
	p.filters = append(p.filters, e.Hooks.AddFilter(hookAddToCart, p.renderAddToCart, 10))
	p.filters = append(p.filters, e.Hooks.AddFilter(hook.ThemeHeaderNavAfter, p.renderMiniCart, 10))

	logger.Info("commerce activated", "version", p.Version())
}

// Deactivate removes every runtime registration so the module leaves no trace.
// Content types are dropped when the engine rebuilds the registry/router after
// deactivation (RefreshActiveTheme).
func (p *Plugin) Deactivate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		return
	}
	for _, h := range p.actions {
		e.Hooks.RemoveAction(h)
	}
	for _, h := range p.filters {
		e.Hooks.RemoveFilter(h)
	}
	for _, g := range p.grants {
		e.RBAC.RevokeCapabilityGrant(g)
	}
	if p.sweepStop != nil {
		close(p.sweepStop)
		p.sweepStop = nil
	}
	if p.reconcileStop != nil {
		close(p.reconcileStop)
		p.reconcileStop = nil
	}
	p.actions, p.filters, p.grants = nil, nil, nil
	logger.Info("commerce deactivated")
}

// contributeNav appends the Commerce admin nav section (admin.nav.items filter).
// Products appear under the Content section automatically (a content type); this
// section holds the non-content admin pages (settings now; orders/coupons in
// later phases).
func (p *Plugin) contributeNav(value interface{}, args ...interface{}) interface{} {
	items, ok := value.([]admin.AdminMenuItem)
	if !ok {
		return value
	}
	lang := p.adminLanguage()
	if len(args) > 1 {
		if supplied, ok := args[1].(string); ok && supplied != "" {
			lang = supplied
		}
	}
	items = append(items,
		admin.AdminMenuItem{Section: p.adminT(lang, "plugin.commerce.nav.section")},
		admin.AdminMenuItem{
			Label:    p.adminT(lang, "plugin.commerce.nav.orders"),
			URL:      "/admin/commerce/orders",
			Active:   "plugin-commerce-orders",
			Icon:     commerceNavIcon,
			Resource: "shop_order",
			Action:   "read",
		},
		admin.AdminMenuItem{
			Label:    p.adminT(lang, "plugin.commerce.nav.settings"),
			URL:      "/admin/plugins/commerce/settings",
			Active:   "plugin-commerce-settings",
			Icon:     commerceNavIcon,
			Resource: "commerce_settings",
			Action:   "read",
		},
	)
	return items
}

// commerceNavIcon is the inline SVG for the Commerce nav section.
const commerceNavIcon = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 2 3 6v13a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4z"/><path d="M3 6h18"/><path d="M16 10a4 4 0 0 1-8 0"/></svg>`

// Compile-time contract checks.
var (
	_ plugin.Plugin                        = (*Plugin)(nil)
	_ plugin.DefaultInactiveProvider       = (*Plugin)(nil)
	_ plugin.SettingsProvider              = (*Plugin)(nil)
	_ plugin.SettingsDataProvider          = (*Plugin)(nil)
	_ plugin.SettingsAuthorizationProvider = (*Plugin)(nil)
	_ plugin.LogoProvider                  = (*Plugin)(nil)
)
