package admin

import (
	"testing"

	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/user"
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
