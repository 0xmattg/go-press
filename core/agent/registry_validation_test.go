package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/0xmattg/go-press/core/user"
)

func TestRegistryConcurrentRegistrationSortingOwnerRevocationAndRevision(t *testing.T) {
	registry := NewRegistry()
	requirement := PermissionRequirement{Scope: scopeContentRead, Resource: "content", Action: "read"}
	var wg sync.WaitGroup
	for index := 19; index >= 0; index-- {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			name := fmt.Sprintf("test.tool.%02d", index)
			if _, err := registry.Register("plugin-a", testReadTool(name, requirement, func(context.Context, Invocation) (any, error) {
				return map[string]bool{"ok": true}, nil
			})); err != nil {
				t.Errorf("register %s: %v", name, err)
			}
		}(index)
	}
	wg.Wait()
	snapshot := registry.Snapshot()
	if snapshot.Revision != 20 || len(snapshot.Tools) != 20 {
		t.Fatalf("snapshot revision=%d tools=%d", snapshot.Revision, len(snapshot.Tools))
	}
	for index, registered := range snapshot.Tools {
		want := fmt.Sprintf("test.tool.%02d", index)
		if registered.Tool.Name != want {
			t.Fatalf("tool[%d]=%q want %q", index, registered.Tool.Name, want)
		}
	}
	if removed := registry.RevokeOwner("plugin-a"); removed != 20 {
		t.Fatalf("removed=%d want 20", removed)
	}
	if registry.Revision() != 21 || len(registry.Snapshot().Tools) != 0 {
		t.Fatalf("owner revocation revision=%d tools=%d", registry.Revision(), len(registry.Snapshot().Tools))
	}
}

func TestRegistryHandleCannotRevokeLaterGeneration(t *testing.T) {
	registry := NewRegistry()
	requirement := PermissionRequirement{Scope: scopeContentRead, Resource: "content", Action: "read"}
	tool := testReadTool("test.replace", requirement, func(context.Context, Invocation) (any, error) {
		return map[string]bool{"ok": true}, nil
	})
	first, err := registry.Register("owner", tool)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Revoke() {
		t.Fatal("first handle did not revoke its registration")
	}
	second, err := registry.Register("owner", tool)
	if err != nil {
		t.Fatal(err)
	}
	first.Revoke()
	if _, exists := registry.get(tool.Name); !exists || second == nil {
		t.Fatal("later registration disappeared")
	}
}

func TestRegistryRejectsInvalidSchemasAndDuplicateNames(t *testing.T) {
	registry := NewRegistry()
	requirement := PermissionRequirement{Scope: scopeContentRead, Resource: "content", Action: "read"}
	tool := testReadTool("test.valid", requirement, func(context.Context, Invocation) (any, error) {
		return map[string]bool{"ok": true}, nil
	})
	if _, err := registry.Register("owner", tool); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register("owner", tool); err != ErrToolAlreadyRegistered {
		t.Fatalf("duplicate error=%v", err)
	}
	tool.Name = "test.invalid"
	tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"bad":{"type":"mystery"}}}`)
	if _, err := registry.Register("owner", tool); err == nil {
		t.Fatal("unsupported nested schema was accepted")
	}
	tool.Name = "test.unsupported-keyword"
	tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","pattern":"secret"}},"additionalProperties":false}`)
	if _, err := registry.Register("owner", tool); err == nil {
		t.Fatal("unsupported schema keyword was accepted")
	}
}

func TestValidationEnforcesSchemaSizeDepthAndUnknownFields(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","maxLength":5}},"additionalProperties":false}`)
	if err := ValidateJSON(json.RawMessage(`{"name":"hello"}`), schema, 100, 4, false); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"name":"too long"}`),
		json.RawMessage(`{"name":"ok","secret":"x"}`),
		json.RawMessage(`{"name":3}`),
	} {
		if err := ValidateJSON(raw, schema, 100, 4, false); !IsErrorCode(err, CodeInvalidArguments) {
			t.Fatalf("raw=%s error=%v", raw, err)
		}
	}
	if err := ValidateJSON(json.RawMessage(`{"name":"hello"}`), schema, 5, 4, false); !IsErrorCode(err, CodeInvalidArguments) {
		t.Fatalf("size limit error=%v", err)
	}
	deep := json.RawMessage(`{"name":"ok","nested":{"nested":{"nested":true}}}`)
	openSchema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":true}`)
	if err := ValidateJSON(deep, openSchema, 1000, 2, false); !IsErrorCode(err, CodeInvalidArguments) {
		t.Fatalf("depth limit error=%v", err)
	}
}

func TestAuthorizerRequiresScopeAndRBACAndChecksOwnership(t *testing.T) {
	rbac := user.NewRBAC()
	authorizer := NewAuthorizer(rbac)
	principal := testPrincipal(user.RoleAuthor, scopeContentRead)
	if err := authorizer.Authorize(context.Background(), principal, PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}); err != nil {
		t.Fatal(err)
	}
	missingScope := principal
	missingScope.Scopes = nil
	if err := authorizer.Authorize(context.Background(), missingScope, PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}); !IsErrorCode(err, CodeInsufficientScope) {
		t.Fatalf("missing scope error=%v", err)
	}
	if err := authorizer.Authorize(context.Background(), principal, PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "update", OwnAction: "update_own", ResourceOwnerID: principal.SubjectID,
	}); err != nil {
		t.Fatalf("own update denied: %v", err)
	}
	if err := authorizer.Authorize(context.Background(), principal, PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "update", OwnAction: "update_own", ResourceOwnerID: principal.SubjectID + 1,
	}); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("foreign owner error=%v", err)
	}
}

func TestNormalizeScopesIsDeterministic(t *testing.T) {
	got := NormalizeScopes([]string{" gopress:media:read ", "gopress:content:read", "gopress:media:read", ""})
	want := []string{"gopress:content:read", "gopress:media:read"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes=%v want=%v", got, want)
	}
}

func TestRegistrySnapshotCanBeSerializedWithoutHandlers(t *testing.T) {
	registry := NewRegistry()
	_, err := registry.Register("test", testReadTool("test.serializable", PermissionRequirement{
		Scope: scopeContentRead, Resource: "content", Action: "read",
	}, func(context.Context, Invocation) (any, error) { return map[string]bool{"ok": true}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(registry.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Handler") || strings.Contains(string(encoded), "ResolvePermission") {
		t.Fatalf("snapshot leaked executable fields: %s", encoded)
	}
	if registry.Snapshot().Tools[0].Tool.Handler != nil || registry.Snapshot().Tools[0].Tool.ResolvePermission != nil {
		t.Fatal("registry snapshot exposed executable callbacks")
	}
}
