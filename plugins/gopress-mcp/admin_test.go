package gopressmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/agent"
)

func TestPluginMetadataLifecycleAndSettingsTemplate(t *testing.T) {
	fixture := newTestFixture(t)
	plugin := New()
	if plugin.Name() != PluginName || plugin.Version() == "" || !plugin.DefaultInactive() || plugin.SettingsPermissionResource() != "mcp" {
		t.Fatalf("plugin metadata name=%q version=%q inactive=%v permission=%q", plugin.Name(), plugin.Version(), plugin.DefaultInactive(), plugin.SettingsPermissionResource())
	}
	if !strings.HasSuffix(plugin.SettingsTemplatePath(), "gopress-mcp/templates/admin/settings.tmpl") {
		t.Fatalf("settings template=%q", plugin.SettingsTemplatePath())
	}
	if _, err := template.New("mcp-settings").Funcs(template.FuncMap{
		"X": func(_ any, _ string, fallback string, args ...any) string {
			if len(args) == 0 {
				return fallback
			}
			return fmt.Sprintf(fallback, args...)
		},
	}).ParseFiles("templates/admin/settings.tmpl"); err != nil {
		t.Fatalf("parse settings template: %v", err)
	}
	catalogs := make([]map[string]string, 0, 2)
	for _, path := range []string{"locales/admin/zh-CN.json", "locales/admin/en.json"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if json.Unmarshal(raw, &catalog) != nil || catalog["plugin.name"] == "" {
			t.Fatalf("invalid locale catalog %s", path)
		}
		catalogs = append(catalogs, catalog)
	}
	zhKeys := make([]string, 0, len(catalogs[0]))
	enKeys := make([]string, 0, len(catalogs[1]))
	for key := range catalogs[0] {
		zhKeys = append(zhKeys, key)
	}
	for key := range catalogs[1] {
		enKeys = append(enKeys, key)
	}
	slices.Sort(zhKeys)
	slices.Sort(enKeys)
	if !reflect.DeepEqual(zhKeys, enKeys) {
		t.Fatalf("locale catalogs have different keys: zh=%v en=%v", zhKeys, enKeys)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	plugin.Activate(fixture.host)
	fixture.host.hooks.DoAction(context.Background(), "routes.register", router)
	settings := plugin.SettingsData()
	if settings["EndpointURL"] != "https://example.test/mcp" || settings["SDKVersion"] != MCPGoSDKVersion {
		t.Fatalf("settings data=%+v", settings)
	}
	if endpointURL("") != "" || endpointURL("example.test") != "" || endpointUsesSafeTransport("http://public.test/mcp") {
		t.Fatal("invalid or insecure endpoint validation succeeded")
	}

	plugin.Deactivate(fixture.host)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("deactivated endpoint status=%d", recorder.Code)
	}
	secondRouter := gin.New()
	fixture.host.hooks.DoAction(context.Background(), "routes.register", secondRouter)
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	recorder = httptest.NewRecorder()
	secondRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("removed route hook status=%d", recorder.Code)
	}
}

