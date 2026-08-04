package core

import (
	"testing"

	"go-press/core/content"
	"go-press/core/user"
)

func TestThemePublicSubmissionCapabilitiesFollowActiveRegistry(t *testing.T) {
	rbac := user.NewRBAC()
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name: "question",
		PublicSubmission: content.PublicSubmissionPolicy{
			Enabled: true, Roles: []string{user.RoleSubscriber},
			AllowUpdateOwn: true, AllowDeleteOwn: true,
		},
	})
	engine := &Engine{RBAC: rbac, Registry: registry}

	engine.registerPublicSubmissionCapabilities()
	for _, action := range []string{"create", "read_own", "update_own", "delete_own"} {
		if !rbac.Can(user.RoleSubscriber, "question", action) {
			t.Fatalf("subscriber missing question.%s", action)
		}
	}

	engine.revokePublicSubmissionCapabilities()
	for _, action := range []string{"create", "read_own", "update_own", "delete_own"} {
		if rbac.Can(user.RoleSubscriber, "question", action) {
			t.Fatalf("stale question.%s remained after theme grant revocation", action)
		}
	}
}

func TestThemeCapabilityRevocationPreservesPreexistingRoleCapability(t *testing.T) {
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleSubscriber, "question", "create")
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{
		Name: "question",
		PublicSubmission: content.PublicSubmissionPolicy{
			Enabled: true, Roles: []string{user.RoleSubscriber},
		},
	})
	engine := &Engine{RBAC: rbac, Registry: registry}

	engine.registerPublicSubmissionCapabilities()
	engine.revokePublicSubmissionCapabilities()
	if !rbac.Can(user.RoleSubscriber, "question", "create") {
		t.Fatal("theme revocation removed a capability that predated the theme grant")
	}
}
