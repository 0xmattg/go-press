package agent

import "testing"

func TestPolicyDefaultsReadOnlyAndRequiresPerToolGrant(t *testing.T) {
	policy := NewPolicy()
	read := Tool{Name: "gopress.site.get", Mutability: MutabilityRead, Risk: RiskRead}
	write := Tool{Name: ToolContentUpdate, Mutability: MutabilityWrite, Risk: RiskWrite}
	if !policy.Allow(Principal{}, read) || policy.Allow(Principal{}, write) {
		t.Fatal("default policy must allow reads and deny writes")
	}
	initialRevision := policy.Revision()
	if err := policy.Configure(ProfileSafeWrite, []string{ToolContentUpdate}); err != nil {
		t.Fatal(err)
	}
	if !policy.Allow(Principal{}, write) || policy.Revision() <= initialRevision {
		t.Fatal("safe-write tool grant was not applied")
	}
	if policy.Allow(Principal{}, Tool{Name: ToolContentTrash, Mutability: MutabilityWrite, Risk: RiskDestructive}) {
		t.Fatal("profile must not enable an unselected write tool")
	}
	if err := policy.Configure(ProfileReadOnly, []string{ToolContentUpdate}); err != nil {
		t.Fatal(err)
	}
	if policy.Allow(Principal{}, write) || len(policy.Snapshot().EnabledWriteTools) != 0 {
		t.Fatal("read-only profile must clear all write grants")
	}
}

func TestPolicyRejectsUnknownProfileAndMalformedToolName(t *testing.T) {
	policy := NewPolicy()
	if err := policy.Configure(ToolProfile("open"), nil); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if err := policy.Configure(ProfileSafeWrite, []string{"bad tool"}); err == nil {
		t.Fatal("malformed tool name accepted")
	}
}
