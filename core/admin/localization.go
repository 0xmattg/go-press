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

func supportedTimezones() []SettingOptionView {
	return []SettingOptionView{
		{Value: "Local", Label: "Server local time"},
		{Value: "UTC", Label: "UTC"},
		{Value: "Asia/Shanghai", Label: "Asia/Shanghai"},
		{Value: "Asia/Hong_Kong", Label: "Asia/Hong_Kong"},
		{Value: "Asia/Tokyo", Label: "Asia/Tokyo"},
		{Value: "Asia/Singapore", Label: "Asia/Singapore"},
		{Value: "Asia/Dubai", Label: "Asia/Dubai"},
		{Value: "Europe/London", Label: "Europe/London"},
		{Value: "Europe/Berlin", Label: "Europe/Berlin"},
		{Value: "America/New_York", Label: "America/New_York"},
		{Value: "America/Chicago", Label: "America/Chicago"},
		{Value: "America/Denver", Label: "America/Denver"},
		{Value: "America/Los_Angeles", Label: "America/Los_Angeles"},
		{Value: "Australia/Sydney", Label: "Australia/Sydney"},
	}
}

func adminT(lang, key string, args ...interface{}) string {
	return adminCatalog.T(lang, key, args...)
}

func adminMessage(lang, key string) string {
	return adminCatalog.Message(lang, key)
}
