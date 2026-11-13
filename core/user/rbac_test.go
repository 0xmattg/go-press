// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"strings"
	"testing"
)

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

func TestDefaultPublishCapabilityIsExplicitlyLimited(t *testing.T) {
	rbac := NewRBAC()
	if !rbac.Can(RoleEditor, "content", "publish") || !rbac.Can(RoleSuperAdmin, "content", "publish") {
		t.Fatal("editor and super admin must be able to publish through the dedicated capability")
	}
	for _, role := range []string{RoleAuthor, RoleContributor, RoleSubscriber} {
		if rbac.Can(role, "content", "publish") {
			t.Fatalf("%s must not receive content.publish implicitly", role)
		}
	}
}

func TestReplaceGrantsAppliesOnlyTheDifference(t *testing.T) {
	r := NewRBAC()
	// The role already had question.create before any theme grant.
	r.GrantCapability(RoleSubscriber, "question", "create")

	first := r.ReplaceGrants(nil, []CapabilityRef{
		{Role: RoleSubscriber, Resource: "question", Action: "create"},
		{Role: RoleSubscriber, Resource: "question", Action: "read_own"},
		{Role: RoleSubscriber, Resource: "answer", Action: "create"},
		{Role: RoleSubscriber, Resource: "answer", Action: "create"}, // duplicate
		{Role: "no-such-role", Resource: "x", Action: "y"},
	})
	for _, c := range []string{"question.create", "question.read_own", "answer.create"} {
		parts := strings.SplitN(c, ".", 2)
		if !r.Can(RoleSubscriber, parts[0], parts[1]) {
			t.Fatalf("missing %s after first replace", c)
		}
	}

	// Next theme keeps question.read_own, drops answer.create, adds idea.create.
	second := r.ReplaceGrants(first, []CapabilityRef{
		{Role: RoleSubscriber, Resource: "question", Action: "read_own"},
		{Role: RoleSubscriber, Resource: "idea", Action: "create"},
	})
	if !r.Can(RoleSubscriber, "question", "read_own") || !r.Can(RoleSubscriber, "idea", "create") {
		t.Fatal("wanted capabilities missing after second replace")
	}
	if r.Can(RoleSubscriber, "answer", "create") {
		t.Fatal("capability no longer wanted was not revoked")
	}
	if !r.Can(RoleSubscriber, "question", "create") {
		t.Fatal("a capability that predated the grants must survive")
	}

	// Revoking everything keeps only the pre-existing policy.
	if left := r.ReplaceGrants(second, nil); len(left) != 0 {
		t.Fatalf("ReplaceGrants(nil) returned %v", left)
	}
	if r.Can(RoleSubscriber, "question", "read_own") || r.Can(RoleSubscriber, "idea", "create") {
		t.Fatal("grants survived a full revoke")
	}
	if !r.Can(RoleSubscriber, "question", "create") {
		t.Fatal("full revoke removed a pre-existing capability")
	}
}
