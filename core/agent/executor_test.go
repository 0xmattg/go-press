package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xmattg/go-press/core/user"
)

func TestExecutorRefreshesPrincipalAndAuditsSuccessfulTool(t *testing.T) {
	registry := NewRegistry()
	audit := &memoryAuditRecorder{}
	principal := testPrincipal(user.RoleEditor, scopeContentRead)
	var calls atomic.Int32
	tool := testReadTool("test.read", PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}, func(ctx context.Context, invocation Invocation) (any, error) {
		calls.Add(1)
		fromContext, ok := PrincipalFromContext(ctx)
		if !ok || fromContext.Role != user.RoleEditor || invocation.Principal.Role != user.RoleEditor {
			t.Fatalf("handler received stale principal: %+v %+v", fromContext, invocation.Principal)
		}
		return map[string]bool{"ok": true}, nil
	})
	if _, err := registry.Register("test", tool); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, audit)
	result, err := executor.Execute(context.Background(), Call{
		RequestID: "req-success", ToolName: tool.Name, Arguments: json.RawMessage(`{"value":"yes"}`),
		Principal: testPrincipal(user.RoleSubscriber),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || string(result.Output) != `{"ok":true}` {
		t.Fatalf("calls=%d output=%s", calls.Load(), result.Output)
	}
	if got := audit.statuses(); !reflect.DeepEqual(got, []string{AuditStarted, AuditSucceeded}) {
		t.Fatalf("audit statuses=%v", got)
	}
}

func TestExecutorRejectsMissingScopeAndRoleBeforeHandler(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal Principal
		code      ErrorCode
	}{
		{name: "scope", principal: testPrincipal(user.RoleEditor), code: CodeInsufficientScope},
		{name: "rbac", principal: testPrincipal(user.RoleSubscriber, scopeContentRead), code: CodePermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			audit := &memoryAuditRecorder{}
			var calls atomic.Int32
			tool := testReadTool("test.denied", PermissionRequirement{
				Scope: scopeContentRead, Resource: "content", Action: "read",
			}, func(context.Context, Invocation) (any, error) {
				calls.Add(1)
				return map[string]bool{"ok": true}, nil
			})
			_, _ = registry.Register("test", tool)
			executor := NewExecutor(registry, fixedPrincipalValidator{principal: test.principal}, NewAuthorizer(user.NewRBAC()), nil, audit)
			_, err := executor.Execute(context.Background(), Call{
				RequestID: "req-denied", ToolName: tool.Name, Arguments: json.RawMessage(`{}`), Principal: test.principal,
			})
			if !IsErrorCode(err, test.code) || calls.Load() != 0 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
			if got := audit.statuses(); !reflect.DeepEqual(got, []string{AuditDenied}) {
				t.Fatalf("audit statuses=%v", got)
			}
		})
	}
}

func TestExecutorRejectsInvalidInputAndFailsClosedWhenAuditUnavailable(t *testing.T) {
	principal := testPrincipal(user.RoleEditor, scopeContentRead)
	for _, test := range []struct {
		name      string
		arguments json.RawMessage
		audit     *memoryAuditRecorder
		code      ErrorCode
	}{
		{name: "schema", arguments: json.RawMessage(`{"unknown":true}`), audit: &memoryAuditRecorder{}, code: CodeInvalidArguments},
		{name: "audit", arguments: json.RawMessage(`{}`), audit: &memoryAuditRecorder{failStatus: AuditStarted}, code: CodeAuditUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			var calls atomic.Int32
			tool := testReadTool("test.closed", PermissionRequirement{
				Scope: scopeContentRead, Resource: "content", Action: "read",
			}, func(context.Context, Invocation) (any, error) {
				calls.Add(1)
				return map[string]bool{"ok": true}, nil
			})
			_, _ = registry.Register("test", tool)
			executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, test.audit)
			_, err := executor.Execute(context.Background(), Call{
				RequestID: "req-closed", ToolName: tool.Name, Arguments: test.arguments, Principal: principal,
			})
			if !IsErrorCode(err, test.code) || calls.Load() != 0 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestExecutorDefaultRiskPolicyBlocksWriteTools(t *testing.T) {
	registry := NewRegistry()
	principal := testPrincipal(user.RoleEditor, "gopress:content:write")
	var calls atomic.Int32
	tool := Tool{
		Name: "test.write", Title: "Test write", Description: "Write a test value.",
		InputSchema: emptyObjectSchema, OutputSchema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`),
		Mutability: MutabilityWrite, Risk: RiskWrite, Idempotent: true,
		Permission: PermissionRequirement{Scope: "gopress:content:write", Resource: "content", Action: "update"},
		Handler: func(context.Context, Invocation) (any, error) {
			calls.Add(1)
			return map[string]bool{"ok": true}, nil
		},
	}
	if _, err := registry.Register("test", tool); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, &memoryAuditRecorder{})
	_, err := executor.Execute(context.Background(), Call{
		RequestID: "req-risk", ToolName: tool.Name, Arguments: json.RawMessage(`{}`),
		IdempotencyKey: "same", Principal: principal,
	})
	if !IsErrorCode(err, CodeRiskDenied) || calls.Load() != 0 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}

func TestExecutorVisibleToolsUsesBothScopeAndRBAC(t *testing.T) {
	registry := NewRegistry()
	noop := func(context.Context, Invocation) (any, error) { return map[string]bool{"ok": true}, nil }
	_, _ = registry.Register("test", testReadTool("test.content", PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}, noop))
	_, _ = registry.Register("test", testReadTool("test.media", PermissionRequirement{
		Scope: scopeMediaRead, Resource: "media", Action: "read",
	}, noop))
	principal := testPrincipal(user.RoleAuthor, scopeContentRead, scopeMediaRead)
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, &memoryAuditRecorder{})
	snapshot, err := executor.VisibleTools(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) != 2 {
		t.Fatalf("visible tools=%v", snapshot.Tools)
	}

	subscriber := testPrincipal(user.RoleSubscriber, scopeContentRead, scopeMediaRead)
	executor.principals = fixedPrincipalValidator{principal: subscriber}
	snapshot, err = executor.VisibleTools(context.Background(), subscriber)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) != 0 {
		t.Fatalf("subscriber unexpectedly sees tools=%v", snapshot.Tools)
	}
}

func TestNormalizeExecutionErrorDoesNotLeakInternalMessage(t *testing.T) {
	err := normalizeExecutionError(errors.New("postgres password=secret path=/srv/private"), CodeInternal)
	if err.Error() != "tool execution failed" || !IsErrorCode(err, CodeInternal) {
		t.Fatalf("normalized error=%v", err)
	}
}

func TestExecutorRecoversHandlerPanicAndAuditsFailure(t *testing.T) {
	registry := NewRegistry()
	audit := &memoryAuditRecorder{}
	principal := testPrincipal(user.RoleEditor, scopeContentRead)
	tool := testReadTool("test.panic", PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}, func(context.Context, Invocation) (any, error) {
		panic("secret panic detail")
	})
	_, _ = registry.Register("test", tool)
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, audit)
	_, err := executor.Execute(context.Background(), Call{
		RequestID: "req-panic", ToolName: tool.Name, Arguments: json.RawMessage(`{}`), Principal: principal,
	})
	if !IsErrorCode(err, CodeInternal) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic error=%v", err)
	}
	if got := audit.statuses(); !reflect.DeepEqual(got, []string{AuditStarted, AuditFailed}) {
		t.Fatalf("audit statuses=%v", got)
	}
}
