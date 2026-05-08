package i18n

import (
	"testing"
	"testing/fstest"
)

func TestCatalogLoadsFlatMessagesAndFallsBack(t *testing.T) {
	t.Parallel()

	localeFS := fstest.MapFS{
		"locales/en.json":    {Data: []byte(`{"hello":"Hello %s","quoted":"Value %q"}`)},
		"locales/zh-CN.json": {Data: []byte(`{"hello":"你好 %s"}`)},
	}

	catalog := NewCatalog("en", LoadFlatMessages(localeFS, "locales"))

	if got := catalog.T("zh-CN", "hello", "GoPress"); got != "你好 GoPress" {
		t.Fatalf("Chinese message = %q, want %q", got, "你好 GoPress")
	}
	if got := catalog.T("zh-CN", "quoted", "admin"); got != `Value "admin"` {
		t.Fatalf("fallback message = %q, want %q", got, `Value "admin"`)
	}
	if got := catalog.T("fr", "hello", "GoPress"); got != "Hello GoPress" {
		t.Fatalf("default language message = %q, want %q", got, "Hello GoPress")
	}
	if got := catalog.T("en", "missing.key", "unused"); got != "missing.key" {
		t.Fatalf("missing key = %q, want %q", got, "missing.key")
	}
}

func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"zh":      "zh-CN",
		"zh_Hans": "zh-CN",
		"en_GB":   "en",
		"pt_br":   "pt-BR",
		"":        "en",
		"unknown": "unknown",
	}

	for input, want := range tests {
		if got := NormalizeLanguage(input, DefaultUILanguage); got != want {
			t.Fatalf("NormalizeLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCatalogFallsBackToOnlyProvidedLanguage(t *testing.T) {
	t.Parallel()

	localeFS := fstest.MapFS{
		"locales/zh-CN.json": {Data: []byte(`{"hello":"你好"}`)},
	}

	catalog := NewCatalog("en", LoadFlatMessages(localeFS, "locales"))

	if got := catalog.T("en", "hello"); got != "你好" {
		t.Fatalf("single-language fallback = %q, want %q", got, "你好")
	}
}

func TestNormalizeSupportedLanguage(t *testing.T) {
	t.Parallel()

	supported := []string{"en", "zh-CN"}
	if got := NormalizeSupportedLanguage("zh_Hans", supported, DefaultUILanguage); got != "zh-CN" {
		t.Fatalf("NormalizeSupportedLanguage() = %q, want %q", got, "zh-CN")
	}
	if got := NormalizeSupportedLanguage("fr", supported, DefaultUILanguage); got != "en" {
		t.Fatalf("NormalizeSupportedLanguage() = %q, want %q", got, "en")
	}
}
