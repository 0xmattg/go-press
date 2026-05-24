package installer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/config"
)

func TestInstallerWelcomePageRenders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/install", nil)
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Choose Setup Language") {
		t.Fatalf("body does not contain language heading: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "English") || !strings.Contains(rec.Body.String(), "简体中文") {
		t.Fatalf("body does not contain language choices: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "WordPress") {
		t.Fatalf("welcome page should not contain WordPress copy: %s", rec.Body.String())
	}
}

func TestInstallerLanguageSubmitSetsCookieAndRedirects(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/install/language", strings.NewReader("language=zh-CN"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); location != "/install/database" {
		t.Fatalf("Location = %q, want /install/database", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != installerLanguageCookie || cookies[0].Value != "zh-CN" {
		t.Fatalf("language cookie not set correctly: %#v", cookies)
	}
}

func TestInstallerSitePageRedirectsWithoutDatabaseStep(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/install/site", nil)
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "/install/database" {
		t.Fatalf("Location = %q, want /install/database", location)
	}
}

func TestInstallerDatabaseSubmitValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	initial := &config.Config{}
	inst := New(filepath.Join(t.TempDir(), "config.toml"), initial, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/install/database", strings.NewReader("database=&user="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "Fill in the PostgreSQL connection details") {
		t.Fatalf("body does not contain validation error: %s", rec.Body.String())
	}
}

func TestInstallerDatabasePageShowsSeparateTestAndContinueButtons(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), &config.Config{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/install/database", nil)
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Test Database Connection") {
		t.Fatalf("database page missing test button: %s", body)
	}
	if !strings.Contains(body, ">Continue<") {
		t.Fatalf("database page missing continue button: %s", body)
	}
}

func TestInstallerDatabasePageUsesSelectedChineseLanguage(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), &config.Config{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/install/database", nil)
	req.AddCookie(&http.Cookie{Name: installerLanguageCookie, Value: "zh-CN"})
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "测试数据库连接") {
		t.Fatalf("database page missing Chinese test button: %s", body)
	}
	if !strings.Contains(body, ">继续<") {
		t.Fatalf("database page missing Chinese continue button: %s", body)
	}
}

func TestInstallerSitePageCarriesSelectedAdminLanguage(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	inst := New(configPath, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/install/site", nil)
	req.AddCookie(&http.Cookie{Name: installerLanguageCookie, Value: "zh-CN"})
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="admin_language" value="zh-CN"`) {
		t.Fatalf("site page does not carry selected admin language: %s", body)
	}
	if !strings.Contains(body, `<option value="zh-CN" selected>`) {
		t.Fatalf("site language does not default to selected language: %s", body)
	}
	if !strings.Contains(body, `name="timezone"`) {
		t.Fatalf("site page does not render timezone selector: %s", body)
	}
}

func TestInstallerDatabaseTestValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	inst := New(filepath.Join(t.TempDir(), "config.toml"), &config.Config{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/install/database/test", strings.NewReader("database=&user="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	inst.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["ok"] != false {
		t.Fatalf("payload ok = %v, want false", payload["ok"])
	}
	if payload["error"] != "Fill in the PostgreSQL connection details." {
		t.Fatalf("payload error = %v", payload["error"])
	}
}

func TestSiteDirNameFromURL(t *testing.T) {
	t.Parallel()

	name, err := siteDirNameFromURL("https://www.example-site.com:8443/news")
	if err != nil {
		t.Fatalf("siteDirNameFromURL() error = %v", err)
	}
	if name != "www-example-site-com" {
		t.Fatalf("siteDirNameFromURL() = %q, want %q", name, "www-example-site-com")
	}
}

func TestPrepareSiteConfigPathRenamesDefaultDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	defaultDir := filepath.Join(root, "sites", "default")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	configPath := filepath.Join(defaultDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\nname='demo'\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	inst := New(configPath, nil, nil, nil)
	got, err := inst.prepareSiteConfigPath("https://demo.example.com")
	if err != nil {
		t.Fatalf("prepareSiteConfigPath() error = %v", err)
	}

	want := filepath.Join(root, "sites", "demo-example-com", "config.toml")
	if got != want {
		t.Fatalf("prepareSiteConfigPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected renamed config file at %q: %v", want, err)
	}
	if _, err := os.Stat(defaultDir); !os.IsNotExist(err) {
		t.Fatalf("expected default dir to be renamed away, stat err = %v", err)
	}
}
