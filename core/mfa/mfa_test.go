// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// RFC 6238 appendix B, SHA-1, cut to six digits.
func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(secret, timeStep(time.Unix(unix, 0))); got != want {
			t.Errorf("T=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestMatchStepWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_800_000_000, 0)
	step := timeStep(now)
	for offset, want := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		_, ok := matchStep(secret, totpCode(secret, step+offset), now, 0)
		if ok != want {
			t.Errorf("offset %d: accepted %v, want %v", offset, ok, want)
		}
	}
	if _, ok := matchStep(secret, totpCode(secret, step), now, step); ok {
		t.Error("a code from an accepted step must be refused")
	}
	if got, ok := matchStep(secret, totpCode(secret, step+1), now, step); !ok || got != step+1 {
		t.Error("a later step is still accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "12a456"} {
		if _, ok := matchStep(secret, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if normalizeCode(" 123 456 ") != "123456" || normalizeCode("123-456") != "123456" {
		t.Error("spaces and dashes must be removed")
	}
}

func TestProvisioningURI(t *testing.T) {
	secret := []byte("12345678901234567890")
	uri, err := url.Parse(provisioningURI(secret, "GoPress (example.com:8080)", "ad:min"))
	if err != nil {
		t.Fatal(err)
	}
	if uri.Scheme != "otpauth" || uri.Host != "totp" {
		t.Fatalf("uri = %s", uri)
	}
	if uri.Path != "/GoPress (example.com 8080):ad min" {
		t.Fatalf("label = %q; colons other than the separator must go", uri.Path)
	}
	q := uri.Query()
	if q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("query = %v", q)
	}
	if got := formatSecret(secret); got != "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ" {
		t.Fatalf("formatted = %q", got)
	}
}

func TestSealBindsUserAndKey(t *testing.T) {
	k, err := deriveKeys("site-secret")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := k.sealFor(7, []byte("totp secret"))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := k.openFor(7, keyIDJWT, sealed); err != nil || string(plain) != "totp secret" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	if _, err := k.openFor(8, keyIDJWT, sealed); err == nil {
		t.Fatal("a secret copied to another user must not open")
	}
	other, _ := deriveKeys("rotated-secret")
	if _, err := other.openFor(7, keyIDJWT, sealed); err == nil {
		t.Fatal("another jwt_secret must not open it")
	}
	if _, err := k.openFor(7, "other-v1", sealed); err == nil {
		t.Fatal("an unknown key ID must not open")
	}
	if _, err := deriveKeys("  "); err == nil {
		t.Fatal("an empty jwt_secret must be refused")
	}
	if k.recoveryHash("abc") == other.recoveryHash("abc") {
		t.Fatal("recovery hashes must depend on the key")
	}
}

func TestRecoveryCodes(t *testing.T) {
	k, _ := deriveKeys("site-secret")
	codes, hashes, err := k.newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' || seen[c] || normalizeRecovery(c) == "" {
			t.Fatalf("bad or repeated code %q", c)
		}
		seen[c] = true
	}
	used, ok := k.useRecovery(hashes, strings.ToUpper(strings.ReplaceAll(codes[3], "-", " ")))
	if !ok || used[3] != "" || countUnused(used) != RecoveryCodeCount-1 {
		t.Fatalf("using code 3: ok=%v left=%d", ok, countUnused(used))
	}
	if _, ok := k.useRecovery(used, codes[3]); ok {
		t.Fatal("a recovery code works once")
	}
	if _, ok := k.useRecovery(hashes, "00000-00000"); ok {
		t.Fatal("an invalid code matched")
	}
}

var testDBs atomic.Int64

func testService(t *testing.T, secret string) (*Service, *time.Time) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	name := strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatInt(testDBs.Add(1), 10)
	sqlDB, err := sql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	// SQLite does not know PostgreSQL's timestamptz, so the test creates the
	// table by hand with the same columns; the PostgreSQL migration test in
	// core runs CreateTableV1 itself.
	if err := db.Exec(`CREATE TABLE gp_user_mfa (user_id INTEGER PRIMARY KEY, totp_secret TEXT NOT NULL DEFAULT '',
		pending_secret TEXT NOT NULL DEFAULT '', pending_expires_at DATETIME, key_id TEXT NOT NULL DEFAULT '',
		enabled_at DATETIME, last_step INTEGER NOT NULL DEFAULT 0, recovery_hashes TEXT NOT NULL DEFAULT '',
		recovery_generated_at DATETIME, failed_count INTEGER NOT NULL DEFAULT 0, failed_since DATETIME,
		locked_until DATETIME, sessions_not_before DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`).Error; err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip("mfa tests require CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	svc, err := New(db, secret)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }
	return svc, &clock
}

