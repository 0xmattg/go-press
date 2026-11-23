// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package mfa is two-step verification for admin accounts: a time-based
// one-time password (TOTP, RFC 6238) from an authenticator app such as Google
// Authenticator, plus single-use recovery codes.
//
// A user sets it up for their own account (Begin, then Confirm with a code
// from the app) and from then on signs in with a code after the password.
// Sensitive actions ask for a fresh code each time (Verify with
// allowRecovery false); core handlers and plugins reach it through
// admin.RequireSecondFactor. Each code is accepted once: a later request
// with the same code, even within its 30 seconds, is refused. After five
// wrong codes in 15 minutes the account's second factor is locked for 15
// minutes.
//
// Secrets are sealed with AES-256-GCM and recovery codes stored as
// HMAC-SHA256, both under keys derived from cms.jwt_secret (see keys). The
// package depends only on the database; core/user and the admin use it
// through small interfaces.
package mfa

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"rsc.io/qr"
)

// Errors returned by Service. Handlers map them to messages; none of them
// says whether a password was right.
var (
	ErrNotEnrolled    = errors.New("mfa: two-step verification is not on for this account")
	ErrAlreadyEnabled = errors.New("mfa: two-step verification is already on")
	ErrNoPending      = errors.New("mfa: no setup in progress, or it expired")
	ErrInvalidCode    = errors.New("mfa: the code is not correct")
	ErrLocked         = errors.New("mfa: too many wrong codes; try again later")
)

// Method says how a code was verified.
type Method string

// Methods.
const (
	MethodTOTP     Method = "totp"
	MethodRecovery Method = "recovery"
)

const (
	// MaxFailures wrong codes within FailureWindow lock the second factor
	// for LockDuration.
	MaxFailures   = 5
	FailureWindow = 15 * time.Minute
	LockDuration  = 15 * time.Minute
	// pendingTTL is how long a setup waits for its first code.
	pendingTTL = 15 * time.Minute
)

// Status is what the account page shows.
type Status struct {
	Enabled      bool
	EnabledAt    time.Time
	RecoveryLeft int
	LockedUntil  time.Time
	// Unreadable is set when the stored secret no longer opens with the
	// current key (cms.jwt_secret changed): codes from the app fail, and
	// only a recovery code or a reset helps.
	Unreadable bool
	// Pending is set while a setup waits for its first code, until
	// PendingExpiresAt.
	Pending          bool
	PendingExpiresAt time.Time
}

// Enrollment is a setup waiting for its first code.
type Enrollment struct {
	// Secret is the key for typing into an app, in groups of four.
	Secret string
	// URI is the otpauth:// link the QR code holds.
	URI string
	// QRCode is a data: URI of a PNG image of URI.
	QRCode    string
	ExpiresAt time.Time
}

// Service stores and checks second factors.
type Service struct {
	db   *gorm.DB
	keys *keys
	now  func() time.Time
}

// New returns a Service over db with keys derived from jwtSecret.
func New(db *gorm.DB, jwtSecret string) (*Service, error) {
	if db == nil {
		return nil, errors.New("mfa: a database is required")
	}
	k, err := deriveKeys(jwtSecret)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, keys: k, now: time.Now}, nil
}

// SetClock replaces the clock the service reads; tests use it to step
// through 30-second windows.
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// CodeAt returns the code an authenticator app shows at t for a key as
// Enrollment.Secret gives it (spaces allowed). Tests and tools use it.
func CodeAt(secret string, t time.Time) (string, error) {
	raw, err := base32NoPad.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	return totpCode(raw, timeStep(t)), nil
}