func TestAdminRoutesEnforceDedicatedRBACSameOriginAndOwnership(t *testing.T) {
	fixture := newTestFixture(t)
	plugin, router := activatedTestPlugin(t, fixture)
	defer plugin.Deactivate(fixture.host)
	subscriberToken := fixture.adminToken(t, fixture.subscriber)

	denied := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/admin/plugins/gopress-mcp/credentials", ""},
		{http.MethodPost, "/admin/plugins/gopress-mcp/credentials", `{}`},
		{http.MethodPost, "/admin/plugins/gopress-mcp/credentials/1/revoke", `{}`},
		{http.MethodGet, "/admin/plugins/gopress-mcp/audit", ""},
		{http.MethodGet, "/admin/plugins/gopress-mcp/diagnostics", ""},
		{http.MethodGet, "/admin/plugins/gopress-mcp/policy", ""},
		{http.MethodPost, "/admin/plugins/gopress-mcp/policy", `{}`},
	}
	for _, test := range denied {
		recorder := performAdminRequest(t, router, test.method, test.path, test.body, subscriberToken, "https://example.test")
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s status=%d body=%s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}

	superToken := fixture.adminToken(t, fixture.superAdmin)
	crossOrigin := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/credentials", validIssueBody(), superToken, "https://evil.test")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin issue status=%d body=%s", crossOrigin.Code, crossOrigin.Body.String())
	}
	trailingJSON := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/credentials", validIssueBody()+`{}`, superToken, "https://example.test")
	if trailingJSON.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d body=%s", trailingJSON.Code, trailingJSON.Body.String())
	}
	issuedResponse := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/credentials", validIssueBody(), superToken, "https://example.test")
	if issuedResponse.Code != http.StatusCreated || issuedResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("issue status=%d headers=%v body=%s", issuedResponse.Code, issuedResponse.Header(), issuedResponse.Body.String())
	}
	var issued struct {
		ID       uint     `json:"id"`
		Token    string   `json:"token"`
		Audience string   `json:"audience"`
		Scopes   []string `json:"scopes"`
	}
	decodeRecorderJSON(t, issuedResponse, &issued)
	if issued.ID == 0 || issued.Token == "" || issued.Audience != "https://example.test/mcp" || len(issued.Scopes) != 2 {
		t.Fatalf("issued credential=%+v", issued)
	}
	listed := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/credentials", "", superToken, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":`+fmt.Sprint(issued.ID)) {
		t.Fatalf("credential list status=%d body=%s", listed.Code, listed.Body.String())
	}

	foreign := fixture.issueToken(t, fixture.editor, "https://example.test/mcp", agent.ScopeSiteRead)
	foreignRevoke := performAdminRequest(t, router, http.MethodPost, fmt.Sprintf("/admin/plugins/gopress-mcp/credentials/%d/revoke", foreign.Credential.ID), `{}`, superToken, "https://example.test")
	if foreignRevoke.Code != http.StatusNotFound {
		t.Fatalf("foreign revoke status=%d body=%s", foreignRevoke.Code, foreignRevoke.Body.String())
	}
	if _, err := fixture.host.credentials.Authenticate(context.Background(), foreign.Token, foreign.Credential.Audience); err != nil {
		t.Fatalf("IDOR attempt changed foreign token: %v", err)
	}
	ownedRevoke := performAdminRequest(t, router, http.MethodPost, fmt.Sprintf("/admin/plugins/gopress-mcp/credentials/%d/revoke", issued.ID), `{}`, superToken, "https://example.test")
	if ownedRevoke.Code != http.StatusNoContent {
		t.Fatalf("owned revoke status=%d body=%s", ownedRevoke.Code, ownedRevoke.Body.String())
	}
	if _, err := fixture.host.credentials.Authenticate(context.Background(), issued.Token, issued.Audience); !agent.IsErrorCode(err, agent.CodeUnauthenticated) {
		t.Fatalf("revoked token error=%v", err)
	}

	diagnostics := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/diagnostics", "", superToken, "")
	if diagnostics.Code != http.StatusOK || !strings.Contains(diagnostics.Body.String(), `"secure_transport":true`) {
		t.Fatalf("diagnostics status=%d body=%s", diagnostics.Code, diagnostics.Body.String())
	}
	audit := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/audit?page=1&limit=50", "", superToken, "")
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), `"events":[]`) {
		t.Fatalf("audit status=%d body=%s", audit.Code, audit.Body.String())
	}
	invalidAudit := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/audit?status=not-a-status", "", superToken, "")
	if invalidAudit.Code != http.StatusBadRequest {
		t.Fatalf("invalid audit filter status=%d body=%s", invalidAudit.Code, invalidAudit.Body.String())
	}
	invalidCredentialFilter := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/audit?credential_id=invalid", "", superToken, "")
	if invalidCredentialFilter.Code != http.StatusBadRequest {
		t.Fatalf("invalid credential filter status=%d body=%s", invalidCredentialFilter.Code, invalidCredentialFilter.Body.String())
	}
}

func TestPolicyAdminRouteDefaultsReadOnlyPersistsAndGatesWriteScopes(t *testing.T) {
	fixture := newTestFixture(t)
	plugin, router := activatedTestPlugin(t, fixture)
	defer plugin.Deactivate(fixture.host)
	token := fixture.adminToken(t, fixture.superAdmin)

	current := performAdminRequest(t, router, http.MethodGet, "/admin/plugins/gopress-mcp/policy", "", token, "")
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), `"profile":"read_only"`) {
		t.Fatalf("default policy status=%d body=%s", current.Code, current.Body.String())
	}
	writeScopeBefore := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/credentials", `{"name":"write","scopes":["gopress:content:write"],"expires_in_days":7}`, token, "https://example.test")
	if writeScopeBefore.Code != http.StatusBadRequest {
		t.Fatalf("disabled write scope status=%d body=%s", writeScopeBefore.Code, writeScopeBefore.Body.String())
	}
	crossOrigin := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/policy", `{"profile":"safe_write","enabled_write_tools":["gopress.content.create_draft"]}`, token, "https://evil.test")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin policy status=%d body=%s", crossOrigin.Code, crossOrigin.Body.String())
	}
	invalid := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/policy", `{"profile":"safe_write","enabled_write_tools":["gopress.system.shell"]}`, token, "https://example.test")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid tool status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	saved := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/policy", `{"profile":"safe_write","enabled_write_tools":["gopress.content.create_draft","gopress.content.create_draft"]}`, token, "https://example.test")
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), agent.ScopeContentWrite) {
		t.Fatalf("save policy status=%d body=%s", saved.Code, saved.Body.String())
	}
	snapshot := fixture.host.policy.Snapshot()
	if snapshot.Profile != agent.ProfileSafeWrite || len(snapshot.EnabledWriteTools) != 1 || fixture.host.options.Get(agent.OptionToolProfile) != string(agent.ProfileSafeWrite) {
		t.Fatalf("runtime=%+v persisted=%q", snapshot, fixture.host.options.Get(agent.OptionToolProfile))
	}
	plugin.Deactivate(fixture.host)
	if fixture.host.policy.Snapshot().Profile != agent.ProfileReadOnly {
		t.Fatal("deactivation must return runtime policy to read-only")
	}
	plugin.Activate(fixture.host)
	if fixture.host.policy.Snapshot().Profile != agent.ProfileSafeWrite {
		t.Fatal("activation did not reload persisted policy")
	}
	writeScopeAfter := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/credentials", `{"name":"write","scopes":["gopress:content:write"],"expires_in_days":7}`, token, "https://example.test")
	if writeScopeAfter.Code != http.StatusCreated {
		t.Fatalf("enabled write scope status=%d body=%s", writeScopeAfter.Code, writeScopeAfter.Body.String())
	}
	readOnly := performAdminRequest(t, router, http.MethodPost, "/admin/plugins/gopress-mcp/policy", `{"profile":"read_only","enabled_write_tools":["gopress.content.create_draft"]}`, token, "https://example.test")
	if readOnly.Code != http.StatusOK || len(fixture.host.policy.Snapshot().EnabledWriteTools) != 0 {
		t.Fatalf("read-only reset status=%d body=%s", readOnly.Code, readOnly.Body.String())
	}
}

func activatedTestPlugin(t *testing.T, fixture *testFixture) (*Plugin, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	plugin := New()
	router := gin.New()
	plugin.Activate(fixture.host)
	fixture.host.hooks.DoAction(context.Background(), "routes.register", router)
	return plugin, router
}

func performAdminRequest(t *testing.T, router http.Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://example.test"+path, bytes.NewBufferString(body))
	request.Host = "example.test"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func validIssueBody() string {
	return `{"name":"Codex local","scopes":["gopress:site:read","gopress:content:read"],"expires_in_days":7}`
}
