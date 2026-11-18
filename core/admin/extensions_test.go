// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// roleSettingsOnly may change settings but holds no extension capability.
const roleSettingsOnly = "settings_only"

const testPassword = "correct horse battery staple"

// fakePackages records what the package endpoints asked for.
type fakePackages struct {
	mu       sync.Mutex
	calls    []string
	mode     DeveloperModeInfo
	uploaded []byte
	confirm  string
	err      error
	catalog  *CatalogView
	market   MarketInfo
	revoked  *RevokedThemeBanner
}

func (f *fakePackages) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakePackages) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePackages) outcome(slug string) *PackageOutcome {
	return &PackageOutcome{Slug: slug, Package: &PackageVersionView{ID: 7, Version: "1.0.0", Digest: "h1:abc"},
		Summary: &PackageSummaryView{Slug: slug, Signature: extension.SignatureVerified}}
}

func (f *fakePackages) manager() *PackageManager {
	return &PackageManager{
		ListFn: func(context.Context) (*PackageList, error) {
			f.record("list")
			return &PackageList{Packages: []PackageView{}}, f.err
		},
		CatalogFn: func(_ context.Context, lang string) (*CatalogView, error) {
			f.record("catalog " + lang)
			if f.err != nil {
				return nil, f.err
			}
			return f.catalog, nil
		},
		MarketFn:        func() MarketInfo { return f.market },
		RevokedActiveFn: func() *RevokedThemeBanner { return f.revoked },
		InstallFn: func(_ context.Context, slug, version string, actor uint) (*PackageOutcome, error) {
			f.record("install " + slug + " " + version)
			if f.err != nil {
				return nil, f.err
			}
			return f.outcome(slug), nil
		},
		UploadFn: func(_ context.Context, data []byte, confirm string, actor uint) (*PackageOutcome, error) {
			f.record("upload")
			f.mu.Lock()
			f.uploaded, f.confirm = data, confirm
			f.mu.Unlock()
			if f.err != nil {
				return nil, f.err
			}
			out := f.outcome("aurora")
			if confirm != "" {
				out.Summary.Signature = extension.SignatureMissing
			}
			return out, nil
		},
		RollbackFn: func(_ context.Context, id int64) (*PackageOutcome, error) {
			f.record("rollback")
			if f.err != nil {
				return nil, f.err
			}
			return f.outcome("aurora"), nil
		},
		UninstallFn: func(_ context.Context, slug string) (*PackageOutcome, error) {
			f.record("uninstall " + slug)
			if f.err != nil {
				return nil, f.err
			}
			return &PackageOutcome{Slug: slug}, nil
		},
		DeveloperModeFn: func() DeveloperModeInfo {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.mode
		},
		SetDeveloperModeFn: func(on bool) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if on && f.mode.Locked {
				return &PackageError{Code: "developer_mode_locked"}
			}
			f.calls = append(f.calls, map[bool]string{true: "developer on", false: "developer off"}[on])
			f.mode.Enabled = on && !f.mode.Locked
			return nil
		},
	}
}

// extensionDBs numbers the in-memory databases of one test run.
var extensionDBs atomic.Int64