func secretOf(t *testing.T, e *Enrollment) []byte {
	t.Helper()
	raw, err := base32NoPad.DecodeString(strings.ReplaceAll(e.Secret, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func enroll(t *testing.T, svc *Service, clock *time.Time, userID uint) ([]byte, []string) {
	t.Helper()
	ctx := context.Background()
	e, err := svc.Begin(ctx, userID, "GoPress (example.com)", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.QRCode, "data:image/png;base64,") || !strings.HasPrefix(e.URI, "otpauth://totp/") {
		t.Fatalf("enrollment = %+v", e)
	}
	secret := secretOf(t, e)
	codes, enabledAt, err := svc.Confirm(ctx, userID, totpCode(secret, timeStep(*clock)))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount || !enabledAt.Equal(clock.Truncate(time.Second)) {
		t.Fatalf("codes %d, enabledAt %v", len(codes), enabledAt)
	}
	*clock = clock.Add(stepSeconds * time.Second)
	return secret, codes
}

func TestEnrollAndVerify(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	if _, on, err := svc.EnabledAt(1); on || err != nil {
		t.Fatalf("before setup: on=%v err=%v", on, err)
	}
	if _, err := svc.Verify(ctx, 1, "123456", true); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("verify before setup = %v", err)
	}

	e, err := svc.Begin(ctx, 1, "GoPress", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Confirm(ctx, 1, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong first code = %v", err)
	}
	if _, on, _ := svc.EnabledAt(1); on {
		t.Fatal("a wrong first code must not turn it on")
	}
	secret := secretOf(t, e)
	if _, _, err := svc.Confirm(ctx, 1, totpCode(secret, timeStep(*clock))); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Begin(ctx, 1, "GoPress", "admin"); !errors.Is(err, ErrAlreadyEnabled) {
		t.Fatalf("Begin when on = %v", err)
	}

	// The code used to confirm cannot be used again.
	if _, err := svc.Verify(ctx, 1, totpCode(secret, timeStep(*clock)), false); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("replayed code = %v", err)
	}
	*clock = clock.Add(30 * time.Second)
	code := totpCode(secret, timeStep(*clock))
	if m, err := svc.Verify(ctx, 1, code, false); err != nil || m != MethodTOTP {
		t.Fatalf("fresh code = %v %v", m, err)
	}
	if _, err := svc.Verify(ctx, 1, code, false); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("the same code twice must fail")
	}
	st, err := svc.Status(ctx, 1)
	if err != nil || !st.Enabled || st.RecoveryLeft != RecoveryCodeCount || st.Unreadable {
		t.Fatalf("status = %+v %v", st, err)
	}
}

func TestConfirmNeedsAFreshSetup(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	if _, _, err := svc.Confirm(ctx, 1, "123456"); !errors.Is(err, ErrNoPending) {
		t.Fatalf("confirm without Begin = %v", err)
	}
	e, _ := svc.Begin(ctx, 1, "GoPress", "admin")
	*clock = clock.Add(pendingTTL)
	if _, _, err := svc.Confirm(ctx, 1, totpCode(secretOf(t, e), timeStep(*clock))); !errors.Is(err, ErrNoPending) {
		t.Fatalf("confirm after 15 minutes = %v", err)
	}
	// A second Begin replaces the first secret.
	first, _ := svc.Begin(ctx, 1, "GoPress", "admin")
	second, _ := svc.Begin(ctx, 1, "GoPress", "admin")
	if _, _, err := svc.Confirm(ctx, 1, totpCode(secretOf(t, first), timeStep(*clock))); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("code from a replaced setup = %v", err)
	}
	if _, _, err := svc.Confirm(ctx, 1, totpCode(secretOf(t, second), timeStep(*clock))); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCodesSignInOnly(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	_, codes := enroll(t, svc, clock, 1)
	if _, err := svc.Verify(ctx, 1, codes[0], false); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("recovery code for a sensitive action = %v", err)
	}
	if m, err := svc.Verify(ctx, 1, codes[0], true); err != nil || m != MethodRecovery {
		t.Fatalf("recovery code to sign in = %v %v", m, err)
	}
	if _, err := svc.Verify(ctx, 1, codes[0], true); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("a recovery code works once")
	}
	st, _ := svc.Status(ctx, 1)
	if st.RecoveryLeft != RecoveryCodeCount-1 {
		t.Fatalf("left = %d", st.RecoveryLeft)
	}
	fresh, err := svc.RegenerateRecovery(ctx, 1)
	if err != nil || len(fresh) != RecoveryCodeCount {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, 1, codes[1], true); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("old codes stop working after regenerating")
	}
	if _, err := svc.Verify(ctx, 1, fresh[1], true); err != nil {
		t.Fatal(err)
	}
}

