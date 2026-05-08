package installer

import (
	"embed"

	coreI18n "go-press/core/i18n"
)

const defaultInstallerLanguage = coreI18n.DefaultUILanguage

var installerLanguageCodes = []string{"en", "zh-CN"}

//go:embed locales/*.json
var installerLocaleFS embed.FS

var installerCatalog = coreI18n.NewCatalog(defaultInstallerLanguage, coreI18n.LoadFlatMessages(installerLocaleFS, "locales"))

func normalizeInstallerLanguage(lang string) string {
	return coreI18n.NormalizeSupportedLanguage(lang, installerLanguageCodes, defaultInstallerLanguage)
}

func installerT(lang, key string, args ...interface{}) string {
	return installerCatalog.T(lang, key, args...)
}
