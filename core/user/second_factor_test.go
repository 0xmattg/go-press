// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCheckSecondFactorClaims(t *testing.T) {
	enabled := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		name    string
		claims  Claims
		on      bool
		wantErr bool
	}{
		{"off: any session", Claims{AMR: []string{"pwd"}}, false, false},
		{"on: session from before it was turned on", Claims{AMR: []string{"pwd"}}, true, true},
		{"on: signed in with a code", Claims{AMR: []string{"pwd", "otp"}, MFA: enabled.Unix()}, true, false},
		{"on: code under an earlier setup", Claims{AMR: []string{"pwd", "otp"}, MFA: enabled.Unix() - 60}, true, true},
		{"on: time without otp", Claims{AMR: []string{"pwd"}, MFA: enabled.Unix()}, true, true},
	} {
		claims := test.claims
		err := checkSecondFactorClaims(&claims, enabled, test.on)
		if (err != nil) != test.wantErr {
			t.Errorf("%s: err = %v", test.name, err)
		}
		if claims.SecondFactor != test.on {
			t.Errorf("%s: SecondFactor = %v", test.name, claims.SecondFactor)
		}
	}
}

func TestSessionTokensCarrySecondFactor(t *testing.T) {
	auth := NewAuth("secret", 1, nil)
	u := &User{ID: 3, Username: "ann", Role: RoleSuperAdmin}
	plain, err := auth.GenerateToken(u)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := auth.ParseToken(plain)
	if hasAMR(claims.AMR, "otp") || claims.MFA != 0 {
		t.Fatalf("plain session = %+v", claims)
	}
	enabled := time.Unix(1_800_000_000, 0)
	withCode, err := auth.RenewSession(u, enabled)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ = auth.ParseToken(withCode)
	if !hasAMR(claims.AMR, "otp") || claims.MFA != enabled.Unix() {
		t.Fatalf("session with a code = %+v", claims)
	}
}

type fakeSecondFactor struct {
	enabledAt time.Time
	on        bool
	notBefore time.Time
	err       error
}

func (f fakeSecondFactor) SecondFactorSession(uint) (time.Time, bool, time.Time, error) {
	return f.enabledAt, f.on, f.notBefore, f.err
}

func TestRequireSessionChecks(t *testing.T) {
	enabled := time.Unix(1_800_000_000, 0)
	claims := &Claims{UserID: 3, AMR: []string{"pwd", "otp"}, MFA: enabled.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{IssuedAt: jwt.NewNumericDate(enabled.Add(time.Hour))}}
	check := func(state fakeSecondFactor, required bool) (*Claims, error) {
		auth := NewAuth("secret", 1, nil)
		auth.SetSecondFactor(state)
		auth.SetSecondFactorPolicy(func(role string) bool { return required && role == RoleSuperAdmin })
		copy := *claims
		copy.Role = RoleSuperAdmin
		return auth.checkSecondFactorSession(&copy)
	}
	if _, err := check(fakeSecondFactor{enabledAt: enabled, on: true}, false); err != nil {
		t.Fatalf("matching session: %v", err)
	}
	if _, err := check(fakeSecondFactor{err: errors.New("db down")}, false); err == nil {
		t.Fatal("a failed lookup must refuse the session")
	}
	if _, err := check(fakeSecondFactor{notBefore: enabled.Add(2 * time.Hour)}, false); err == nil {
		t.Fatal("a session started before a reset must end")
	}
	if c, err := check(fakeSecondFactor{notBefore: enabled}, true); err != nil || !c.SecondFactorSetupRequired {
		t.Fatalf("a required role without two-step verification: %+v %v", c, err)
	}
	if c, err := check(fakeSecondFactor{enabledAt: enabled, on: true}, true); err != nil || c.SecondFactorSetupRequired {
		t.Fatalf("a required role with two-step verification: %+v %v", c, err)
	}
}
