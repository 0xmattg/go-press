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

func TestDefaultPublicCommentAndProfileCapabilities(t *testing.T) {
	rbac := NewRBAC()
	for _, role := range []string{RoleSubscriber, RoleContributor, RoleAuthor, RoleEditor} {
		if !rbac.Can(role, "comment", "create") {
			t.Errorf("%s must be able to create comments", role)
		}
		if !rbac.Can(role, "profile", "read_own") {
			t.Errorf("%s must be able to read its own profile", role)
		}
	}
	if rbac.Can(RoleSubscriber, "comment", "moderate") {
		t.Fatal("subscriber must not moderate comments")
	}
	if !rbac.Can(RoleEditor, "comment", "moderate") {
		t.Fatal("editor must be able to moderate comments")
	}
}
