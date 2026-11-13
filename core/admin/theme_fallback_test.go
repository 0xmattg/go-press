// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"
)

func fallbackTestHandler(t *testing.T) *Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewHandler(
		&Service{rbac: user.NewRBAC(), options: option.NewMemoryStore(map[string]string{"admin_language": "en"})},
		content.NewRegistry(),
		filepath.Join("templates"),
	)
	h.SetThemeManager(&ThemeManager{
		AvailableFn: func() []ThemeDisplayInfo {
			return []ThemeDisplayInfo{{Name: "Fallback", Slug: "fallback-theme", Version: "1.0.0", Active: true}}
		},
		FallbackFn: func() *ThemeFallbackInfo {
			return &ThemeFallbackInfo{Requested: "saved-theme", Used: "fallback-theme"}
		},
		LoadIssuesFn: func() []ThemeLoadIssueInfo {
			return []ThemeLoadIssueInfo{{Slug: "saved-theme", Reason: strings.Repeat("x", maxThemeIssueRunes+50)}}
		},
	})
	return h
}

func serveAs(h *Handler, role string, handle func(*gin.Context)) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/admin/test", func(c *gin.Context) {
		c.Set("admin_role", role)
		handle(c)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/test", nil))
	return rec
}

// The fallback banner appears on admin pages for roles that can open the
// themes page, and never shows the underlying error.
func TestThemeFallbackBannerFollowsSettingRead(t *testing.T) {
	h := fallbackTestHandler(t)
	page := func(c *gin.Context) { h.render(c, "themes", gin.H{"Title": "Any page"}) }

	for role, want := range map[string]bool{
		user.RoleSuperAdmin: true,
		user.RoleEditor:     h.svc.rbac.Can(user.RoleEditor, "setting", "read"),
		user.RoleAuthor:     false,
		user.RoleSubscriber: false,
	} {
		body := serveAs(h, role, page).Body.String()
		shown := strings.Contains(body, "Fallback theme in use:")
		if shown != want {
			t.Errorf("%s: banner shown = %v, want %v", role, shown, want)
		}
		if shown && (!strings.Contains(body, `the saved theme &#34;saved-theme&#34;`) || !strings.Contains(body, `href="/admin/themes"`)) {
			t.Errorf("%s: banner text or link missing:\n%s", role, body)
		}
	}
}

func TestThemeListShowsThemesThatFailedToLoad(t *testing.T) {
	h := fallbackTestHandler(t)
	rec := serveAs(h, user.RoleSuperAdmin, h.ThemeList)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "These themes failed to load and are unavailable:") || !strings.Contains(body, "<code>saved-theme</code>") {
		t.Fatalf("failed-theme list missing:\n%s", body)
	}
	if strings.Contains(body, strings.Repeat("x", maxThemeIssueRunes+1)) || !strings.Contains(body, strings.Repeat("x", maxThemeIssueRunes)+"…") {
		t.Fatal("the reason was not truncated")
	}

	// Roles without setting.read cannot open the themes page at all.
	if rec := serveAs(h, user.RoleAuthor, h.ThemeList); rec.Code != http.StatusFound {
		t.Fatalf("author: status = %d, want redirect", rec.Code)
	}
}
