package financialnews

import "go-press/core/option"

// registerTranslatableOptions declares text-based theme settings that can be
// translated by the multilingual plugin.
func registerTranslatableOptions() {
	for _, key := range []string{
		"site_name",
		"site_description",
		"footer_text",
	} {
		option.RegisterTranslatable(key, "brand", key)
	}
}
