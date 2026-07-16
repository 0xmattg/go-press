package user

import (
	"strings"

	"go-press/core/option"
)

// OptionReader is the narrow settings surface required by registration policy.
type OptionReader interface {
	GetDefault(name, defaultValue string) string
}

// RegistrationPolicy centralizes public registration and identity-link rules.
// Provider plugins must not make role or account-provisioning decisions.
type RegistrationPolicy struct {
	options OptionReader
	rbac    *RBAC
}

func NewRegistrationPolicy(options OptionReader, rbac *RBAC) *RegistrationPolicy {
	return &RegistrationPolicy{options: options, rbac: rbac}
}

func (p *RegistrationPolicy) RegistrationEnabled() bool {
	return p.enabled(option.KeyUserRegistrationEnabled, false)
}

func (p *RegistrationPolicy) ExternalLoginEnabled() bool {
	return p.enabled(option.KeyExternalIdentityLoginEnabled, true)
}

func (p *RegistrationPolicy) AutoRegistrationEnabled() bool {
	return p.RegistrationEnabled() && p.enabled(option.KeyExternalIdentityAutoRegister, false)
}

func (p *RegistrationPolicy) AccountLinkingEnabled() bool {
	return p.enabled(option.KeyUserAccountLinkingEnabled, true)
}

func (p *RegistrationPolicy) DefaultRole() string {
	role := RoleSubscriber
	var rbac *RBAC
	if p != nil && p.options != nil {
		role = strings.TrimSpace(p.options.GetDefault(option.KeyNewUserDefaultRole, RoleSubscriber))
	}
	if p != nil {
		rbac = p.rbac
	}
	if !IsAllowedPublicRegistrationRole(rbac, role) {
		return RoleSubscriber
	}
	return role
}

func (p *RegistrationPolicy) enabled(key string, defaultValue bool) bool {
	if p == nil || p.options == nil {
		return defaultValue
	}
	fallback := "0"
	if defaultValue {
		fallback = "1"
	}
	return p.options.GetDefault(key, fallback) == "1"
}

// IsAllowedPublicRegistrationRole limits anonymous provisioning to roles no
// more privileged than subscriber. Future low-privilege roles can participate
// without allowing a setting or provider to grant administrative access.
func IsAllowedPublicRegistrationRole(rbac *RBAC, role string) bool {
	if rbac == nil || strings.TrimSpace(role) == "" {
		return role == RoleSubscriber
	}
	level := rbac.RoleLevel(role)
	return level > 0 && level <= rbac.RoleLevel(RoleSubscriber)
}
