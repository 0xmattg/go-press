package user

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestUserAllowsExternalOnlyAccountFields(t *testing.T) {
	parsed, err := schema.Parse(&User{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(User) error = %v", err)
	}
	for _, fieldName := range []string{"Email", "PasswordHash"} {
		field := parsed.LookUpField(fieldName)
		if field == nil {
			t.Fatalf("User.%s schema field missing", fieldName)
		}
		if field.NotNull {
			t.Fatalf("User.%s must allow NULL/empty for external-only accounts", fieldName)
		}
	}
}

func TestUserIdentityHasProviderIssuerSubjectUniqueIndex(t *testing.T) {
	parsed, err := schema.Parse(&UserIdentity{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(UserIdentity) error = %v", err)
	}
	index, ok := parsed.ParseIndexes()["idx_user_identity_key"]
	if !ok || index.Class != "UNIQUE" {
		t.Fatalf("identity composite unique index = %#v", index)
	}
	if len(index.Fields) != 3 || index.Fields[0].Name != "Provider" || index.Fields[1].Name != "Issuer" || index.Fields[2].Name != "Subject" {
		t.Fatalf("identity index fields = %#v", index.Fields)
	}
}

func TestNormalizeVerifiedIdentityUsesOnlyVerifiedEmail(t *testing.T) {
	identity, err := normalizeVerifiedIdentity(VerifiedIdentity{
		Provider: "oidc", Issuer: "https://issuer.example", Subject: "subject-1",
		Email: "User@Example.com", EmailVerified: false,
	})
	if err != nil {
		t.Fatalf("normalizeVerifiedIdentity() error = %v", err)
	}
	if identity.Email != nil || identity.EmailVerified {
		t.Fatalf("unverified email persisted: %#v", identity)
	}

	identity, err = normalizeVerifiedIdentity(VerifiedIdentity{
		Provider: "oidc", Issuer: "https://issuer.example", Subject: "subject-1",
		Email: "User@Example.com", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("normalizeVerifiedIdentity() verified email error = %v", err)
	}
	if identity.Email == nil || *identity.Email != "user@example.com" || !identity.EmailVerified {
		t.Fatalf("verified email = %#v", identity)
	}
}
