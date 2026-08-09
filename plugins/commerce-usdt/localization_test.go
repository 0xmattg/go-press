package commerceusdt

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

func renderUSDTAdminSettings(t *testing.T, lang string, data gin.H) string {
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
	data["PluginName"] = "Commerce USDT"
	data["PluginSlug"] = pluginSlug
	if _, ok := data["Chains"]; !ok {
		data["Chains"] = presetIDs()
	}
	if _, ok := data["Chain"]; !ok {
		data["Chain"] = "ethereum"
	}
	if _, ok := data["Network"]; !ok {
		data["Network"] = "mainnet"
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestUSDTAdminSettingsSwitchLanguage(t *testing.T) {
	english := renderUSDTAdminSettings(t, "en", gin.H{
		"Enabled": true, "Ready": true, "Confirmations": 24, "WindowMinutes": 30, "USDRate": "1",
	}) + renderUSDTAdminSettings(t, "en", gin.H{"Enabled": false, "Ready": false})
	if regexp.MustCompile(`\p{Han}`).MatchString(english) {
		t.Fatalf("English USDT settings leaked Chinese copy: %s", english)
	}
	for _, want := range []string{"USDT Configuration", "Save Settings", "Confirmations", "Ethereum (ERC-20)"} {
		if !strings.Contains(english, want) {
			t.Fatalf("English USDT settings missing %q", want)
		}
	}

	chinese := renderUSDTAdminSettings(t, "zh-CN", gin.H{"Confirmations": 24, "WindowMinutes": 30, "USDRate": "1"})
	if !strings.Contains(chinese, "USDT 配置") || !strings.Contains(chinese, "保存设置") || !strings.Contains(chinese, "确认数") {
		t.Fatalf("Chinese USDT settings were not localized: %s", chinese)
	}
}
