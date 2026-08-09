package gopressmcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"
)

type testHost struct {
	registry    *agent.Registry
	executor    *agent.Executor
	credentials *agent.CredentialService
	audit       *agent.AuditStore
	policy      *agent.Policy
	options     *option.Store
	hooks       *hook.Bus
	auth        *user.Auth
	rbac        *user.RBAC
	siteURL     string
}

func (h *testHost) AgentToolRegistry() *agent.Registry               { return h.registry }
func (h *testHost) AgentExecutor() *agent.Executor                   { return h.executor }
func (h *testHost) AgentCredentialService() *agent.CredentialService { return h.credentials }
func (h *testHost) AgentAuditStore() *agent.AuditStore               { return h.audit }
func (h *testHost) AgentToolPolicy() *agent.Policy                   { return h.policy }
func (h *testHost) OptionsStore() *option.Store                      { return h.options }
func (h *testHost) HookBus() *hook.Bus                               { return h.hooks }
func (h *testHost) PublicSiteURL() string                            { return h.siteURL }
func (h *testHost) AdminAuth() *user.Auth                            { return h.auth }
func (h *testHost) RBACManager() *user.RBAC                          { return h.rbac }

type testFixture struct {
	db         *gorm.DB
	host       *testHost
	superAdmin user.User
	editor     user.User
	subscriber user.User
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()
	db := mcpTestDB(t)
	rbac := user.NewRBAC()
	accounts := []user.User{
		{Username: "root", DisplayName: "Root", Role: user.RoleSuperAdmin, IsActive: true},
		{Username: "editor", DisplayName: "Editor", Role: user.RoleEditor, IsActive: true},
		{Username: "reader", DisplayName: "Reader", Role: user.RoleSubscriber, IsActive: true},
	}
	for index := range accounts {
		if err := db.Create(&accounts[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	credentials := agent.NewCredentialService(db, rbac)
	audit := agent.NewAuditStore(db)
	registry := agent.NewRegistry()
	registerTestTool(t, registry, "test.site.get", agent.ScopeSiteRead, "dashboard", map[string]any{"site": "example"})
	registerTestTool(t, registry, "test.content.list", agent.ScopeContentRead, "content", map[string]any{"items": []any{}})
	executor := agent.NewExecutor(registry, credentials, agent.NewAuthorizer(rbac), agent.NewIdempotencyStore(db), audit)
	policy := agent.NewPolicy()
	executor.SetRiskPolicy(policy)
	host := &testHost{
		registry: registry, executor: executor, credentials: credentials, audit: audit,
		policy: policy, options: option.NewMemoryStore(nil),
		hooks: hook.New(), auth: user.NewAuth("mcp-admin-test-secret", 1, nil), rbac: rbac,
		siteURL: "https://example.test",
	}
	return &testFixture{db: db, host: host, superAdmin: accounts[0], editor: accounts[1], subscriber: accounts[2]}
}

func registerTestTool(t *testing.T, registry *agent.Registry, name, scope, resource string, output map[string]any) {
	t.Helper()
	outputSchema := json.RawMessage(`{"type":"object","properties":{"site":{"type":"string"},"items":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`)
	if _, err := registry.Register("test", agent.Tool{
		Name: name, Title: name, Description: "A deterministic read-only test tool.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		OutputSchema: outputSchema, Mutability: agent.MutabilityRead, Risk: agent.RiskRead,
		Permission: agent.PermissionRequirement{Scope: scope, Resource: resource, Action: "read"},
		Handler:    func(context.Context, agent.Invocation) (any, error) { return output, nil },
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *testFixture) issueToken(t *testing.T, account user.User, audience string, scopes ...string) *agent.IssuedCredential {
	t.Helper()
	issued, err := f.host.credentials.Issue(context.Background(), agent.CreateCredentialInput{
		SubjectKind: agent.PrincipalUser, SubjectID: account.ID, Name: "test token",
		Scopes: scopes, Audience: audience, ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: account.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func (f *testFixture) adminToken(t *testing.T, account user.User) string {
	t.Helper()
	token, err := f.host.auth.GenerateToken(&account)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func mcpTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE gp_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, email TEXT,
			password_hash TEXT, display_name TEXT, avatar_url TEXT, role TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE, last_login_at DATETIME,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE gp_agent_service_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, role TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE, created_by INTEGER,
			created_at DATETIME, updated_at DATETIME, disabled_at DATETIME
		)`,
		`CREATE TABLE gp_agent_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_kind TEXT NOT NULL, subject_id INTEGER NOT NULL,
			name TEXT NOT NULL, token_prefix TEXT NOT NULL, secret_hash TEXT NOT NULL UNIQUE,
			scopes TEXT NOT NULL, audience TEXT NOT NULL, expires_at DATETIME NOT NULL,
			last_used_at DATETIME, revoked_at DATETIME, created_by INTEGER, created_at DATETIME
		)`,
		`CREATE TABLE gp_agent_idempotency_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT, credential_id INTEGER NOT NULL, tool_name TEXT NOT NULL,
			idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, status TEXT NOT NULL,
			resource_type TEXT, resource_id INTEGER, result_json TEXT, result_hash TEXT,
			error_code TEXT, error_message TEXT, expires_at DATETIME NOT NULL,
			created_at DATETIME, updated_at DATETIME,
			UNIQUE (credential_id, tool_name, idempotency_key)
		)`,
		`CREATE TABLE gp_agent_audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT NOT NULL, trace_id TEXT,
			adapter TEXT, protocol TEXT, client_version TEXT, principal_kind TEXT,
			subject_id INTEGER, username TEXT, credential_id INTEGER, tool_name TEXT,
			tool_owner TEXT, risk TEXT, status TEXT NOT NULL, error_code TEXT,
			duration_ms INTEGER, arguments_summary TEXT, result_hash TEXT,
			source_digest TEXT, user_agent TEXT, created_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("gopress-mcp tests require CGO-backed SQLite")
			}
			t.Fatalf("prepare MCP test schema: %v", err)
		}
	}
	return db
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (transport bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
