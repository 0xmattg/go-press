package user

import "testing"

func TestCapabilityGrantPreservesExistingPolicy(t *testing.T) {
	rbac := NewRBAC()

	existing := rbac.GrantCapability(RoleEditor, "content", "read")
	if existing.Added {
		t.Fatal("existing capability must not be marked as newly added")
	}
	rbac.RevokeCapabilityGrant(existing)
	if !rbac.Can(RoleEditor, "content", "read") {
		t.Fatal("revoking a no-op grant removed existing policy")
	}

	added := rbac.GrantCapability(RoleEditor, "analytics", "read")
	if !added.Added || !rbac.Can(RoleEditor, "analytics", "read") {
		t.Fatal("runtime capability was not granted")
	}
	rbac.RevokeCapabilityGrant(added)
	if rbac.Can(RoleEditor, "analytics", "read") {
		t.Fatal("runtime capability was not revoked")
	}
}
