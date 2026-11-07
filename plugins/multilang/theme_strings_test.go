package multilang

import (
	"reflect"
	"testing"
	"testing/fstest"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/0xmattg/go-press/core"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
)

// T19: the string translation page lists the active theme's locale keys from
// core's accessor, the same keys and defaults the theme's locales/*.json hold,
// without the plugin reading the theme directory.
func TestReadThemeLocaleKeysUsesCoreThemeMessages(t *testing.T) {
	mgr := coreI18n.NewManager("en")
	mgr.LoadThemeLocalesFS(fstest.MapFS{
		"locales/en.json":       {Data: []byte(`{"nav.home":"Home","hero.title":"Welcome"}`)},
		"locales/zh.json":       {Data: []byte(`{"nav.home":"首页","footer.note":"备注"}`)},
		"locales/admin/en.json": {Data: []byte(`{"admin.only":"not a theme string"}`)},
	})
	// Overrides saved through the plugin are not theme defaults.
	mgr.AddMessages("en", []*goi18n.Message{{ID: "override.only", Other: "saved override"}})
	p := &Plugin{engine: &core.Engine{I18n: mgr}}

	byLang, keys := p.readThemeLocaleKeys()
	if want := []string{"footer.note", "hero.title", "nav.home"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if byLang["en"]["nav.home"] != "Home" || byLang["zh"]["nav.home"] != "首页" || byLang["zh"]["footer.note"] != "备注" {
		t.Fatalf("defaults by language = %v", byLang)
	}
	if _, ok := byLang["admin"]; ok {
		t.Fatal("admin locale files are not theme strings")
	}
}

func TestReadThemeLocaleKeysWithoutEngine(t *testing.T) {
	byLang, keys := (&Plugin{}).readThemeLocaleKeys()
	if byLang == nil || len(byLang) != 0 || keys != nil {
		t.Fatalf("without an engine: %v, %v; want empty", byLang, keys)
	}
	byLang, keys = themeLocaleKeys(nil)
	if byLang == nil || len(keys) != 0 {
		t.Fatalf("themeLocaleKeys(nil) = %v, %v", byLang, keys)
	}
}
