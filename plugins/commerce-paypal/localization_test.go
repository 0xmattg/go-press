package commercepaypal

import (
	"bytes"
	"html/template"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	coreI18n "github.com/0xmattg/go-press/core/i18n"

	"github.com/gin-gonic/gin"
)

func renderPayPalAdminSettings(t *testing.T, lang string, data gin.H) string {
	t.Helper()
	catalog := coreI18n.NewCatalog("en", coreI18n.LoadFlatMessagesDir(filepath.Join("locales", "admin")))
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"X": func(_ interface{}, key, fallback string, args ...interface{}) string {
			if msg := catalog.Message(lang, key); msg != "" {
				return coreI18n.FormatMessage(msg, args...)
			}
			return coreI18n.FormatMessage(fallback, args...)
		},
	}).ParseFiles(filepath.Join("templates", "admin", "settings.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	data["AdminLanguage"] = lang
	data["PluginName"] = "Commerce PayPal"
	data["PluginSlug"] = pluginSlug
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "content", data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestPayPalAdminSettingsSwitchLanguage(t *testing.T) {
	englishReady := renderPayPalAdminSettings(t, "en", gin.H{
		"Enabled": true, "Sandbox": true, "SecretConfigured": true,
		"Ready": true, "WebhookURL": "https://shop.example/commerce/paypal/webhook",
	})
	englishMissing := renderPayPalAdminSettings(t, "en", gin.H{
		"Enabled": false, "Sandbox": false, "SecretConfigured": false, "Ready": false,
	})
	english := englishReady + englishMissing
	if regexp.MustCompile(`\p{Han}`).MatchString(english) {
		t.Fatalf("English PayPal settings leaked Chinese copy: %s", english)
	}
	for _, want := range []string{"PayPal Configuration", "Save Settings", "Configured; leave blank", "Not configured"} {
		if !strings.Contains(english, want) {
			t.Fatalf("English PayPal settings missing %q", want)
		}
	}

	chinese := renderPayPalAdminSettings(t, "zh-CN", gin.H{"Sandbox": true})
	if !strings.Contains(chinese, "PayPal 配置") || !strings.Contains(chinese, "保存设置") {
		t.Fatalf("Chinese PayPal settings were not localized: %s", chinese)
	}
}
