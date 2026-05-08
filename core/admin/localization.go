package admin

import (
	"embed"

	coreI18n "go-press/core/i18n"
)

const defaultAdminLanguage = coreI18n.DefaultUILanguage

var adminLanguageCodes = []string{"en", "zh-CN"}

//go:embed locales/*.json
var adminLocaleFS embed.FS

var adminCatalog = coreI18n.NewCatalog(defaultAdminLanguage, coreI18n.LoadFlatMessages(adminLocaleFS, "locales"))

func normalizeAdminLanguage(lang string) string {
	return coreI18n.NormalizeSupportedLanguage(lang, adminLanguageCodes, defaultAdminLanguage)
}

func adminLanguageName(lang string) string {
	switch normalizeAdminLanguage(lang) {
	case "zh-CN":
		return "简体中文"
	default:
		return "English"
	}
}

func adminSupportedLanguages() []SettingOptionView {
	return []SettingOptionView{
		{Value: "en", Label: "English"},
		{Value: "zh-CN", Label: "简体中文"},
	}
}

func adminT(lang, key string, args ...interface{}) string {
	return adminCatalog.T(lang, key, args...)
}

func adminMessage(lang, key string) string {
	return adminCatalog.Message(lang, key)
}
