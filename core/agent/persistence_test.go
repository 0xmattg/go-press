package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/user"
)

func TestCredentialLifecycleRefreshesCurrentUserRoleAndRevocation(t *testing.T) {
	db := agentTestDB(t)
	rbac := user.NewRBAC()
	account := insertTestUser(t, db, user.RoleEditor)
	service := NewCredentialService(db, rbac)
	service.now = func() time.Time { return time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC) }
	issued, err := service.Issue(context.Background(), CreateCredentialInput{
		SubjectKind: PrincipalUser, SubjectID: account.ID, Name: "Local agent",
		Scopes:   []string{scopeContentRead, scopeContentRead, scopeSiteRead},
		Audience: "https://example.test/agent", CreatedBy: account.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Token == "" || issued.Credential.SecretHash == issued.Token || issued.Credential.TokenPrefix == issued.Token {
		t.Fatalf("credential secret storage is unsafe: %+v", issued.Credential)
	}
	if _, err := service.Authenticate(context.Background(), issued.Token, "https://wrong.test/agent"); !IsErrorCode(err, CodeUnauthenticated) {
		t.Fatalf("wrong audience error=%v", err)
	}
	principal, stored, err := service.AuthenticateWithCredential(context.Background(), issued.Token, "https://example.test/agent")
	if err != nil {
		t.Fatal(err)
	}
	if principal.Role != user.RoleEditor || !principal.HasScope(scopeContentRead) || principal.CredentialID != issued.Credential.ID ||
		!stored.ExpiresAt.Equal(issued.Credential.ExpiresAt) {
		t.Fatalf("authenticated principal=%+v", principal)
	}
	if err := db.Model(&user.User{}).Where("id = ?", account.ID).Update("role", user.RoleSubscriber).Error; err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.ValidatePrincipal(context.Background(), principal)
	if err != nil || refreshed.Role != user.RoleSubscriber {
		t.Fatalf("refreshed principal=%+v err=%v", refreshed, err)
	}
	if err := service.Revoke(context.Background(), issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ValidatePrincipal(context.Background(), refreshed); !IsErrorCode(err, CodeUnauthenticated) {
		t.Fatalf("revoked credential error=%v", err)
	}
}

func TestCredentialSubjectListingAndOwnedRevocationPreventIDOR(t *testing.T) {
	db := agentTestDB(t)
	first := insertTestUser(t, db, user.RoleEditor)
	second := user.User{Username: "bob", Role: user.RoleEditor, IsActive: true}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	service := NewCredentialService(db, user.NewRBAC())
	issued, err := service.Issue(context.Background(), CreateCredentialInput{
		SubjectKind: PrincipalUser, SubjectID: first.ID, Name: "Owned token",
		Scopes: []string{ScopeSiteRead}, Audience: "https://example.test/mcp", CreatedBy: first.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := service.ListForSubject(context.Background(), PrincipalUser, first.ID, 10)
	if err != nil || len(credentials) != 1 || credentials[0].ID != issued.Credential.ID {
		t.Fatalf("credentials=%+v err=%v", credentials, err)
	}
	if err := service.RevokeForSubject(context.Background(), issued.Credential.ID, PrincipalUser, second.ID); !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("foreign revocation error=%v", err)
	}
	if _, err := service.Authenticate(context.Background(), issued.Token, issued.Credential.Audience); err != nil {
		t.Fatalf("foreign revocation changed credential: %v", err)
	}
	if err := service.RevokeForSubject(context.Background(), issued.Credential.ID, PrincipalUser, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), issued.Token, issued.Credential.Audience); !IsErrorCode(err, CodeUnauthenticated) {
		t.Fatalf("owned revocation error=%v", err)
	}
}

func TestServiceAccountDisableImmediatelyInvalidatesPrincipal(t *testing.T) {
	db := agentTestDB(t)
	service := NewCredentialService(db, user.NewRBAC())
	account, err := service.CreateServiceAccount(context.Background(), "Indexer", user.RoleAuthor, 1)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Issue(context.Background(), CreateCredentialInput{
		SubjectKind: PrincipalServiceAccount, SubjectID: account.ID, Name: "Indexer token",
		Scopes: []string{scopeContentRead}, Audience: "https://example.test/agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.Authenticate(context.Background(), issued.Token, issued.Credential.Audience)
	if err != nil || principal.Kind != PrincipalServiceAccount {
		t.Fatalf("service principal=%+v err=%v", principal, err)
	}
	if err := service.DisableServiceAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ValidatePrincipal(context.Background(), principal); !IsErrorCode(err, CodeUnauthenticated) {
		t.Fatalf("disabled service account error=%v", err)
	}
}

func TestIdempotencyStoreConvergesConcurrentRequestsAndReplaysResult(t *testing.T) {
	db := agentTestDB(t)
	store := NewIdempotencyStore(db)
	arguments := json.RawMessage(`{"title":"same"}`)
	var acquired atomic.Int32
	var pending atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := store.Begin(context.Background(), 7, "test.write", "same-key", arguments)
			if err == nil && decision.Acquired {
				acquired.Add(1)
				return
			}
			if IsErrorCode(err, CodeIdempotencyPending) {
				pending.Add(1)
				return
			}
			t.Errorf("unexpected decision=%+v error=%v", decision, err)
		}()
	}
	wg.Wait()
	if acquired.Load() != 1 || pending.Load() != 7 {
		t.Fatalf("acquired=%d pending=%d", acquired.Load(), pending.Load())
	}
	var record IdempotencyRecord
	if err := db.Where("credential_id = ? AND tool_name = ? AND idempotency_key = ?", 7, "test.write", "same-key").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	output := json.RawMessage(`{"id":42}`)
	if err := store.Complete(context.Background(), record.ID, output, "post", 42); err != nil {
		t.Fatal(err)
	}
	decision, err := store.Begin(context.Background(), 7, "test.write", "same-key", arguments)
	if err != nil || !decision.Replayed || string(decision.Output) != string(output) {
		t.Fatalf("replay decision=%+v error=%v", decision, err)
	}
	decision, err = store.Begin(context.Background(), 7, "test.write", "same-key", json.RawMessage(`{ "title" : "same" }`))
	if err != nil || !decision.Replayed {
		t.Fatalf("semantic JSON replay decision=%+v error=%v", decision, err)
	}
	if _, err := store.Begin(context.Background(), 7, "test.write", "same-key", json.RawMessage(`{"title":"different"}`)); !IsErrorCode(err, CodeConflict) {
		t.Fatalf("argument conflict error=%v", err)
	}
}

func TestAuditStorePersistsAppendOnlyExecutionEvents(t *testing.T) {
	db := agentTestDB(t)
	store := NewAuditStore(db)
	for _, status := range []string{AuditStarted, AuditSucceeded} {
		if err := store.Record(context.Background(), &AuditEvent{
			RequestID: "req-audit", ToolName: "gopress.site.get", ToolOwner: "core",
			Status: status, PrincipalKind: PrincipalUser, SubjectID: 1, CredentialID: 9,
			ArgumentsSummary: summarizeArguments(json.RawMessage(`{"password":"secret","page":1}`)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListByRequest(context.Background(), "req-audit")
	if err != nil || len(events) != 2 || events[0].Status != AuditStarted || events[1].Status != AuditSucceeded {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if strings.Contains(events[0].ArgumentsSummary, "secret") {
		t.Fatalf("audit summary leaked argument value: %s", events[0].ArgumentsSummary)
	}
	page, err := store.Query(context.Background(), AuditQuery{Page: 1, PerPage: 1, Status: AuditSucceeded})
	if err != nil || page.Total != 1 || len(page.Events) != 1 || page.Events[0].Status != AuditSucceeded {
		t.Fatalf("audit page=%+v err=%v", page, err)
	}
	if _, err := store.Query(context.Background(), AuditQuery{Status: "not-a-status"}); !IsErrorCode(err, CodeInvalidRequest) {
		t.Fatalf("invalid audit filter error=%v", err)
	}
}

func TestExecutorWriteReplayCallsHandlerOnce(t *testing.T) {
	db := agentTestDB(t)
	registry := NewRegistry()
	principal := testPrincipal(user.RoleEditor, "gopress:content:write")
	var calls atomic.Int32
	tool := Tool{
		Name: "test.idempotent", Title: "Idempotent write", Description: "Exercise executor idempotency.",
		InputSchema:  emptyObjectSchema,
		OutputSchema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`),
		Mutability:   MutabilityWrite, Risk: RiskWrite, Idempotent: true,
		Permission: PermissionRequirement{Scope: "gopress:content:write", Resource: "content", Action: "update"},
		Handler: func(context.Context, Invocation) (any, error) {
			calls.Add(1)
			return map[string]bool{"ok": true}, nil
		},
	}
	if _, err := registry.Register("test", tool); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), NewIdempotencyStore(db), &memoryAuditRecorder{})
	executor.SetRiskPolicy(StaticRiskPolicy{MaxRisk: RiskWrite})
	call := Call{RequestID: "req-first", ToolName: tool.Name, Arguments: json.RawMessage(`{}`), IdempotencyKey: "stable", Principal: principal}
	if _, err := executor.Execute(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	call.RequestID = "req-retry"
	replayed, err := executor.Execute(context.Background(), call)
	if err != nil || !replayed.Replayed || calls.Load() != 1 {
		t.Fatalf("replayed=%+v calls=%d err=%v", replayed, calls.Load(), err)
	}
}
