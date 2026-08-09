package agent

import (
	"context"
	"strings"

	"github.com/0xmattg/go-press/core/user"
)

// Authorizer applies OAuth-style Scope and current Core RBAC as a logical AND.
type Authorizer struct {
	rbac *user.RBAC
}

func NewAuthorizer(rbac *user.RBAC) *Authorizer { return &Authorizer{rbac: rbac} }

func (a *Authorizer) Authorize(_ context.Context, principal Principal, requirement PermissionRequirement) error {
	if a == nil || a.rbac == nil || !principal.Valid() {
		return NewError(CodeUnauthenticated, "valid agent principal required")
	}
	if !principal.HasScope(requirement.Scope) {
		return &Error{
			Code:           CodeInsufficientScope,
			Message:        "credential scope does not allow this operation",
			RequiredScopes: []string{requirement.Scope},
		}
	}
	resource := strings.TrimSpace(requirement.Resource)
	action := strings.TrimSpace(requirement.Action)
	if resource == "" || action == "" {
		return NewError(CodePermissionDenied, "tool permission policy is incomplete")
	}
	if a.rbac.Can(principal.Role, resource, action) {
		return nil
	}
	if requirement.OwnAction != "" && requirement.ResourceOwnerID > 0 &&
		requirement.ResourceOwnerID == principal.SubjectID &&
		a.rbac.Can(principal.Role, resource, requirement.OwnAction) {
		return nil
	}
	return NewError(CodePermissionDenied, "current role does not allow this operation")
}

// CanDiscover is intentionally less specific than Authorize: tools whose
// update_own/delete_own decision depends on an argument may be listed, while
// Execute still resolves and enforces the concrete resource owner.
func (a *Authorizer) CanDiscover(principal Principal, requirement PermissionRequirement) bool {
	if a == nil || a.rbac == nil || !principal.Valid() || !principal.HasScope(requirement.Scope) {
		return false
	}
	if a.rbac.Can(principal.Role, requirement.Resource, requirement.Action) {
		return true
	}
	return requirement.OwnAction != "" && a.rbac.Can(principal.Role, requirement.Resource, requirement.OwnAction)
}