// extensionTestDB returns users (1: an admin with testPassword, 2: one
// without a password) and an audit log.
func extensionTestDB(t *testing.T) (*user.Repository, *audit.Service, *gorm.DB) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	name := strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatInt(extensionDBs.Add(1), 10)
	sqlDB, err := sql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE gp_users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, email TEXT UNIQUE,
			password_hash TEXT, display_name TEXT, avatar_url TEXT, role TEXT NOT NULL DEFAULT 'subscriber',
			is_active BOOLEAN NOT NULL DEFAULT TRUE, last_login_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE gp_options (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, value TEXT, autoload BOOLEAN NOT NULL DEFAULT 1)`,
		`CREATE TABLE gp_audit_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, username TEXT, action TEXT NOT NULL,
			resource TEXT, resource_id INTEGER, details TEXT, ip_address TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "requires cgo") {
				t.Skip("admin extension tests require CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO gp_users (id, username, password_hash, role) VALUES (1, 'admin', ?, 'super_admin'), (2, 'sso', '', 'super_admin')`, string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	return user.NewRepository(db), audit.NewService(db), db
}

func extensionTestHandler(t *testing.T, f *fakePackages) (*Handler, *audit.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	users, log, db := extensionTestDB(t)
	rbac := user.NewRBAC()
	rbac.RegisterRole(roleSettingsOnly, "Settings", 60, map[string]bool{"setting.read": true, "setting.update": true, "dashboard.read": true})
	svc := &Service{rbac: rbac, userRepo: users, db: db, options: option.NewMemoryStore(map[string]string{"admin_language": "en"})}
	svc.SetAuditService(log)
	h := NewHandler(svc, content.NewRegistry(), filepath.Join("templates"))
	h.SetPackageManager(f.manager())
	return h, log
}

// extensionRouter serves the extension routes as the given role and user.
func extensionRouter(h *Handler, role string, userID uint) *gin.Engine {
	router := gin.New()
	group := router.Group("/admin", func(c *gin.Context) {
		c.Set("admin_role", role)
		c.Set("admin_user_id", userID)
		c.Set("admin_username", "tester")
	})
	group.GET("/extensions/themes", h.ExtensionThemeList)
	group.POST("/extensions/themes/install", h.ExtensionThemeInstall)
	group.POST("/extensions/themes/upload", h.ExtensionThemeUpload)
	group.POST("/extensions/themes/rollback", h.ExtensionThemeRollback)
	group.POST("/extensions/themes/uninstall", h.ExtensionThemeUninstall)
	group.POST("/settings/developer-mode", h.DeveloperModeUpdate)
	return router
}

func postForm(router http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func uploadRequest(t *testing.T, data []byte, confirm string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("package", "aurora-1.0.0.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	if confirm != "" {
		_ = w.WriteField("confirm_digest", confirm)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/extensions/themes/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	if body.OK {
		return ""
	}
	return body.Error.Code
}

func auditActions(t *testing.T, log *audit.Service) []string {
	t.Helper()
	events, err := log.ListRecent(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		out = append(out, e.Action+" "+e.Resource+": "+e.Details)
	}
	return out
}

// Every extension route checks its own capability on the server. Roles
// without it, including one that may change settings, get 403 JSON and
// reach nothing.
func TestExtensionRoutesRequireTheirCapability(t *testing.T) {
	routes := []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "/admin/extensions/themes", nil},
		{http.MethodPost, "/admin/extensions/themes/install", url.Values{"slug": {"aurora"}, "version": {"1.0.0"}}},
		{http.MethodPost, "/admin/extensions/themes/upload", nil},
		{http.MethodPost, "/admin/extensions/themes/rollback", url.Values{"id": {"7"}}},
		{http.MethodPost, "/admin/extensions/themes/uninstall", url.Values{"slug": {"aurora"}}},
		{http.MethodPost, "/admin/settings/developer-mode", url.Values{"enabled": {"0"}}},
	}
	for _, role := range []string{user.RoleEditor, user.RoleAuthor, user.RoleSubscriber, roleSettingsOnly} {
		f := &fakePackages{}
		h, log := extensionTestHandler(t, f)
		router := extensionRouter(h, role, 1)
		for _, route := range routes {
			var rec *httptest.ResponseRecorder
			if route.method == http.MethodGet {
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route.path, nil))
			} else if route.path == "/admin/extensions/themes/upload" {
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, uploadRequest(t, []byte("zip"), ""))
			} else {
				rec = postForm(router, route.path, route.form)
			}
			if rec.Code != http.StatusForbidden || errorCode(t, rec) != "forbidden" {
				t.Errorf("%s %s %s = %d %s, want 403 forbidden", role, route.method, route.path, rec.Code, rec.Body.String())
			}
		}
		if f.callCount() != 0 {
			t.Errorf("%s reached the package manager: %v", role, f.calls)
		}
		if actions := auditActions(t, log); len(actions) != 0 {
			t.Errorf("%s left audit entries: %v", role, actions)
		}
	}

	f := &fakePackages{}
	h, log := extensionTestHandler(t, f)
	router := extensionRouter(h, user.RoleSuperAdmin, 1)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/extensions/themes", nil))
	if rec.Code != http.StatusOK || errorCode(t, rec) != "" {
		t.Fatalf("super_admin list = %d %s", rec.Code, rec.Body.String())
	}
	for _, route := range routes[1:] {
		if route.path == "/admin/extensions/themes/upload" {
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, uploadRequest(t, []byte("zip"), ""))
		} else {
			rec = postForm(router, route.path, route.form)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("super_admin %s = %d %s", route.path, rec.Code, rec.Body.String())
		}
	}
	want := []string{"list", "install aurora 1.0.0", "upload", "rollback", "uninstall aurora", "developer off"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	if got := len(auditActions(t, log)); got != 5 {
		t.Fatalf("audit entries = %d, want one per change: %v", got, auditActions(t, log))
	}
}

