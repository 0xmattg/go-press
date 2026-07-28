package monojournal

import "go-press/core/option"

func registerTranslatableOptions() {
	entries := []struct {
		key, group, label string
	}{
		{"home_logo_text", "brand", "站点品牌名"},
		{"home_intro_eyebrow", "home", "首页眉题"},
		{"home_intro_title", "home", "首页标题"},
		{"home_intro_text", "home", "首页简介"},
		{"home_featured_label", "home", "精选文章标签"},
		{"home_latest_title", "home", "最新文章标题"},
		{"home_about_title", "home", "作者区域标题"},
		{"home_about_text", "home", "作者区域简介"},
		{"about_author_name", "author", "作者姓名"},
		{"about_author_role", "author", "作者身份"},
		{"about_author_bio", "author", "作者简介"},
		{"about_location", "author", "所在地"},
		{"footer_text", "footer", "页脚文案"},
	}
	for _, entry := range entries {
		option.RegisterTranslatable(entry.key, entry.group, entry.label)
	}
}
