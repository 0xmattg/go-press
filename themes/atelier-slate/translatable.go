package atelierslate

import "go-press/core/option"

// registerTranslatableOptions declares all text-based theme settings that need translation.
// Called during Setup() so Core and multilang plugin can route translations.
func registerTranslatableOptions() {
	// Company info
	option.RegisterTranslatable("company_name", "company", "公司名称")
	option.RegisterTranslatable("company_address", "company", "公司地址")
	option.RegisterTranslatable("company_description", "company", "公司简介")
	option.RegisterTranslatable("footer_text", "company", "页脚文案")

	// Brand
	option.RegisterTranslatable("home_logo_text", "brand", "Logo 品牌名")
	option.RegisterTranslatable("home_cta_text", "brand", "导航 CTA 按钮文字")

	// Hero slides (x3)
	for _, n := range []string{"1", "2", "3"} {
		option.RegisterTranslatable("home_hero_"+n+"_label", "hero", "轮播"+n+" 标签")
		option.RegisterTranslatable("home_hero_"+n+"_title", "hero", "轮播"+n+" 标题")
		option.RegisterTranslatable("home_hero_"+n+"_desc", "hero", "轮播"+n+" 描述")
		option.RegisterTranslatable("home_hero_"+n+"_btn1_text", "hero", "轮播"+n+" 按钮1")
		option.RegisterTranslatable("home_hero_"+n+"_btn2_text", "hero", "轮播"+n+" 按钮2")
	}

	// About section
	option.RegisterTranslatable("home_about_title", "about", "标题")
	option.RegisterTranslatable("home_about_btn_text", "about", "按钮文字")

	// Stats (x3)
	for _, n := range []string{"1", "2", "3"} {
		option.RegisterTranslatable("home_stat_"+n+"_label", "stats", "统计"+n+" 标签")
	}

	// Products section
	option.RegisterTranslatable("home_products_label", "products", "区域标签")
	option.RegisterTranslatable("home_products_title", "products", "标题")

	// Services section
	option.RegisterTranslatable("home_services_label", "services", "区域标签")
	option.RegisterTranslatable("home_services_title", "services", "标题")

	// ===== About Page =====
	// Banner
	option.RegisterTranslatable("about_banner_title", "about_page", "横幅标题")
	option.RegisterTranslatable("about_banner_subtitle", "about_page", "横幅副标题")

	// Intro (Who We Are)
	option.RegisterTranslatable("about_intro_label", "about_page", "简介标签")
	option.RegisterTranslatable("about_intro_title", "about_page", "简介标题")
	option.RegisterTranslatable("about_intro_p1", "about_page", "简介第一段")
	option.RegisterTranslatable("about_intro_p2", "about_page", "简介第二段")

	// Principles (x4)
	for _, n := range []string{"1", "2", "3", "4"} {
		option.RegisterTranslatable("about_principle_"+n+"_title", "about_page", "原则"+n+" 标题")
		option.RegisterTranslatable("about_principle_"+n+"_desc", "about_page", "原则"+n+" 描述")
	}
}