func TestDeveloperModeNeedsConfirmationAndPassword(t *testing.T) {
	f := &fakePackages{}
	h, log := extensionTestHandler(t, f)
	router := extensionRouter(h, user.RoleSuperAdmin, 1)
	turnOn := func(form url.Values) *httptest.ResponseRecorder {
		form.Set("enabled", "1")
		return postForm(router, "/admin/settings/developer-mode", form)
	}

	if rec := turnOn(url.Values{"password": {testPassword}}); rec.Code != http.StatusConflict || errorCode(t, rec) != "confirmation_required" {
		t.Fatalf("without confirmation = %d %s", rec.Code, rec.Body.String())
	}
	for i := 0; i < developerModeMaxFailures; i++ {
		if rec := turnOn(url.Values{"confirm": {"1"}, "password": {"wrong"}}); rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "password_incorrect" {
			t.Fatalf("wrong password %d = %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if rec := turnOn(url.Values{"confirm": {"1"}, "password": {testPassword}}); rec.Code != http.StatusTooManyRequests || errorCode(t, rec) != "too_many_attempts" {
		t.Fatalf("after %d failures = %d %s", developerModeMaxFailures, rec.Code, rec.Body.String())
	}
	if f.callCount() != 0 || len(auditActions(t, log)) != 0 {
		t.Fatalf("refused attempts changed something: calls %v, audit %v", f.calls, auditActions(t, log))
	}

	// Another account is counted separately; it has no password to give.
	other := extensionRouter(h, user.RoleSuperAdmin, 2)
	if rec := postForm(other, "/admin/settings/developer-mode", url.Values{"enabled": {"1"}, "confirm": {"1"}, "password": {""}}); errorCode(t, rec) != "password_unavailable" {
		t.Fatalf("account without a password = %d %s", rec.Code, rec.Body.String())
	}

	// A fresh handler: the right password turns it on, and turning it off
	// needs none.
	f = &fakePackages{}
	h, log = extensionTestHandler(t, f)
	router = extensionRouter(h, user.RoleSuperAdmin, 1)
	if rec := turnOn(url.Values{"confirm": {"1"}, "password": {testPassword}}); rec.Code != http.StatusOK || !f.mode.Enabled {
		t.Fatalf("turn on = %d %s", rec.Code, rec.Body.String())
	}
	if rec := postForm(router, "/admin/settings/developer-mode", url.Values{"enabled": {"0"}}); rec.Code != http.StatusOK || f.mode.Enabled {
		t.Fatalf("turn off = %d %s", rec.Code, rec.Body.String())
	}
	actions := strings.Join(auditActions(t, log), "\n")
	if !strings.Contains(actions, "developer mode turned on") || !strings.Contains(actions, "developer mode turned off") {
		t.Fatalf("audit = %s", actions)
	}

	// Locked by config.toml: refused before the password is looked at.
	f = &fakePackages{mode: DeveloperModeInfo{Locked: true}}
	h, _ = extensionTestHandler(t, f)
	router = extensionRouter(h, user.RoleSuperAdmin, 1)
	if rec := turnOn(url.Values{"confirm": {"1"}, "password": {testPassword}}); rec.Code != http.StatusForbidden || errorCode(t, rec) != "developer_mode_locked" {
		t.Fatalf("locked = %d %s", rec.Code, rec.Body.String())
	}
	if f.mode.Enabled {
		t.Fatal("a locked developer mode was turned on")
	}
}

func TestPackageUploadLimitsAndErrors(t *testing.T) {
	f := &fakePackages{}
	h, log := extensionTestHandler(t, f)
	router := extensionRouter(h, user.RoleSuperAdmin, 1)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, uploadRequest(t, make([]byte, maxPackageUploadBytes+1), ""))
	if rec.Code != http.StatusRequestEntityTooLarge || errorCode(t, rec) != "too_large" || f.callCount() != 0 {
		t.Fatalf("oversized upload = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, uploadRequest(t, []byte("zip bytes"), "h1:confirmed"))
	if rec.Code != http.StatusOK || string(f.uploaded) != "zip bytes" || f.confirm != "h1:confirmed" {
		t.Fatalf("upload = %d %s; manager got %q confirm %q", rec.Code, rec.Body.String(), f.uploaded, f.confirm)
	}
	if actions := strings.Join(auditActions(t, log), "\n"); !strings.Contains(actions, "install_unsigned extension: upload theme package aurora 1.0.0 (h1:abc), signature unsigned") {
		t.Fatalf("audit = %s", actions)
	}

	// Errors keep their code, status, problems (capped) and package details.
	problems := make([]PackageProblemView, maxReportedProblems+5)
	for code, status := range map[string]int{
		"confirmation_required": http.StatusConflict,
		"unsigned_not_allowed":  http.StatusForbidden,
		"check_failed":          http.StatusUnprocessableEntity,
		"download_failed":       http.StatusBadGateway,
		"theme_active":          http.StatusConflict,
		"something_new":         http.StatusInternalServerError,
	} {
		f.err = &PackageError{Code: code, Detail: "detail", Problems: problems, Package: &PackageSummaryView{Slug: "aurora", Digest: "h1:x"}}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, uploadRequest(t, []byte("zip"), ""))
		var body struct {
			Error    struct{ Code string }
			Problems []PackageProblemView
			Package  *PackageSummaryView
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != status || body.Error.Code != code || len(body.Problems) != maxReportedProblems || body.Package == nil || body.Package.Digest != "h1:x" {
			t.Errorf("%s = %d %s", code, rec.Code, rec.Body.String())
		}
	}
}

// The developer mode banner is on every admin page while the mode is on and
// cannot be dismissed; only those who may turn it off see the link.
func TestDeveloperModeBanner(t *testing.T) {
	f := &fakePackages{mode: DeveloperModeInfo{Enabled: true}}
	h, _ := extensionTestHandler(t, f)
	page := func(c *gin.Context) { h.render(c, "themes", gin.H{"Title": "Any page"}) }
	for role, link := range map[string]bool{user.RoleSuperAdmin: true, user.RoleEditor: false, roleSettingsOnly: false} {
		body := serveAs(h, role, page).Body.String()
		if !strings.Contains(body, "Developer mode is on:") || !strings.Contains(body, "The site operator bears the risk.") {
			t.Errorf("%s: banner missing", role)
		}
		if got := strings.Contains(body, ">Turn off</a>"); got != link {
			t.Errorf("%s: turn-off link shown = %v, want %v", role, got, link)
		}
	}
	f.mode.Enabled = false
	if body := serveAs(h, user.RoleSuperAdmin, page).Body.String(); strings.Contains(body, "Developer mode is on:") {
		t.Fatal("banner shown while developer mode is off")
	}
}

// The generic settings save cannot create or overwrite options core manages
// through new_key, so it cannot switch themes past their checks, activate
// plugins past plugin.update, or turn developer mode on.
func TestSettingUpdateRefusesCoreOwnedNewKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, key := range []string{"active_theme", "plugin_active_seo", "demo_imported_x", extension.OptionDeveloperMode, " site_name "} {
		store := option.NewMemoryStore(map[string]string{"admin_language": "en"})
		h := &Handler{svc: &Service{options: store, rbac: user.NewRBAC()}}
		router := gin.New()
		router.POST("/admin/settings", func(c *gin.Context) {
			c.Set("admin_role", user.RoleSuperAdmin)
			h.SettingUpdate(c)
		})
		rec := postForm(router, "/admin/settings", url.Values{"new_key": {key}, "new_value": {"1"}})
		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "/admin/settings?error=") {
			t.Errorf("%q: status %d location %q", key, rec.Code, rec.Header().Get("Location"))
		}
		if got := store.Get(strings.TrimSpace(key)); got != "" {
			t.Errorf("%q was written: %q", key, got)
		}
	}
	for key, owned := range map[string]bool{"active_theme": true, "plugin_active_seo": true, extension.OptionDeveloperMode: true, "my_custom_flag": false, "theme_aurora_color": false} {
		if coreOwnedOption(key) != owned {
			t.Errorf("coreOwnedOption(%q) = %v", key, !owned)
		}
	}
	svc := &Service{}
	if !svc.isSettingReadOnly("plugin_active_seo") || svc.isSettingReadOnly("my_custom_flag") {
		t.Fatal("plugin_active_* must be read-only in the settings list, custom options editable")
	}
}

func getAs(h *Handler, role, path string, handle gin.HandlerFunc) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET(strings.SplitN(path, "?", 2)[0], func(c *gin.Context) {
		c.Set("admin_role", role)
		c.Set("admin_user_id", uint(1))
		handle(c)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The market page needs extension.read; its tabs, the upload button and the
// script's texts follow the viewer's capabilities and the market state.
func TestThemeMarketPage(t *testing.T) {
	f := &fakePackages{market: MarketInfo{Enabled: true, Mode: "custom", Host: "market.example.com"}}
	h, _ := extensionTestHandler(t, f)
	for _, role := range []string{user.RoleEditor, roleSettingsOnly} {
		if rec := getAs(h, role, "/admin/themes/market", h.ThemeMarket); rec.Code != http.StatusFound {
			t.Errorf("%s: status %d, want a redirect", role, rec.Code)
		}
	}
	body := getAs(h, user.RoleSuperAdmin, "/admin/themes/market", h.ThemeMarket).Body.String()
	for _, want := range []string{
		`data-tab="browse"`, `data-market-enabled="true"`, `data-can-install="true"`, `data-ext-upload`,
		`aria-current="page">Browse</a>`, "Market: market.example.com", `id="extensionsText"`,
		`"loading_catalog":"Loading the market catalog…"`, `/static/admin/js/extensions.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("market page lacks %q", want)
		}
	}
	installed := getAs(h, user.RoleSuperAdmin, "/admin/themes/market?tab=installed", h.ThemeMarket).Body.String()
	if !strings.Contains(installed, `data-tab="installed"`) || !strings.Contains(installed, `aria-current="page">Installed</a>`) {
		t.Fatal("the installed tab is not selected")
	}

	// A market that is not configured is reported without a request.
	f.market = MarketInfo{Mode: "disabled"}
	disabled := getAs(h, user.RoleSuperAdmin, "/admin/themes/market", h.ThemeMarket).Body.String()
	if !strings.Contains(disabled, "The market is not available") || strings.Contains(disabled, "data-ext-search") {
		t.Fatal("a disabled market must show its state instead of the catalog")
	}
	if f.callCount() != 0 {
		t.Fatalf("rendering the page called %v", f.calls)
	}
}

func TestThemeCatalogEndpoint(t *testing.T) {
	f := &fakePackages{catalog: &CatalogView{Items: []CatalogItemView{{Slug: "aurora", Title: "Aurora"}}}}
	h, _ := extensionTestHandler(t, f)
	for _, role := range []string{user.RoleEditor, roleSettingsOnly} {
		rec := getAs(h, role, "/admin/extensions/themes/catalog", h.ExtensionThemeCatalog)
		if rec.Code != http.StatusForbidden || errorCode(t, rec) != "forbidden" {
			t.Errorf("%s: %d %s", role, rec.Code, rec.Body.String())
		}
	}
	rec := getAs(h, user.RoleSuperAdmin, "/admin/extensions/themes/catalog", h.ExtensionThemeCatalog)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slug":"aurora"`) || f.calls[0] != "catalog en" {
		t.Fatalf("catalog = %d %s, calls %v", rec.Code, rec.Body.String(), f.calls)
	}

	f.err = &PackageError{Code: "catalog_unavailable", Reason: "expired", Detail: "expired"}
	rec = getAs(h, user.RoleSuperAdmin, "/admin/extensions/themes/catalog", h.ExtensionThemeCatalog)
	var body struct {
		Error struct{ Code, Reason string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadGateway || body.Error.Code != "catalog_unavailable" || body.Error.Reason != "expired" {
		t.Fatalf("failed catalog = %d %s", rec.Code, rec.Body.String())
	}
}

// Theme cards show where a package came from, warn about unsigned and
// revoked versions, refuse switching to a revoked one, and list the hosts
// a package loads resources from in the switch confirmation.
func TestThemesPageShowsPackageState(t *testing.T) {
	h, _ := extensionTestHandler(t, &fakePackages{})
	h.SetThemeManager(&ThemeManager{AvailableFn: func() []ThemeDisplayInfo {
		return []ThemeDisplayInfo{
			{Name: "Built In", Slug: "built-in", Version: "1.0.0", Active: true},
			{Name: "Aurora", Slug: "aurora", Version: "1.2.0", Package: true, PackageSource: "upload", Unsigned: true,
				External: []string{"fonts.example.com"}, ExternalSummary: "fonts.example.com",
				UpdateVersion: "1.3.0", UpdateSeverity: "normal"},
			{Name: "Cascade", Slug: "cascade", Version: "1.0.0", Package: true, PackageSource: "market",
				UpdateVersion: "1.0.1", UpdateSeverity: "security"},
			{Name: "Borealis", Slug: "borealis", Version: "2.0.0", Package: true, PackageSource: "market", Revoked: true,
				UpdateVersion: "2.1.0", UpdateSeverity: "unsupported"},
		}
	}})
	body := getAs(h, user.RoleSuperAdmin, "/admin/themes", h.ThemeList).Body.String()
	for _, want := range []string{
		`href="/admin/themes/market" class="btn btn-primary">Theme market</a>`,
		`<span class="badge badge-package">Uploaded</span>`, `<span class="badge badge-danger">Unsigned</span>`,
		`It loads resources from: fonts.example.com.`,
		`<span class="badge badge-package">Market</span>`,
		`btn-blocked" disabled title="The market revoked this version; it cannot be enabled.">Revoked</button>`,
		`href="/admin/themes/market?tab=installed#package-aurora"`,
		`<a class="badge badge-update" href="/admin/themes/market#market-aurora">New version 1.3.0</a>`,
		`<a class="badge badge-update badge-update-urgent" href="/admin/themes/market#market-cascade">Security update 1.0.1</a>`,
		// An unsupported version is as urgent, but the new one is not
		// necessarily a security fix.
		`<a class="badge badge-update badge-update-urgent" href="/admin/themes/market#market-borealis">New version 2.1.0</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("themes page lacks %q", want)
		}
	}
	if strings.Count(body, `name="theme" value="borealis"`) != 0 {
		t.Error("a revoked package can still be switched to")
	}
	// Without extension.read: no market entry and no version links; update
	// badges are plain text.
	other := getAs(h, roleSettingsOnly, "/admin/themes", h.ThemeList).Body.String()
	if strings.Contains(other, "/admin/themes/market") {
		t.Error("a role without extension.read sees market links")
	}
	if !strings.Contains(other, `<span class="badge badge-update badge-update-urgent">Security update 1.0.1</span>`) {
		t.Error("the update badge is missing for a role without extension.read")
	}
}

// Developer options are a separate block for those who may change them,
// with the state, the lock and the right button.
func TestSettingsDeveloperOptions(t *testing.T) {
	f := &fakePackages{}
	h, _ := extensionTestHandler(t, f)
	page := func(role string) string { return getAs(h, role, "/admin/settings", h.SettingList).Body.String() }

	off := page(user.RoleSuperAdmin)
	if !strings.Contains(off, `id="developer-options"`) || !strings.Contains(off, "Developer mode is off") ||
		!strings.Contains(off, "data-ext-devmode-on>Turn on…</button>") || !strings.Contains(off, "extensions.js") {
		t.Fatalf("off state:\n%s", off)
	}
	if other := page(roleSettingsOnly); strings.Contains(other, "developer-options") || strings.Contains(other, "extensions.js") {
		t.Fatal("a role without extension.developer_mode sees developer options")
	}

	f.mode = DeveloperModeInfo{Locked: true}
	locked := page(user.RoleSuperAdmin)
	if !strings.Contains(locked, `data-ext-devmode-on disabled aria-disabled="true"`) || !strings.Contains(locked, "lock_developer_mode") {
		t.Fatal("a locked developer mode can still be turned on from the page")
	}

	f.mode = DeveloperModeInfo{Enabled: true}
	on := page(user.RoleSuperAdmin)
	if !strings.Contains(on, "Developer mode is on") || !strings.Contains(on, "data-ext-devmode-off>Turn off</button>") ||
		!strings.Contains(on, `href="/admin/settings#developer-options"`) {
		t.Fatal("on state or banner link missing")
	}
}

// The revoked-theme banner is on every admin page for those who can open
// the themes page, and cannot be dismissed.
func TestRevokedThemeBanner(t *testing.T) {
	f := &fakePackages{revoked: &RevokedThemeBanner{Slug: "aurora", Version: "1.2.0"}}
	h, _ := extensionTestHandler(t, f)
	page := func(c *gin.Context) { h.render(c, "themes", gin.H{"Title": "Any page"}) }
	body := serveAs(h, user.RoleSuperAdmin, page).Body.String()
	if !strings.Contains(body, "Theme revoked:") || !strings.Contains(body, `the theme in use, &#34;aurora&#34; 1.2.0`) ||
		strings.Contains(body, "dep-warning-close") {
		t.Fatalf("banner:\n%s", body)
	}
	if body := serveAs(h, user.RoleAuthor, page).Body.String(); strings.Contains(body, "Theme revoked:") {
		t.Fatal("a role without setting.read sees the revoked-theme banner")
	}
	f.revoked = nil
	if body := serveAs(h, user.RoleSuperAdmin, page).Body.String(); strings.Contains(body, "Theme revoked:") {
		t.Fatal("banner shown without a revoked theme")
	}
}

// /admin/themes/market sits next to /admin/themes/:slug/settings: a theme
// whose slug is "market" still reaches its settings page.
func TestMarketRouteLeavesThemeSettingsReachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/admin/themes/market", func(c *gin.Context) { c.String(http.StatusOK, "market") })
	router.GET("/admin/themes/:slug/settings", func(c *gin.Context) { c.String(http.StatusOK, "settings "+c.Param("slug")) })
	for path, want := range map[string]string{"/admin/themes/market": "market", "/admin/themes/market/settings": "settings market"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Body.String() != want {
			t.Errorf("%s = %d %q, want %q", path, rec.Code, rec.Body.String(), want)
		}
	}
}
