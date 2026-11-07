package commerce

// SettingsPermissionResource declares the narrow RBAC resource used by the
// generic plugin settings handlers. Core supplies the read/update action and
// remains unaware of the Commerce plugin slug.
func (p *Plugin) SettingsPermissionResource() string { return "commerce_settings" }

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
		"OfflineAccountName":   p.opt(optOfflineAccountName, ""),
		"OfflineBankName":      p.opt(optOfflineBankName, ""),
		"OfflineAccountNumber": p.opt(optOfflineAccountNumber, ""),
		"OfflineInstructions":  p.opt(optOfflineInstructions, ""),
		// OfflineReady mirrors offlineGateway.Available so the admin sees
		// whether bank transfer is currently offered at checkout.
		"OfflineReady": p.offlineConfigured(),
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
