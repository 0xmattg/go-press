package gopressanalytics

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/admin"
	"go-press/core/user"
)

type fakeSummaryStore struct {
	result Summary
}

func (f fakeSummaryStore) Summary(_ context.Context, days int, _ *time.Location) (Summary, error) {
	result := f.result
	result.Days = days
	return result, nil
}

func TestSummaryRouteRejectsRoleWithoutAnalyticsPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	p := New()
	p.active.Store(true)
	p.summary = fakeSummaryStore{result: Summary{PageViews: 12}}

	router := gin.New()
	router.GET("/admin/plugins/gopress-analytics/summary",
		admin.RequirePermission(auth, rbac, "analytics", "read"),
		p.handleSummary,
	)

	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "subscriber", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins/gopress-analytics/summary?days=30", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSummaryRouteAllowsGrantedRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "analytics", "read")
	p := New()
	p.active.Store(true)
	p.summary = fakeSummaryStore{result: Summary{PageViews: 12, UniqueVisitors: 7}}

	router := gin.New()
	router.GET("/admin/plugins/gopress-analytics/summary",
		admin.RequirePermission(auth, rbac, "analytics", "read"),
		p.handleSummary,
	)

	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins/gopress-analytics/summary?days=7", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Days != 7 || got.PageViews != 12 || got.UniqueVisitors != 7 {
		t.Fatalf("unexpected summary: %#v", got)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cacheControl)
	}
}

func TestSummaryRouteRejectsInvalidRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := New()
	p.active.Store(true)
	p.summary = fakeSummaryStore{}

	router := gin.New()
	router.GET("/summary", p.handleSummary)
	req := httptest.NewRequest(http.MethodGet, "/summary?days=365", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDashboardWidgetRequiresAnalyticsPermission(t *testing.T) {
	widget, err := template.New("analytics-widget").ParseFiles(
		filepath.Join("templates", "admin", "dashboard_widget.tmpl"),
	)
	if err != nil {
		t.Fatalf("parse dashboard widget: %v", err)
	}
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "analytics", "read")
	p := New()
	p.engine = &core.Engine{RBAC: rbac}
	p.summary = fakeSummaryStore{result: Summary{PageViews: 5}}
	p.widget = widget
	p.active.Store(true)

	editorOutput := p.renderDashboardWidget(template.HTML(""), gin.H{
		"CurrentRole":   user.RoleEditor,
		"AdminLanguage": "en",
	})
	if !strings.Contains(string(htmlValue(editorOutput)), `id="gpaDashboard"`) {
		t.Fatal("authorized role did not receive analytics widget")
	}
	widgetHTML := string(htmlValue(editorOutput))
	for _, fragment := range []string{
		`class="gpa-axis-note"`,
		`id="gpaDonut"`,
		`id="gpaTopToggle"`,
		`.gpa-panels > .gpa-panel { height:376px;`,
		`gpa-top-bar`,
		`collapsedTopRows = 6`,
	} {
		if !strings.Contains(widgetHTML, fragment) {
			t.Fatalf("analytics widget missing %s", fragment)
		}
	}
	if !strings.Contains(widgetHTML, `var pvLabel = "Page views";`) {
		t.Fatal("analytics widget did not render JS labels as plain strings")
	}
	if strings.Contains(widgetHTML, `\"Page views\"`) {
		t.Fatal("analytics widget rendered JS labels with nested quotes")
	}

	subscriberOutput := p.renderDashboardWidget(template.HTML(""), gin.H{
		"CurrentRole":   user.RoleSubscriber,
		"AdminLanguage": "en",
	})
	if strings.Contains(string(htmlValue(subscriberOutput)), `id="gpaDashboard"`) {
		t.Fatal("unauthorized role received analytics widget")
	}
}