func TestWrongCodesLock(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	secret, _ := enroll(t, svc, clock, 1)
	for i := 1; i < MaxFailures; i++ {
		if _, err := svc.Verify(ctx, 1, "000000", true); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("failure %d = %v", i, err)
		}
	}
	if _, err := svc.Verify(ctx, 1, "000000", true); !errors.Is(err, ErrLocked) {
		t.Fatalf("failure %d = %v, want locked", MaxFailures, err)
	}
	*clock = clock.Add(time.Minute)
	if _, err := svc.Verify(ctx, 1, totpCode(secret, timeStep(*clock)), false); !errors.Is(err, ErrLocked) {
		t.Fatalf("a right code while locked = %v", err)
	}
	*clock = clock.Add(LockDuration)
	if _, err := svc.Verify(ctx, 1, totpCode(secret, timeStep(*clock)), false); err != nil {
		t.Fatalf("after the lock = %v", err)
	}
	// Failures spread over more than the window do not add up.
	for i := 0; i < MaxFailures+2; i++ {
		*clock = clock.Add(FailureWindow / 3)
		if _, err := svc.Verify(ctx, 1, "000000", false); errors.Is(err, ErrLocked) && i < MaxFailures-1 {
			t.Fatalf("locked after %d spread-out failures", i+1)
		}
	}
}

func TestRemoveAndRotatedSecret(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	_, codes := enroll(t, svc, clock, 1)
	rotated, err := New(svc.db, "rotated-secret")
	if err != nil {
		t.Fatal(err)
	}
	rotated.now = svc.now
	st, _ := rotated.Status(ctx, 1)
	if !st.Enabled || !st.Unreadable {
		t.Fatalf("after changing jwt_secret: %+v", st)
	}
	if _, err := rotated.Verify(ctx, 1, codes[0], true); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("recovery hashes are keyed too; old codes cannot be checked under a new key")
	}
	if err := svc.Remove(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, on, _ := svc.EnabledAt(1); on {
		t.Fatal("Remove must turn it off")
	}
}

func TestPendingTicket(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ticket, expires, err := svc.SignPending(42)
	if err != nil || !expires.Equal(clock.Add(PendingTTL)) {
		t.Fatal(err)
	}
	if id, err := svc.ParsePending(ticket); err != nil || id != 42 {
		t.Fatalf("parse = %d %v", id, err)
	}
	for _, bad := range []string{"", "42", strings.Replace(ticket, "42.", "43.", 1), ticket + "x"} {
		if _, err := svc.ParsePending(bad); !errors.Is(err, ErrPendingInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
	other, _ := New(svc.db, "other-secret")
	other.now = svc.now
	if _, err := other.ParsePending(ticket); err == nil {
		t.Fatal("a ticket from another key must fail")
	}
	*clock = clock.Add(PendingTTL)
	if _, err := svc.ParsePending(ticket); err == nil {
		t.Fatal("an expired ticket must fail")
	}
}

func TestResetEndsSessionsAndClearsTheFactor(t *testing.T) {
	svc, clock := testService(t, "site-secret")
	ctx := context.Background()
	_, codes := enroll(t, svc, clock, 1)
	if err := svc.Reset(ctx, 1); err != nil {
		t.Fatal(err)
	}
	enabledAt, on, notBefore, err := svc.SecondFactorSession(1)
	if err != nil || on || !enabledAt.IsZero() || !notBefore.Equal(clock.Truncate(time.Second)) {
		t.Fatalf("after reset: %v %v %v %v", enabledAt, on, notBefore, err)
	}
	if _, err := svc.Verify(ctx, 1, codes[0], true); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("recovery code after reset = %v", err)
	}
	// The user can set it up again, and the reset time stays.
	enroll(t, svc, clock, 1)
	if _, on, nb, _ := svc.SecondFactorSession(1); !on || nb.IsZero() {
		t.Fatalf("after setting up again: on=%v notBefore=%v", on, nb)
	}
	// Reset of an account that never had it still ends its sessions.
	if err := svc.Reset(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if _, on, nb, _ := svc.SecondFactorSession(2); on || nb.IsZero() {
		t.Fatal("reset without a factor must still record the time")
	}
	if code, err := CodeAt("GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ", time.Unix(59, 0)); err != nil || code != "287082" {
		t.Fatalf("CodeAt = %s %v", code, err)
	}
}
