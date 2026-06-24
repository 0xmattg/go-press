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

type fakeDataQueryStore struct {
	page    int
	limit   int
	rows    []EventQueryRow
	hasMore bool
}

func (f *fakeDataQueryStore) RecentEventRows(_ context.Context, page, limit int) ([]EventQueryRow, bool, error) {
	f.page = page
	f.limit = limit
	return f.rows, f.hasMore, nil
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

func TestDataQueryRouteRejectsRoleWithoutAnalyticsPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	p := New()
	p.active.Store(true)
	p.dataQuery = &fakeDataQueryStore{}

	router := gin.New()
	router.GET("/admin/plugins/gopress-analytics/data-query",
		admin.RequirePermission(auth, rbac, "analytics", "read"),
		p.handleDataQuery,
	)

	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "subscriber", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins/gopress-analytics/data-query?table=events", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDataQueryRouteAllowsGrantedRoleWithPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "analytics", "read")
	store := &fakeDataQueryStore{
		rows: []EventQueryRow{{
			OccurredAt:     time.Date(2026, 6, 24, 1, 2, 3, 0, time.UTC),
			NormalizedPath: "/blog",
			IPAddress:      "203.0.113.8",
			Country:        "US",
			UserAgent:      "Mozilla/5.0",
		}},
		hasMore: true,
	}
	p := New()
	p.active.Store(true)
	p.dataQuery = store

	router := gin.New()
	router.GET("/admin/plugins/gopress-analytics/data-query",
		admin.RequirePermission(auth, rbac, "analytics", "read"),
		p.handleDataQuery,
	)

	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins/gopress-analytics/data-query?table=events&page=2&limit=500", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.page != 2 || store.limit != 100 {
		t.Fatalf("pagination = page %d limit %d, want page 2 limit 100", store.page, store.limit)
	}
	var got struct {
		Table   string          `json:"table"`
		Page    int             `json:"page"`
		Limit   int             `json:"limit"`
		HasMore bool            `json:"has_more"`
		Rows    []EventQueryRow `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Table != "events" || got.Page != 2 || got.Limit != 100 || !got.HasMore || len(got.Rows) != 1 {
		t.Fatalf("unexpected data query response: %#v", got)
	}
	if got.Rows[0].Country != "US" {
		t.Fatalf("country = %q, want US", got.Rows[0].Country)
	}
}

func TestGeoIPUpdateRouteRequiresPluginUpdatePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	p := New()
	p.active.Store(true)
	p.geoIP = newGeoIPDatabase(filepath.Join(t.TempDir(), "geoip.csv.gz"))

	router := gin.New()
	router.POST("/admin/plugins/gopress-analytics/geoip/update",
		admin.RequirePermission(auth, rbac, "plugin", "update"),
		p.handleGeoIPUpdate,
	)

	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/gopress-analytics/geoip/update", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGeoIPUpdateRouteAllowsPluginUpdater(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	rbac := user.NewRBAC()
	dbPath := filepath.Join(t.TempDir(), "geoip.csv.gz")
	sourcePath := filepath.Join(t.TempDir(), "source.csv.gz")
	if err := writeGeoIPTestFile(sourcePath, "203.0.113.0,203.0.113.255,US\n"); err != nil {
		t.Fatalf("write geoip fixture: %v", err)
	}
	p := New()
	p.active.Store(true)
	p.geoIP = newGeoIPDatabase(dbPath)
	p.geoIP.sources = []string{"file://" + sourcePath}

	router := gin.New()
	router.POST("/admin/plugins/gopress-analytics/geoip/update",
		admin.RequirePermission(auth, rbac, "plugin", "update"),
		p.handleGeoIPUpdate,
	)

	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "admin", Role: user.RoleSuperAdmin})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/gopress-analytics/geoip/update", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := p.geoIP.LookupCountry("203.0.113.8"); got != "US" {
		t.Fatalf("LookupCountry after update = %q, want US", got)
	}
}

func TestDataQueryRouteRejectsUnknownTable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := New()
	p.active.Store(true)
	p.dataQuery = &fakeDataQueryStore{}

	router := gin.New()
	router.GET("/data-query", p.handleDataQuery)
	req := httptest.NewRequest(http.MethodGet, "/data-query?table=sessions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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
		`id="gpaCountryDonut"`,
		`IP country distribution`,
		`id="gpaTopToggle"`,
		`.gpa-panels > .gpa-panel { height:376px;`,
		`gpa-top-bar`,
		`collapsedTopRows = 6`,
		`data-days="30">30`,
		`"days":30`,
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
