package commerce

import "path/filepath"

// SettingsPermissionResource declares the narrow RBAC resource used by the
// generic plugin settings handlers. Core supplies the read/update action and
// remains unaware of the Commerce plugin slug.
func (p *Plugin) SettingsPermissionResource() string { return "commerce_settings" }

// SettingsTemplatePath implements plugin.SettingsProvider: the admin settings
// page reachable at /admin/plugins/commerce/settings.
func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", pluginSlug, "templates", "admin", "settings.tmpl")
}

// SettingsData implements plugin.SettingsDataProvider: current store settings
// for the settings template. Fields persist as plugin_commerce_* options via the
// standard plugin settings-save pipeline.
func (p *Plugin) SettingsData() map[string]interface{} {
	return map[string]interface{}{
		"StoreCurrency":        p.opt("plugin_commerce_store_currency", "USD"),
		"StoreCountry":         p.opt("plugin_commerce_store_country", ""),
		"WeightUnit":           p.opt("plugin_commerce_weight_unit", "g"),
		"FlatShipping":         p.opt("plugin_commerce_flat_shipping", ""),
		"ReservationTTL":       p.opt("plugin_commerce_reservation_ttl_minutes", "60"),
		"OfflineAccountName":   p.opt("plugin_commerce_offline_account_name", ""),
		"OfflineBankName":      p.opt("plugin_commerce_offline_bank_name", ""),
		"OfflineAccountNumber": p.opt("plugin_commerce_offline_account_number", ""),
		"OfflineInstructions":  p.opt("plugin_commerce_offline_instructions", ""),
	}
}

func (p *Plugin) opt(key, def string) string {
	if p.engine == nil || p.engine.Options == nil {
		return def
	}
	if v := p.engine.Options.Get(key); v != "" {
		return v
	}
	return def
}
