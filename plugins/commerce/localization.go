package commerce

import (
	"embed"

	"github.com/gin-gonic/gin"

	coreI18n "github.com/0xmattg/go-press/core/i18n"
)

// commerceLocaleFS keeps the plugin's storefront copy with the plugin. Message
// IDs are namespaced with commerce. so they can safely share Core's bundle with
// the active theme and other extensions.
//
//go:embed locales/*.json locales/admin/*.json
var commerceLocaleFS embed.FS

var commerceAdminCatalog = coreI18n.NewCatalog(
	coreI18n.DefaultUILanguage,
	coreI18n.LoadFlatMessages(commerceLocaleFS, "locales/admin"),
)

func (p *Plugin) loadStorefrontLocales() {
	if p == nil || p.engine == nil || p.engine.I18n == nil {
		return
	}
	p.engine.I18n.LoadLocalesFS(commerceLocaleFS, "locales")
}

func (p *Plugin) t(c *gin.Context, key string) string {
	if p == nil || p.engine == nil || p.engine.I18n == nil {
		return key
	}
	return p.engine.I18n.Translate(c, key)
}

func (p *Plugin) adminLanguage() string {
	if p != nil && p.engine != nil && p.engine.Options != nil {
		return coreI18n.NormalizeLanguage(
			p.engine.Options.GetDefault("admin_language", coreI18n.DefaultUILanguage),
			coreI18n.DefaultUILanguage,
		)
	}
	return coreI18n.DefaultUILanguage
}

func (p *Plugin) adminT(lang, key string, args ...interface{}) string {
	return commerceAdminCatalog.T(lang, key, args...)
}

func (p *Plugin) adminOrderStatusLabel(lang, status string) string {
	key := map[string]string{
		OrderPending:           "plugin.commerce.status.pending",
		OrderOnHold:            "plugin.commerce.status.on_hold",
		OrderProcessing:        "plugin.commerce.status.processing",
		OrderCompleted:         "plugin.commerce.status.completed",
		OrderCancelled:         "plugin.commerce.status.cancelled",
		OrderFailed:            "plugin.commerce.status.failed",
		OrderRefunded:          "plugin.commerce.status.refunded",
		OrderPartiallyRefunded: "plugin.commerce.status.partially_refunded",
		OrderReconciliation:    "plugin.commerce.status.reconciliation",
	}[status]
	if key == "" {
		return status
	}
	return p.adminT(lang, key)
}
