package civicestate

import "go-press/core/option"

// registerTranslatableOptions declares text-based theme settings that can be
// translated by the multilingual plugin. Image URLs and numeric limits are
// intentionally omitted.
func registerTranslatableOptions() {
	for _, key := range []string{
		"site_name",
		"site_description",
		"company_name",
		"company_address",
		"company_description",
		"footer_text",
		"home_logo_text",
		"nav_about",
		"nav_services",
		"nav_showcase",
		"nav_process",
		"nav_blog",
		"nav_contact",
	} {
		option.RegisterTranslatable(key, "brand", key)
	}

	for _, key := range []string{
		"home_hero_title",
		"home_hero_subtitle",
		"home_hero_btn_text",
		"home_search_placeholder",
		"home_search_button",
		"home_filter_1",
		"home_filter_2",
		"home_filter_3",
		"home_filter_4",
		"home_products_title",
		"home_nearby_title",
		"home_nearby_desc",
		"home_nearby_metric",
		"home_nearby_label",
		"home_nearby_btn",
	} {
		option.RegisterTranslatable(key, "home", key)
	}

	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("about_stat_"+n+"_label", "about", "about_stat_"+n+"_label")
	}

	for _, key := range []string{
		"about_banner_title",
		"about_banner_subtitle",
		"about_intro_label",
		"about_intro_title",
		"about_intro_p1",
		"about_intro_p2",
		"about_intro_btn_text",
		"contact_title",
		"contact_subtitle",
	} {
		option.RegisterTranslatable(key, "pages", key)
	}
}
