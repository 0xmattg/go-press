package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestPluginSettingsResourceUsesProviderAndFallback(t *testing.T) {
	custom := &PluginCallbacks{SettingsResourceFn: func(slug string) string {
		if slug == "shop" {
			return "shop_settings"
		}
		return ""
	}}
	if got := custom.SettingsResource("shop"); got != "shop_settings" {
		t.Fatalf("custom settings resource = %q, want shop_settings", got)
	}
	if got := custom.SettingsResource("plain"); got != "plugin" {
		t.Fatalf("empty provider resource = %q, want plugin fallback", got)
	}
	if got := (*PluginCallbacks)(nil).SettingsResource("plain"); got != "plugin" {
		t.Fatalf("nil callbacks resource = %q, want plugin fallback", got)
	}
}

func TestPluginSettingsSaveChecksPermissionBeforeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		role          string
		wantValidated bool
	}{
		{name: "subscriber denied", role: user.RoleSubscriber, wantValidated: false},
		{name: "super admin reaches validation", role: user.RoleSuperAdmin, wantValidated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validated := false
			h := &Handler{
				svc: &Service{rbac: user.NewRBAC()},
				pluginCallbacks: &PluginCallbacks{SettingsValidateFn: func(string, map[string]string) error {
					validated = true
					return errAdminCapabilityUnavailable
				}},
			}
			r := gin.New()
			r.POST("/admin/plugins/shop/settings", func(c *gin.Context) {
				c.Set("admin_role", tc.role)
				h.PluginSettingsSave(c)
			})
			form := url.Values{"plugin_shop_enabled": {"1"}}
			req := httptest.NewRequest(http.MethodPost, "/admin/plugins/shop/settings", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want redirect", rec.Code)
			}
			if validated != tc.wantValidated {
				t.Fatalf("validated = %v, want %v", validated, tc.wantValidated)
			}
		})
	}
}

func TestPluginSettingsPermissionUsesDeclaredResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "shop_settings", "read")
	rbac.GrantCapability(user.RoleEditor, "shop_settings", "update")
	h := &Handler{
		svc: &Service{rbac: rbac},
		pluginCallbacks: &PluginCallbacks{SettingsResourceFn: func(string) string {
			return "shop_settings"
		}},
	}

	for _, tc := range []struct {
		name   string
		role   string
		action string
		want   int
	}{
		{name: "subscriber read denied", role: user.RoleSubscriber, action: "read", want: http.StatusFound},
		{name: "subscriber update denied", role: user.RoleSubscriber, action: "update", want: http.StatusFound},
		{name: "editor read allowed", role: user.RoleEditor, action: "read", want: http.StatusNoContent},
		{name: "editor update allowed", role: user.RoleEditor, action: "update", want: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Any("/admin/plugins/shop/settings", func(c *gin.Context) {
				c.Set("admin_role", tc.role)
				if !h.checkPermission(c, h.pluginSettingsResource("shop"), tc.action) {
					return
				}
				c.Status(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/plugins/shop/settings", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestPermissionAwareMenuHidesDeniedExtensionSection(t *testing.T) {
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "shop_order", "read")
	h := &Handler{svc: &Service{rbac: rbac}}
	items := []AdminMenuItem{
		{Section: "Commerce"},
		{Label: "Orders", Resource: "shop_order", Action: "read"},
		{Label: "Settings", Resource: "shop_settings", Action: "read"},
	}

	editor := h.filterPermittedMenuItems(items, user.RoleEditor)
	if len(editor) != 2 || editor[0].Section != "Commerce" || editor[1].Label != "Orders" {
		t.Fatalf("editor menu = %#v, want section + orders only", editor)
	}
	subscriber := h.filterPermittedMenuItems(items, user.RoleSubscriber)
	if len(subscriber) != 0 {
		t.Fatalf("subscriber menu = %#v, want empty without dangling section", subscriber)
	}
}
