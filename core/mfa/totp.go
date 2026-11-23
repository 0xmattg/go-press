// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238): HMAC-SHA1, six digits, 30-second steps, which
// is what Google Authenticator and every common app support. HMAC-SHA1 is
// still sound here: TOTP does not rely on SHA-1's collision resistance.
const (
	secretBytes = 20
	codeDigits  = 6
	stepSeconds = 30
	// skewSteps is how many steps before and after the current one a code
	// may come from, to allow for clocks that are off by up to 30 seconds.
	skewSteps = 1
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

func newSecret() ([]byte, error) {
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// timeStep returns the TOTP step that contains t.
func timeStep(t time.Time) int64 {
	return t.Unix() / stepSeconds
}

// totpCode returns the code for one step (RFC 4226 dynamic truncation).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", codeDigits, value%1_000_000)
}

// normalizeCode removes the spaces and dashes people type or paste.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

func isTOTPCode(code string) bool {
	if len(code) != codeDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// matchStep returns the step of the window around now whose code equals
// code and is later than lastStep, so a code that was already accepted is
// refused (replay).
func matchStep(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if !isTOTPCode(code) {
		return 0, false
	}
	current := timeStep(now)
	for step := current - skewSteps; step <= current+skewSteps; step++ {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// formatSecret returns the secret as people type it into an app: base32
// in groups of four.
func formatSecret(secret []byte) string {
	plain := base32NoPad.EncodeToString(secret)
	var b strings.Builder
	for i, r := range plain {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// provisioningURI is the otpauth:// link an authenticator app reads from the
// QR code. issuer names the site, account the user; neither may contain
// ":" (the label separator), so colons are replaced.
func provisioningURI(secret []byte, issuer, account string) string {
	issuer = strings.ReplaceAll(strings.TrimSpace(issuer), ":", " ")
	account = strings.ReplaceAll(strings.TrimSpace(account), ":", " ")
	query := url.Values{}
	query.Set("secret", base32NoPad.EncodeToString(secret))
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(codeDigits))
	query.Set("period", fmt.Sprint(stepSeconds))
	label := url.PathEscape(issuer + ":" + account)
	return "otpauth://totp/" + label + "?" + query.Encode()
}
