package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func agentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
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
		`CREATE TABLE gp_contents (
			id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL, status TEXT NOT NULL,
			title TEXT NOT NULL, slug TEXT NOT NULL, content TEXT, excerpt TEXT, image_url TEXT,
			author_id INTEGER, parent_id INTEGER, sort_order INTEGER DEFAULT 0,
			comment_status TEXT DEFAULT 'open', published_at DATETIME,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE gp_content_meta (
			id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER NOT NULL,
			meta_key TEXT NOT NULL, meta_value TEXT
		)`,
		`CREATE TABLE gp_terms (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE gp_taxonomies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, term_id INTEGER NOT NULL, taxonomy TEXT NOT NULL,
			description TEXT, parent_id INTEGER, count INTEGER DEFAULT 0
		)`,
		`CREATE TABLE gp_term_relationships (
			content_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, sort_order INTEGER DEFAULT 0,
			PRIMARY KEY (content_id, taxonomy_id)
		)`,
		`CREATE TABLE gp_media (
			id INTEGER PRIMARY KEY AUTOINCREMENT, filename TEXT NOT NULL, original_name TEXT,
			mime_type TEXT, size INTEGER, path TEXT NOT NULL, alt_text TEXT, title TEXT,
			caption TEXT, width INTEGER, height INTEGER, uploaded_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("agent tests require CGO-backed SQLite")
			}
			t.Fatalf("prepare agent schema: %v", err)
		}
	}
	return db
}

func testPrincipal(role string, scopes ...string) Principal {
	return Principal{
		Kind: PrincipalUser, SubjectID: 1, Username: "alice", Role: role,
		Scopes: NormalizeScopes(scopes), Audience: "https://example.test/agent", CredentialID: 10,
	}
}

type fixedPrincipalValidator struct {
	principal Principal
	err       error
}

func (v fixedPrincipalValidator) ValidatePrincipal(_ context.Context, _ Principal) (Principal, error) {
	if v.err != nil {
		return Principal{}, v.err
	}
	return v.principal, nil
}

type memoryAuditRecorder struct {
	mu         sync.Mutex
	events     []AuditEvent
	failStatus string
}

func (r *memoryAuditRecorder) Record(_ context.Context, event *AuditEvent) error {
	if event == nil {
		return errors.New("nil event")
	}
	if r.failStatus != "" && event.Status == r.failStatus {
		return errors.New("audit offline")
	}
	r.mu.Lock()
	r.events = append(r.events, *event)
	r.mu.Unlock()
	return nil
}

func (r *memoryAuditRecorder) statuses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := make([]string, len(r.events))
	for index := range r.events {
		statuses[index] = r.events[index].Status
	}
	return statuses
}

func testReadTool(name string, requirement PermissionRequirement, handler Handler) Tool {
	return Tool{
		Name: name, Title: "Test read tool", Description: "Read a value for executor tests.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string","maxLength":20}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`),
		Mutability:   MutabilityRead, Risk: RiskRead, Permission: requirement, Handler: handler,
	}
}

func insertTestUser(t *testing.T, db *gorm.DB, role string) user.User {
	t.Helper()
	account := user.User{Username: "alice", Role: role, IsActive: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	return account
}
