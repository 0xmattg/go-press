// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAccountDisabled    = errors.New("account is disabled")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

// Claims represents JWT custom claims.
type Claims struct {
	UserID      uint   `json:"uid"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
	// AMR lists how the session signed in: "pwd", plus "otp" after a
	// second-factor code.
	AMR []string `json:"amr,omitempty"`
	// MFA is the Unix time two-step verification was turned on for the
	// account when the session signed in with it. ActiveClaims refuses a
	// session whose MFA does not match the account's current setting, so
	// turning it on or resetting it ends older sessions.
	MFA int64 `json:"mfa,omitempty"`
	// SecondFactor is set by ActiveClaims when the account has two-step
	// verification on. It is not part of the token.
	SecondFactor bool `json:"-"`
	// SecondFactorSetupRequired is set by ActiveClaims when the site requires
	// two-step verification for the account's role and the account has not
	// set it up yet; the admin then only lets it open the setup page. It is
	// not part of the token.
	SecondFactorSetupRequired bool `json:"-"`
	jwt.RegisteredClaims
}

// SecondFactorState reports, for an account, whether two-step verification
// is on and since when, and the time before which its sessions no longer
// count (zero when none). core/mfa implements it.
type SecondFactorState interface {
	SecondFactorSession(userID uint) (enabledAt time.Time, on bool, notBefore time.Time, err error)
}

// Auth handles authentication operations.
type Auth struct {
	secret        []byte
	expireTime    time.Duration
	repo          *Repository
	secureCookies bool
	secondFactor  SecondFactorState
	// secondFactorRequired reports whether the site requires two-step
	// verification for a role.
	secondFactorRequired func(role string) bool
}

// NewAuth creates a new Auth handler.
func NewAuth(jwtSecret string, expireHours int, repo *Repository) *Auth {
	return &Auth{
		secret:     []byte(jwtSecret),
		expireTime: time.Duration(expireHours) * time.Hour,
		repo:       repo,
	}
}

// SetSecureCookies records whether admin session cookies must carry the Secure
// attribute. It is derived from the site scheme by the engine so that HTTPS
// deployments never transmit the admin token over plaintext, while local HTTP
// development keeps working.
func (a *Auth) SetSecureCookies(secure bool) {
	if a != nil {
		a.secureCookies = secure
	}
}

// SetSecondFactor makes ActiveClaims check sessions against the accounts'
// two-step verification. Without it, as in tests, sessions are checked as
// before.
func (a *Auth) SetSecondFactor(state SecondFactorState) {
	if a != nil {
		a.secondFactor = state
	}
}

// SetSecondFactorPolicy sets which roles must use two-step verification.
func (a *Auth) SetSecondFactorPolicy(required func(role string) bool) {
	if a != nil {
		a.secondFactorRequired = required
	}
}

// SecureCookies reports whether admin cookies should be marked Secure.
func (a *Auth) SecureCookies() bool {
	return a != nil && a.secureCookies
}

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// CheckPassword verifies a password against a hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Login authenticates a user and returns a JWT token. It does not know
// about two-step verification; the admin signs in with Authenticate and
// IssueSession instead.
func (a *Auth) Login(username, password string) (string, *User, error) {
	u, err := a.Authenticate(username, password)
	if err != nil {
		return "", nil, err
	}
	token, err := a.IssueSession(u, time.Time{})
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// Authenticate checks a username and password and returns the active
// account, without starting a session.
func (a *Auth) Authenticate(username, password string) (*User, error) {
	u, err := a.repo.FindByUsername(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if !u.IsActive {
		return nil, ErrAccountDisabled
	}
	return u, nil
}

// IssueSession records the sign-in and returns a session token. A non-zero
// secondFactorAt is the account's two-step verification time, for a sign-in
// that passed a second-factor code.
func (a *Auth) IssueSession(u *User, secondFactorAt time.Time) (string, error) {
	now := time.Now()
	u.LastLoginAt = &now
	if a.repo != nil {
		_ = a.repo.Update(u)
	}
	return a.sessionToken(u, secondFactorAt)
}

// RenewSession returns a new token for the signed-in account with its
// current two-step verification time, without recording a sign-in. The
// admin uses it right after the user turns two-step verification on, so
// that session stays signed in while older ones end.
func (a *Auth) RenewSession(u *User, secondFactorAt time.Time) (string, error) {
	return a.sessionToken(u, secondFactorAt)
}

// GenerateToken creates a JWT token for a user.
func (a *Auth) GenerateToken(u *User) (string, error) {
	return a.sessionToken(u, time.Time{})
}

func (a *Auth) sessionToken(u *User, secondFactorAt time.Time) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      u.ID,
		Username:    u.Username,
		Role:        u.Role,
		DisplayName: u.DisplayName,
		AMR:         []string{"pwd"},
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.expireTime)),
			Issuer:    "gopress",
		},
	}
	if !secondFactorAt.IsZero() {
		claims.AMR = append(claims.AMR, "otp")
		claims.MFA = secondFactorAt.Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// ParseToken validates a JWT token and returns claims.
func (a *Auth) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ActiveClaims validates the token signature/expiry and then re-checks the
// account against the current data store: a disabled or deleted account is
// rejected, and role/username/display name are refreshed from the database so
// privilege changes and deactivations take effect immediately instead of
// lingering until the token expires. When no repository is configured (unit
// tests, stateless contexts) it degrades to plain ParseToken.
func (a *Auth) ActiveClaims(tokenStr string) (*Claims, error) {
	claims, err := a.ParseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if a == nil || a.repo == nil {
		return claims, nil
	}
	account, err := a.repo.FindByID(claims.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if err := refreshClaimsFromAccount(claims, account); err != nil {
		return nil, err
	}
	return a.checkSecondFactorSession(claims)
}

// checkSecondFactorSession applies two-step verification to a session whose
// account is active: a failed lookup, a session started before a reset, or
// one that does not match the account's setting is refused. It also marks
// accounts that must still set it up.
func (a *Auth) checkSecondFactorSession(claims *Claims) (*Claims, error) {
	if a.secondFactor == nil {
		return claims, nil
	}
	enabledAt, on, notBefore, err := a.secondFactor.SecondFactorSession(claims.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if !notBefore.IsZero() && (claims.IssuedAt == nil || claims.IssuedAt.Time.Before(notBefore)) {
		return nil, ErrInvalidToken
	}
	if err := checkSecondFactorClaims(claims, enabledAt, on); err != nil {
		return nil, err
	}
	claims.SecondFactorSetupRequired = !on && a.secondFactorRequired != nil && a.secondFactorRequired(claims.Role)
	return claims, nil
}

// checkSecondFactorClaims refuses a session of an account with two-step
// verification on unless it signed in with a code under the current
// setting: sessions from before it was turned on, reset or set up again
// end.
func checkSecondFactorClaims(claims *Claims, enabledAt time.Time, on bool) error {
	claims.SecondFactor = on
	if !on {
		return nil
	}
	if claims.MFA != enabledAt.Unix() || !hasAMR(claims.AMR, "otp") {
		return ErrInvalidToken
	}
	return nil
}

func hasAMR(list []string, method string) bool {
	for _, m := range list {
		if m == method {
			return true
		}
	}
	return false
}

// refreshClaimsFromAccount rejects inactive accounts and syncs mutable identity
// fields from the persisted user onto the token claims. Kept as a pure function
// so the active-session policy is unit-testable without a database.
func refreshClaimsFromAccount(claims *Claims, account *User) error {
	if claims == nil || account == nil {
		return ErrInvalidToken
	}
	if !account.IsActive {
		return ErrAccountDisabled
	}
	claims.Role = account.Role
	claims.Username = account.Username
	claims.DisplayName = account.DisplayName
	return nil
}
