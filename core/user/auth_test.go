package user

import (
	"errors"
	"testing"
)

func TestRefreshClaimsFromAccountRejectsDisabled(t *testing.T) {
	claims := &Claims{UserID: 1, Username: "old", Role: RoleSuperAdmin}
	account := &User{ID: 1, Username: "old", Role: RoleSuperAdmin, IsActive: false}

	if err := refreshClaimsFromAccount(claims, account); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("err = %v, want ErrAccountDisabled", err)
	}
}

func TestRefreshClaimsFromAccountSyncsMutableFields(t *testing.T) {
	// Token still carries the stale (elevated) role; the persisted account has
	// since been demoted. The refreshed claims must reflect the current role.
	claims := &Claims{UserID: 7, Username: "editor", Role: RoleSuperAdmin, DisplayName: "Old"}
	account := &User{ID: 7, Username: "editor2", Role: RoleEditor, DisplayName: "New", IsActive: true}

	if err := refreshClaimsFromAccount(claims, account); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Role != RoleEditor {
		t.Fatalf("Role = %q, want %q", claims.Role, RoleEditor)
	}
	if claims.Username != "editor2" || claims.DisplayName != "New" {
		t.Fatalf("identity fields not refreshed: %+v", claims)
	}
}

func TestActiveClaimsWithoutRepoFallsBackToParse(t *testing.T) {
	auth := NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&User{ID: 3, Username: "u", Role: RoleAuthor})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	claims, err := auth.ActiveClaims(token)
	if err != nil {
		t.Fatalf("ActiveClaims() error = %v", err)
	}
	if claims.UserID != 3 || claims.Role != RoleAuthor {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestActiveClaimsRejectsInvalidToken(t *testing.T) {
	auth := NewAuth("test-secret", 1, nil)
	if _, err := auth.ActiveClaims("not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestSecureCookiesFlag(t *testing.T) {
	auth := NewAuth("test-secret", 1, nil)
	if auth.SecureCookies() {
		t.Fatal("SecureCookies() should default to false")
	}
	auth.SetSecureCookies(true)
	if !auth.SecureCookies() {
		t.Fatal("SecureCookies() should be true after SetSecureCookies(true)")
	}
}
