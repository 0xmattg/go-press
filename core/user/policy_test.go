package user

import "testing"

type optionMap map[string]string

func (o optionMap) GetDefault(name, fallback string) string {
	if value, ok := o[name]; ok {
		return value
	}
	return fallback
}

func TestRegistrationPolicyRequiresBothRegistrationSwitches(t *testing.T) {
	policy := NewRegistrationPolicy(optionMap{
		"user_registration_enabled":               "0",
		"external_identity_auto_register_enabled": "1",
	}, NewRBAC())
	if policy.AutoRegistrationEnabled() {
		t.Fatal("auto registration enabled while public registration is disabled")
	}

	policy = NewRegistrationPolicy(optionMap{
		"user_registration_enabled":               "1",
		"external_identity_auto_register_enabled": "1",
	}, NewRBAC())
	if !policy.AutoRegistrationEnabled() {
		t.Fatal("auto registration disabled with both switches enabled")
	}
}

func TestRegistrationPolicyRejectsPrivilegedDefaultRole(t *testing.T) {
	policy := NewRegistrationPolicy(optionMap{"new_user_default_role": RoleSuperAdmin}, NewRBAC())
	if got := policy.DefaultRole(); got != RoleSubscriber {
		t.Fatalf("DefaultRole() = %q, want %q", got, RoleSubscriber)
	}
}
