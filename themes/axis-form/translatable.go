package axisform

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
		"nav_contact",
	} {
		option.RegisterTranslatable(key, "brand", key)
	}

	for _, key := range []string{
		"home_hero_title",
		"home_hero_subtitle",
		"home_hero_btn_text",
		"home_about_title",
		"home_about_desc",
		"home_services_title",
		"home_showcase_title",
		"home_showcase_subtitle",
		"home_showcase_btn",
	} {
		option.RegisterTranslatable(key, "home", key)
	}

	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("home_stat_"+n+"_label", "home", "home_stat_"+n+"_label")
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
