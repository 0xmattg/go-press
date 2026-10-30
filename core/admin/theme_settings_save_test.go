// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

func themeSettingsTestRouter(store *option.Store, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := &Handler{svc: &Service{options: store, rbac: user.NewRBAC()}}
	router := gin.New()
	router.POST("/admin/themes/:slug/settings", func(c *gin.Context) {
		c.Set("admin_user_id", uint(1))
		c.Set("admin_username", "operator")
		c.Set("admin_role", role)
		handler.ThemeSettingsSave(c)
	})
	return router
}

func postThemeSettings(router http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/demo/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestThemeSettingsSaveRejectsRoleWithoutSettingUpdate(t *testing.T) {
	store := option.NewMemoryStore(nil)
	rec := postThemeSettings(themeSettingsTestRouter(store, user.RoleEditor), url.Values{"home_title": {"Hijacked"}})

	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/?error=") {
		t.Fatalf("status = %d location = %q, want permission-denied redirect", rec.Code, rec.Header().Get("Location"))
	}
	if got := store.Get("home_title"); got != "" {
		t.Fatalf("home_title = %q, want unchanged", got)
	}
}

func TestThemeSettingsSaveValidatesCoreKeysAndSavesNothingOnFailure(t *testing.T) {
	store := option.NewMemoryStore(map[string]string{"site_timezone": "UTC"})
	rec := postThemeSettings(themeSettingsTestRouter(store, user.RoleSuperAdmin), url.Values{
		"home_title":    {"New title"},
		"site_timezone": {"Mars/Olympus"},
	})

	location := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.Contains(location, "/admin/themes/demo/settings?error=") {
		t.Fatalf("status = %d location = %q, want validation error redirect", rec.Code, location)
	}
	if got := store.Get("site_timezone"); got != "UTC" {
		t.Fatalf("site_timezone = %q, want UTC (invalid value rejected)", got)
	}
	if got := store.Get("home_title"); got != "" {
		t.Fatalf("home_title = %q, want nothing saved from a rejected form", got)
	}
}

func TestThemeSettingsSaveAcceptsThemeAndValidCoreKeys(t *testing.T) {
	store := option.NewMemoryStore(nil)
	rec := postThemeSettings(themeSettingsTestRouter(store, user.RoleSuperAdmin), url.Values{
		"home_title":    {"Welcome"},
		"site_name":     {"Demo Site"},
		"site_timezone": {"Asia/Shanghai"},
		"color_scheme":  {"ocean"},
		"unrelated_key": {"ignored"},
	})

	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "success=") {
		t.Fatalf("status = %d location = %q, want success redirect", rec.Code, rec.Header().Get("Location"))
	}
	for key, want := range map[string]string{
		"home_title": "Welcome", "site_name": "Demo Site", "site_timezone": "Asia/Shanghai", "theme_color_scheme": "ocean",
	} {
		if got := store.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got := store.Get("unrelated_key"); got != "" {
		t.Fatalf("unrelated_key = %q, want keys outside the theme namespace ignored", got)
	}
}
