package admin

import (
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/user"
)

func testPluginAdminCatalog() *coreI18n.Catalog {
	return coreI18n.NewCatalog("en", map[string]map[string]string{
		"en": {
			"plugin.name":                         "Commerce",
			"plugin.description":                  "Localized commerce description",
			"admin.content_type.plugin_product":   "Plugin Products",
			"admin.taxonomy.plugin_product_cat":   "Plugin Product Categories",
			"admin.meta.plugin_product.stock_qty": "Stock Quantity",
		},
		"zh-CN": {
			"plugin.name":                       "电商",
			"admin.content_type.plugin_product": "插件商品",
			"admin.taxonomy.plugin_product_cat": "插件商品分类",
		},
	})
}

func TestActivePluginCatalogLocalizesRegisteredAdminDomains(t *testing.T) {
	active := true
	catalog := testPluginAdminCatalog()
	h := &Handler{pluginCallbacks: &PluginCallbacks{
		AllFn: func() []PluginInfo {
			return []PluginInfo{{Slug: "commerce", Active: active}}
		},
		LocaleCatalogFn: func(slug string) *coreI18n.Catalog {
			if slug == "commerce" {
				return catalog
			}
			return nil
		},
	}}

	if got := h.contentTypeLabel("en", "plugin_product", "fallback"); got != "Plugin Products" {
		t.Fatalf("English plugin content label = %q, want Plugin Products", got)
	}
	if got := h.taxonomyLabel("zh-CN", "plugin_product_cat", "fallback"); got != "插件商品分类" {
		t.Fatalf("Chinese plugin taxonomy label = %q, want 插件商品分类", got)
	}
	if got := h.metaFieldLabel("en", "plugin_product", "stock_qty", "fallback"); got != "Stock Quantity" {
		t.Fatalf("English plugin meta label = %q, want Stock Quantity", got)
	}

	active = false
	if got := h.contentTypeLabel("en", "plugin_product", "fallback"); got != "fallback" {
		t.Fatalf("inactive plugin catalog leaked label %q", got)
	}
}

func TestPluginCardsUseLocalizedDisplayMetadata(t *testing.T) {
	h := &Handler{pluginCallbacks: &PluginCallbacks{
		LocaleCatalogFn: func(string) *coreI18n.Catalog { return testPluginAdminCatalog() },
	}}
	plugins := []PluginInfo{{Name: "commerce", DisplayName: "commerce", Slug: "commerce", Description: "fallback"}}
	h.localizePlugins(plugins, "en")
	if plugins[0].DisplayName != "Commerce" || plugins[0].Description != "Localized commerce description" {
		t.Fatalf("localized plugin metadata = %#v", plugins[0])
	}
}

func TestOptInModulesUseLocalizedDisplayMetadata(t *testing.T) {
	h := &Handler{pluginCallbacks: &PluginCallbacks{
		AllFn: func() []PluginInfo {
			return []PluginInfo{
				{Name: "commerce", DisplayName: "commerce", Slug: "commerce", Description: "fallback", DefaultInactive: true},
				{Name: "always-on", DisplayName: "always-on", Slug: "always-on"},
			}
		},
		LocaleCatalogFn: func(slug string) *coreI18n.Catalog {
			if slug == "commerce" {
				return testPluginAdminCatalog()
			}
			return nil
		},
	}}

	modules := h.optInModules("en")
	if len(modules) != 1 {
		t.Fatalf("optInModules() returned %d modules, want 1", len(modules))
	}
	if modules[0].DisplayName != "Commerce" || modules[0].Description != "Localized commerce description" {
		t.Fatalf("localized module metadata = %#v", modules[0])
	}
}

func TestModulePanelCoreLocales(t *testing.T) {
	tests := []struct {
		lang string
		key  string
		want string
	}{
		{lang: "en", key: "modules.panel_title", want: "Modules"},
		{lang: "en", key: "modules.on", want: "Enabled"},
		{lang: "en", key: "modules.off", want: "Disabled"},
		{lang: "en", key: "modules.enable", want: "Enable"},
		{lang: "en", key: "modules.disable", want: "Disable"},
		{lang: "en", key: "modules.enable_now", want: "Enable now"},
		{lang: "en", key: "modules.panel_help", want: "Optional feature modules are disabled by default. Once enabled, their management entries appear in the sidebar."},
		{lang: "en", key: "modules.required_title", want: "The current theme requires these modules to be enabled:"},
		{lang: "zh-CN", key: "modules.panel_title", want: "模块"},
		{lang: "zh-CN", key: "modules.on", want: "已启用"},
		{lang: "zh-CN", key: "modules.off", want: "未启用"},
		{lang: "zh-CN", key: "modules.enable", want: "启用"},
		{lang: "zh-CN", key: "modules.disable", want: "停用"},
		{lang: "zh-CN", key: "modules.enable_now", want: "去启用"},
		{lang: "zh-CN", key: "modules.panel_help", want: "可选功能模块默认关闭，启用后其管理入口会出现在左侧导航。"},
		{lang: "zh-CN", key: "modules.required_title", want: "当前主题需要启用以下模块才能正常工作："},
	}

	for _, tt := range tests {
		t.Run(tt.lang+"/"+tt.key, func(t *testing.T) {
			if got := adminT(tt.lang, tt.key); got != tt.want {
				t.Fatalf("adminT(%q, %q) = %q, want %q", tt.lang, tt.key, got, tt.want)
			}
		})
	}
}

func TestAdminNavFilterReceivesRoleAndLanguage(t *testing.T) {
	bus := hook.New()
	var gotRole, gotLang string
	bus.AddFilter(hook.AdminNavItems, func(value interface{}, args ...interface{}) interface{} {
		if len(args) > 0 {
			gotRole, _ = args[0].(string)
		}
		if len(args) > 1 {
			gotLang, _ = args[1].(string)
		}
		return value
	}, 10)
	h := &Handler{
		registry: content.NewRegistry(),
		hooks:    bus,
		svc:      &Service{rbac: user.NewRBAC()},
	}
	h.buildMenuItems("zh-CN", user.RoleSuperAdmin)
	if gotRole != user.RoleSuperAdmin || gotLang != "zh-CN" {
		t.Fatalf("admin nav args = role %q, lang %q", gotRole, gotLang)
	}
}

func TestSystemSettingsIsAlwaysLastAdminNavItem(t *testing.T) {
	bus := hook.New()
	bus.AddFilter(hook.AdminNavItems, func(value interface{}, _ ...interface{}) interface{} {
		items, _ := value.([]AdminMenuItem)
		return append(items,
			AdminMenuItem{Section: "Extension"},
			AdminMenuItem{Label: "Extension Item", URL: "/admin/extension"},
		)
	}, 10)
	h := &Handler{
		registry: content.NewRegistry(),
		hooks:    bus,
		svc:      &Service{rbac: user.NewRBAC()},
	}

	items := h.buildMenuItems("en", user.RoleSuperAdmin)
	if len(items) == 0 {
		t.Fatal("admin menu is empty")
	}
	last := items[len(items)-1]
	if last.URL != "/admin/settings" || last.Active != "settings" {
		t.Fatalf("last admin menu item = %#v, want System Settings", last)
	}
	settingsCount := 0
	for _, item := range items {
		if item.URL == "/admin/settings" {
			settingsCount++
		}
	}
	if settingsCount != 1 {
		t.Fatalf("System Settings item count = %d, want 1", settingsCount)
	}
}
