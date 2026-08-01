package shopstarter

import "go-press/core/option"

// registerTranslatableOptions exposes every theme-owned visible text setting
// through Core's generic option registry.
func registerTranslatableOptions() {
	for _, item := range []struct {
		key   string
		group string
		label string
	}{
		{"home_announcement", "brand", "Announcement"},
		{"home_hero_eyebrow", "hero", "Hero eyebrow"},
		{"home_hero_title", "hero", "Hero title"},
		{"home_hero_description", "hero", "Hero description"},
		{"home_hero_primary_cta", "hero", "Hero primary button"},
		{"home_hero_secondary_cta", "hero", "Hero secondary button"},
		{"home_products_title", "products", "Product section title"},
		{"home_products_description", "products", "Product section description"},
		{"footer_tagline", "footer", "Footer tagline"},
	} {
		option.RegisterTranslatable(item.key, item.group, item.label)
	}
}
