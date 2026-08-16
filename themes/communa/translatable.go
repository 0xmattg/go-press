package communa

import "github.com/0xmattg/go-press/core/option"

// registerTranslatableOptions declares every text-based theme setting that can be
// translated per language. Called from Setup() so core and the optional multilang
// plugin know which option keys to route through the translation layer. Only
// human-readable text is registered here — URLs, image paths, and numeric values
// are intentionally left out.
func registerTranslatableOptions() {
	// Brand & header
	option.RegisterTranslatable("home_logo_text", "brand", "品牌名称")
	option.RegisterTranslatable("home_login_text", "brand", "登录按钮文字")
	option.RegisterTranslatable("home_join_text", "brand", "注册按钮文字")

	// Hero
	option.RegisterTranslatable("home_hero_eyebrow", "hero", "Hero 小标签")
	option.RegisterTranslatable("home_hero_title", "hero", "Hero 标题")
	option.RegisterTranslatable("home_hero_subtitle", "hero", "Hero 副标题")
	option.RegisterTranslatable("home_hero_search_placeholder", "hero", "搜索框占位文字")
	option.RegisterTranslatable("home_hero_cta_text", "hero", "Hero 按钮文字")
	option.RegisterTranslatable("home_hero_btn1_text", "hero", "欢迎屏按钮1 文字")
	option.RegisterTranslatable("home_hero_btn2_text", "hero", "欢迎屏按钮2 文字")
	for _, n := range []string{"1", "2", "3"} {
		option.RegisterTranslatable("home_banner_"+n+"_label", "hero", "Banner"+n+" 标签")
		option.RegisterTranslatable("home_banner_"+n+"_title", "hero", "Banner"+n+" 标题")
		option.RegisterTranslatable("home_banner_"+n+"_text", "hero", "Banner"+n+" 描述")
	}

	// Homepage widget / section titles
	option.RegisterTranslatable("home_members_title", "home", "成员组件标题")
	option.RegisterTranslatable("home_groups_title", "home", "群组组件标题")
	option.RegisterTranslatable("home_activity_title", "home", "动态组件标题")
	option.RegisterTranslatable("home_discussions_title", "home", "讨论组件标题")
	option.RegisterTranslatable("home_stats_title", "home", "统计组件标题")
	option.RegisterTranslatable("home_featured_groups_label", "home", "推荐群组区域标签")
	option.RegisterTranslatable("home_featured_groups_title", "home", "推荐群组区域标题")
	option.RegisterTranslatable("home_featured_groups_subtitle", "home", "推荐群组区域副标题")

	// CTA band
	option.RegisterTranslatable("home_cta_title", "cta", "CTA 标题")
	option.RegisterTranslatable("home_cta_desc", "cta", "CTA 描述")
	option.RegisterTranslatable("home_cta_btn_text", "cta", "CTA 按钮文字")

	// Company / footer
	option.RegisterTranslatable("company_description", "company", "社区简介")
	option.RegisterTranslatable("company_address", "company", "地址")
	option.RegisterTranslatable("company_hours", "company", "服务时间")
	option.RegisterTranslatable("footer_copyright_text", "company", "页脚版权文案")

	// About page
	option.RegisterTranslatable("about_title", "about", "关于页标题")
	option.RegisterTranslatable("about_subtitle", "about", "关于页副标题")
	option.RegisterTranslatable("about_intro", "about", "关于页导语")
	option.RegisterTranslatable("about_story", "about", "关于页正文")
	option.RegisterTranslatable("about_values_title", "about", "价值区块标题")
	for _, n := range []string{"1", "2", "3"} {
		option.RegisterTranslatable("about_value_"+n+"_title", "about", "价值"+n+" 标题")
		option.RegisterTranslatable("about_value_"+n+"_desc", "about", "价值"+n+" 描述")
	}
	option.RegisterTranslatable("about_cta_title", "about", "关于页 CTA 标题")
	option.RegisterTranslatable("about_cta_desc", "about", "关于页 CTA 描述")
	option.RegisterTranslatable("about_cta_btn_text", "about", "关于页 CTA 按钮")

	// Contact page
	option.RegisterTranslatable("contact_title", "contact", "联系页标题")
	option.RegisterTranslatable("contact_subtitle", "contact", "联系页副标题")
}
