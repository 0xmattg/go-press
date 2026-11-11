// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package installer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/config"
)

func tokenInstaller(t *testing.T, bootErr error) (*Installer, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "sites", "default", "config.toml")
	cfg := &config.Config{}
	cfg.Install.RequireToken = true
	return New(configPath, cfg, bootErr, nil), configPath
}

func postLanguage(inst *Installer, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/install/language", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	inst.Router().ServeHTTP(rec, req)
	return rec
}

func TestInstallTokenIsOffByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, cfg := range map[string]*config.Config{"no config": nil, "empty config": {}} {
		dir := t.TempDir()
		inst := New(filepath.Join(dir, "config.toml"), cfg, nil, nil)

		rec := httptest.NewRecorder()
		inst.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/install", nil))
		if strings.Contains(rec.Body.String(), `name="install_token"`) {
			t.Fatalf("%s: token field shown although require_token is off", name)
		}
		if _, err := os.Stat(filepath.Join(dir, InstallTokenFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: token file written although require_token is off (err=%v)", name, err)
		}
		if res := postLanguage(inst, url.Values{"language": {"en"}, "beta_notice_accepted": {"1"}}); res.Code != http.StatusSeeOther {
			t.Fatalf("%s: language submit status = %d, want %d", name, res.Code, http.StatusSeeOther)
		}
	}
}

func TestInstallTokenIsGeneratedAndSavedPrivately(t *testing.T) {
	inst, configPath := tokenInstaller(t, nil)
	if !regexp.MustCompile(`^[A-Z2-7]{4}(-[A-Z2-7]{4}){5}$`).MatchString(inst.installToken) {
		t.Fatalf("token %q is not six groups of four base32 characters", inst.installToken)
	}
	path := filepath.Join(filepath.Dir(configPath), InstallTokenFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != inst.installToken {
		t.Fatalf("token file = %q, want %q", data, inst.installToken)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}

	other, _ := tokenInstaller(t, nil)
	if other.installToken == inst.installToken {
		t.Fatal("two installers produced the same token")
	}
}

func TestLanguageSubmitRequiresTheInstallToken(t *testing.T) {
	inst, _ := tokenInstaller(t, nil)

	welcome := httptest.NewRecorder()
	inst.Router().ServeHTTP(welcome, httptest.NewRequest(http.MethodGet, "/install", nil))
	if !strings.Contains(welcome.Body.String(), `name="install_token"`) {
		t.Fatal("welcome page must ask for the install token")
	}

	for name, token := range map[string]string{"missing": "", "wrong": "AAAA-AAAA-AAAA-AAAA-AAAA-AAAA"} {
		rec := postLanguage(inst, url.Values{"language": {"en"}, "beta_notice_accepted": {"1"}, "install_token": {token}})
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "install token is missing or incorrect") {
			t.Fatalf("%s token: status=%d body=%s", name, rec.Code, rec.Body.String())
		}
		if cookieValue(rec.Result().Cookies(), installerPolicyCookie) != "" {
			t.Fatalf("%s token: policy cookie issued", name)
		}
		if strings.Contains(rec.Body.String(), inst.installToken) {
			t.Fatalf("%s token: response leaked the real token", name)
		}
	}

	direct := httptest.NewRecorder()
	inst.Router().ServeHTTP(direct, httptest.NewRequest(http.MethodGet, "/install/database", nil))
	if direct.Code != http.StatusFound || direct.Header().Get("Location") != "/install" {
		t.Fatalf("later steps must stay gated: status=%d location=%q", direct.Code, direct.Header().Get("Location"))
	}

	// The beta notice is still required, and the form keeps asking for the token.
	noNotice := postLanguage(inst, url.Values{"language": {"en"}, "install_token": {inst.installToken}})
	if noNotice.Code != http.StatusBadRequest || !strings.Contains(noNotice.Body.String(), `name="install_token"`) {
		t.Fatalf("missing notice: status=%d", noNotice.Code)
	}

	// Typed by hand: lower case, spaces instead of hyphens.
	typed := strings.ToLower(strings.ReplaceAll(inst.installToken, "-", " "))
	ok := postLanguage(inst, url.Values{"language": {"en"}, "beta_notice_accepted": {"1"}, "install_token": {typed}})
	if ok.Code != http.StatusSeeOther || cookieValue(ok.Result().Cookies(), installerPolicyCookie) == "" {
		t.Fatalf("correct token: status=%d cookies=%v", ok.Code, ok.Result().Cookies())
	}
}

func TestBootErrorIsHiddenUntilPastTheFirstScreen(t *testing.T) {
	inst, _ := tokenInstaller(t, errors.New("dial tcp 10.0.0.5:5432: connection refused"))

	welcome := httptest.NewRecorder()
	inst.Router().ServeHTTP(welcome, httptest.NewRequest(http.MethodGet, "/install", nil))
	if strings.Contains(welcome.Body.String(), "10.0.0.5") {
		t.Fatal("the first screen must not show connection details to anyone who finds /install")
	}

	req := httptest.NewRequest(http.MethodGet, "/install/database", nil)
	addPolicyCookie(req, inst)
	database := httptest.NewRecorder()
	inst.Router().ServeHTTP(database, req)
	if !strings.Contains(database.Body.String(), "10.0.0.5") {
		t.Fatal("the database step should still explain why the site did not start")
	}
}

func TestInstallTokenIsRetiredWithTheSiteDirectory(t *testing.T) {
	inst, _ := tokenInstaller(t, nil)
	token := inst.installToken

	// Finishing the installer renames sites/default; the token file moves
	// with it and is removed from the new place.
	finalPath, err := inst.prepareSiteConfigPath("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	inst.configPath = finalPath
	moved := filepath.Join(filepath.Dir(finalPath), InstallTokenFile)
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("token file did not move with the site directory: %v", err)
	}

	inst.removeInstallToken()
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file still present after install: %v", err)
	}
	if inst.validInstallToken(token) {
		t.Fatal("the token must stop working once the site is installed")
	}
}
