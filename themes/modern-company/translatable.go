package moderncompany

import "go-press/core/option"

// registerTranslatableOptions declares all text-based theme settings that need translation.
// Called during Setup() so Core and multilang plugin can route translations.
func registerTranslatableOptions() {
	// Company info
	option.RegisterTranslatable("company_address", "company", "公司地址")
	option.RegisterTranslatable("company_description", "company", "公司简介")
	option.RegisterTranslatable("footer_copyright_text", "company", "页脚版权文案")

	// Brand
	option.RegisterTranslatable("home_logo_text", "brand", "Logo 品牌名")

	// Hero slides (x6)
	for _, n := range []string{"1", "2", "3", "4", "5", "6"} {
		option.RegisterTranslatable("home_hero_"+n+"_label", "hero", "轮播"+n+" 标签")
		option.RegisterTranslatable("home_hero_"+n+"_title", "hero", "轮播"+n+" 标题")
		option.RegisterTranslatable("home_hero_"+n+"_desc", "hero", "轮播"+n+" 描述")
		option.RegisterTranslatable("home_hero_"+n+"_btn1_text", "hero", "轮播"+n+" 按钮1")
		option.RegisterTranslatable("home_hero_"+n+"_btn2_text", "hero", "轮播"+n+" 按钮2")
	}

	// About section
	option.RegisterTranslatable("home_about_label", "about", "区域标签")
	option.RegisterTranslatable("home_about_title", "about", "标题")
	option.RegisterTranslatable("home_about_desc", "about", "描述")
	option.RegisterTranslatable("home_about_badge_text", "about", "徽章文字")
	option.RegisterTranslatable("home_about_feature_1", "about", "亮点 1")
	option.RegisterTranslatable("home_about_feature_2", "about", "亮点 2")
	option.RegisterTranslatable("home_about_feature_3", "about", "亮点 3")
	option.RegisterTranslatable("home_about_btn_text", "about", "按钮文字")

	// Stats (x4)
	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("home_stat_"+n+"_label", "stats", "统计"+n+" 标签")
	}

	// Products section
	option.RegisterTranslatable("home_products_label", "products", "区域标签")
	option.RegisterTranslatable("home_products_title", "products", "标题")
	option.RegisterTranslatable("home_products_subtitle", "products", "副标题")

	// Services section
	option.RegisterTranslatable("home_services_label", "services", "区域标签")
	option.RegisterTranslatable("home_services_title", "services", "标题")
	option.RegisterTranslatable("home_services_subtitle", "services", "副标题")

	// Partners section
	option.RegisterTranslatable("home_partners_label", "partners", "区域标签")
	option.RegisterTranslatable("home_partners_title", "partners", "标题")
	option.RegisterTranslatable("home_partners_subtitle", "partners", "副标题")

	// CTA section
	option.RegisterTranslatable("home_cta_label", "cta", "区域标签")
	option.RegisterTranslatable("home_cta_title", "cta", "标题")
	option.RegisterTranslatable("home_cta_desc", "cta", "描述")
	option.RegisterTranslatable("home_cta_btn1_text", "cta", "按钮1 文字")
	option.RegisterTranslatable("home_cta_btn2_text", "cta", "按钮2 文字")

	// ===== About Page =====
	// Banner
	option.RegisterTranslatable("about_banner_title", "about_page", "横幅标题")
	option.RegisterTranslatable("about_banner_subtitle", "about_page", "横幅副标题")

	// Intro (Who We Are)
	option.RegisterTranslatable("about_intro_label", "about_page", "简介标签")
	option.RegisterTranslatable("about_intro_title", "about_page", "简介标题")
	option.RegisterTranslatable("about_intro_badge_text", "about_page", "徽章文字")
	option.RegisterTranslatable("about_intro_p1", "about_page", "简介第一段")
	option.RegisterTranslatable("about_intro_p2", "about_page", "简介第二段")
	option.RegisterTranslatable("about_intro_btn_text", "about_page", "按钮文字")

	// Stats (x4)
	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("about_stat_"+n+"_label", "about_page", "统计"+n+" 标签")
	}

	// Principles (x5)
	option.RegisterTranslatable("about_principles_label", "about_page", "原则区域标签")
	option.RegisterTranslatable("about_principles_title", "about_page", "原则区域标题")
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		option.RegisterTranslatable("about_principle_"+n+"_title", "about_page", "原则"+n+" 标题")
		option.RegisterTranslatable("about_principle_"+n+"_desc", "about_page", "原则"+n+" 描述")
	}

	// Expertise (x4)
	option.RegisterTranslatable("about_exp_label", "about_page", "经验区域标签")
	option.RegisterTranslatable("about_exp_title", "about_page", "经验区域标题")
	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("about_exp_"+n+"_title", "about_page", "经验"+n+" 标题")
		option.RegisterTranslatable("about_exp_"+n+"_desc", "about_page", "经验"+n+" 描述")
	}

	// CTA
	option.RegisterTranslatable("about_cta_title", "about_page", "CTA 标题")
	option.RegisterTranslatable("about_cta_desc", "about_page", "CTA 描述")
	option.RegisterTranslatable("about_cta_btn_text", "about_page", "CTA 按钮文字")
}