func (s *Service) load(ctx context.Context, userID uint) (*record, error) {
	var rec record
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// EnabledAt reports whether two-step verification is on for the user and
// since when. Sessions record this time, so turning it on again or
// resetting it ends the sessions signed in before.
func (s *Service) EnabledAt(userID uint) (time.Time, bool, error) {
	rec, err := s.load(context.Background(), userID)
	if err != nil || rec == nil || rec.EnabledAt == nil {
		return time.Time{}, false, err
	}
	return rec.EnabledAt.UTC(), true, nil
}

// SecondFactorSession is what user.Auth checks on every admin request:
// whether two-step verification is on and since when, and the time before
// which the account's sessions no longer count (set by Reset).
func (s *Service) SecondFactorSession(userID uint) (time.Time, bool, time.Time, error) {
	rec, err := s.load(context.Background(), userID)
	if err != nil || rec == nil {
		return time.Time{}, false, time.Time{}, err
	}
	var enabledAt, notBefore time.Time
	if rec.EnabledAt != nil {
		enabledAt = rec.EnabledAt.UTC()
	}
	if rec.SessionsNotBefore != nil {
		notBefore = rec.SessionsNotBefore.UTC()
	}
	return enabledAt, rec.EnabledAt != nil, notBefore, nil
}

// Status returns the user's state for the account page.
func (s *Service) Status(ctx context.Context, userID uint) (Status, error) {
	rec, err := s.load(ctx, userID)
	if err != nil || rec == nil {
		return Status{}, err
	}
	if rec.EnabledAt == nil {
		if rec.PendingSecret != "" && rec.PendingExpiresAt != nil && s.now().Before(*rec.PendingExpiresAt) {
			return Status{Pending: true, PendingExpiresAt: rec.PendingExpiresAt.UTC()}, nil
		}
		return Status{}, nil
	}
	st := Status{
		Enabled:      true,
		EnabledAt:    rec.EnabledAt.UTC(),
		RecoveryLeft: countUnused(decodeHashes(rec.RecoveryHashes)),
	}
	if rec.LockedUntil != nil && s.now().Before(*rec.LockedUntil) {
		st.LockedUntil = rec.LockedUntil.UTC()
	}
	if _, err := s.keys.openFor(userID, rec.KeyID, rec.TOTPSecret); err != nil {
		st.Unreadable = true
	}
	return st, nil
}

// Begin starts a setup: it creates a secret, keeps it sealed for 15
// minutes, and returns what the page shows. A new Begin replaces an earlier
// unconfirmed one. issuer names the site in the app, account the user.
func (s *Service) Begin(ctx context.Context, userID uint, issuer, account string) (*Enrollment, error) {
	rec, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rec != nil && rec.EnabledAt != nil {
		return nil, ErrAlreadyEnabled
	}
	secret, err := newSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.keys.sealFor(userID, secret)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	expires := now.Add(pendingTTL)
	if rec == nil {
		rec = &record{UserID: userID, CreatedAt: now}
	}
	rec.PendingSecret, rec.PendingExpiresAt, rec.KeyID, rec.UpdatedAt = sealed, &expires, s.keys.id, now
	if err := s.db.WithContext(ctx).Save(rec).Error; err != nil {
		return nil, err
	}
	uri := provisioningURI(secret, issuer, account)
	image, err := qrDataURI(uri)
	if err != nil {
		return nil, err
	}
	return &Enrollment{Secret: formatSecret(secret), URI: uri, QRCode: image, ExpiresAt: expires}, nil
}

// Confirm turns two-step verification on when code comes from the pending
// secret, and returns the recovery codes to show once.
func (s *Service) Confirm(ctx context.Context, userID uint, code string) ([]string, time.Time, error) {
	rec, err := s.load(ctx, userID)
	if err != nil {
		return nil, time.Time{}, err
	}
	now := s.now().UTC()
	if rec == nil || rec.PendingSecret == "" || rec.PendingExpiresAt == nil || !now.Before(*rec.PendingExpiresAt) {
		return nil, time.Time{}, ErrNoPending
	}
	if rec.EnabledAt != nil {
		return nil, time.Time{}, ErrAlreadyEnabled
	}
	if locked(rec, now) {
		return nil, time.Time{}, ErrLocked
	}
	secret, err := s.keys.openFor(userID, rec.KeyID, rec.PendingSecret)
	if err != nil {
		return nil, time.Time{}, ErrNoPending
	}
	step, ok := matchStep(secret, normalizeCode(code), now, 0)
	if !ok {
		return nil, time.Time{}, s.fail(ctx, rec, now)
	}
	codes, hashes, err := s.keys.newRecoveryCodes()
	if err != nil {
		return nil, time.Time{}, err
	}
	// Whole seconds: sessions carry the time as Unix seconds.
	enabled := now.Truncate(time.Second)
	updates := map[string]interface{}{
		"totp_secret": rec.PendingSecret, "pending_secret": "", "pending_expires_at": nil,
		"enabled_at": enabled, "last_step": step,
		"recovery_hashes": encodeHashes(hashes), "recovery_generated_at": now,
		"failed_count": 0, "failed_since": nil, "locked_until": nil, "updated_at": now,
	}
	res := s.db.WithContext(ctx).Model(&record{}).
		Where("user_id = ? AND enabled_at IS NULL AND pending_secret = ?", userID, rec.PendingSecret).
		Updates(updates)
	if res.Error != nil {
		return nil, time.Time{}, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, time.Time{}, ErrNoPending
	}
	return codes, enabled, nil
}

// Verify checks a code for an account with two-step verification on: a
// code from the app, or, when allowRecovery is set, an unused recovery
// code, which is then used up. Signing in and turning two-step verification
// off allow recovery codes; a sensitive action does not, so a stolen
// session plus one recovery code is not enough for it.
func (s *Service) Verify(ctx context.Context, userID uint, code string, allowRecovery bool) (Method, error) {
	rec, err := s.load(ctx, userID)
	if err != nil {
		return "", err
	}
	if rec == nil || rec.EnabledAt == nil {
		return "", ErrNotEnrolled
	}
	now := s.now().UTC()
	if locked(rec, now) {
		return "", ErrLocked
	}
	code = normalizeCode(code)
	if isTOTPCode(code) {
		secret, err := s.keys.openFor(userID, rec.KeyID, rec.TOTPSecret)
		if err == nil {
			if step, ok := matchStep(secret, code, now, rec.LastStep); ok {
				// Compare-and-set on last_step: of two requests with the
				// same code, only one wins.
				res := s.db.WithContext(ctx).Model(&record{}).
					Where("user_id = ? AND last_step < ?", userID, step).
					Updates(map[string]interface{}{"last_step": step, "failed_count": 0, "failed_since": nil, "updated_at": now})
				if res.Error != nil {
					return "", res.Error
				}
				if res.RowsAffected == 1 {
					return MethodTOTP, nil
				}
			}
		}
		return "", s.fail(ctx, rec, now)
	}
	if allowRecovery {
		if hashes, ok := s.keys.useRecovery(decodeHashes(rec.RecoveryHashes), code); ok {
			res := s.db.WithContext(ctx).Model(&record{}).
				Where("user_id = ? AND recovery_hashes = ?", userID, rec.RecoveryHashes).
				Updates(map[string]interface{}{"recovery_hashes": encodeHashes(hashes), "failed_count": 0, "failed_since": nil, "updated_at": now})
			if res.Error != nil {
				return "", res.Error
			}
			if res.RowsAffected == 1 {
				return MethodRecovery, nil
			}
		}
	}
	return "", s.fail(ctx, rec, now)
}

// RegenerateRecovery replaces the user's recovery codes. The caller checks
// a code first.
func (s *Service) RegenerateRecovery(ctx context.Context, userID uint) ([]string, error) {
	codes, hashes, err := s.keys.newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	res := s.db.WithContext(ctx).Model(&record{}).
		Where("user_id = ? AND enabled_at IS NOT NULL", userID).
		Updates(map[string]interface{}{"recovery_hashes": encodeHashes(hashes), "recovery_generated_at": now, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrNotEnrolled
	}
	return codes, nil
}

// Remove turns two-step verification off for the user, who did it
// themselves with their password and a code: the row is deleted and their
// sessions continue.
func (s *Service) Remove(ctx context.Context, userID uint) error {
	return s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&record{}).Error
}

// Reset turns two-step verification off for someone else (an administrator
// or the server command): the secret and recovery codes are cleared and
// every session the user started before now ends, so whoever may have taken
// over the account is signed out. The user sets it up again after signing
// in.
func (s *Service) Reset(ctx context.Context, userID uint) error {
	now := s.now().UTC()
	// Sessions carry their start in whole seconds; one that starts later
	// in this same second still counts.
	notBefore := now.Truncate(time.Second)
	rec := record{UserID: userID, SessionsNotBefore: &notBefore, CreatedAt: now, UpdatedAt: now}
	existing, err := s.load(ctx, userID)
	if err != nil {
		return err
	}
	if existing != nil {
		rec.CreatedAt = existing.CreatedAt
	}
	return s.db.WithContext(ctx).Save(&rec).Error
}

func locked(rec *record, now time.Time) bool {
	return rec.LockedUntil != nil && now.Before(*rec.LockedUntil)
}

// fail counts a wrong code and locks the second factor after MaxFailures
// within FailureWindow. It returns ErrInvalidCode, or ErrLocked when this
// failure locked it.
func (s *Service) fail(ctx context.Context, rec *record, now time.Time) error {
	count, since := rec.FailedCount+1, now
	if rec.FailedSince != nil && now.Sub(*rec.FailedSince) <= FailureWindow {
		since = *rec.FailedSince
	} else {
		count = 1
	}
	updates := map[string]interface{}{"failed_count": count, "failed_since": since, "updated_at": now}
	result := ErrInvalidCode
	if count >= MaxFailures {
		updates = map[string]interface{}{"failed_count": 0, "failed_since": nil, "locked_until": now.Add(LockDuration), "updated_at": now}
		result = ErrLocked
	}
	if err := s.db.WithContext(ctx).Model(&record{}).Where("user_id = ?", rec.UserID).Updates(updates).Error; err != nil {
		return err
	}
	return result
}

func qrDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
