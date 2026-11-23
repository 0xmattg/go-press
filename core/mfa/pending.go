// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// PendingTTL is how long the second step of signing in waits for a code
// after the password was accepted.
const PendingTTL = 5 * time.Minute

// ErrPendingInvalid reports a second-step ticket that is missing, altered or
// expired; the user starts again with the password.
var ErrPendingInvalid = errors.New("mfa: sign in again")

// SignPending returns the ticket that carries a user from the password step
// to the code step. It is not a session: it names the user and an expiry,
// and is signed with its own key, so it cannot pass for a session token or
// the other way round.
func (s *Service) SignPending(userID uint) (string, time.Time, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(PendingTTL)
	body := strconv.FormatUint(uint64(userID), 10) + "." +
		strconv.FormatInt(expires.Unix(), 10) + "." +
		hex.EncodeToString(nonce)
	return body + "." + s.pendingMAC(body), expires, nil
}

// ParsePending returns the user a ticket was issued to, or
// ErrPendingInvalid.
func (s *Service) ParsePending(ticket string) (uint, error) {
	body, mac, ok := cutLast(ticket, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(s.pendingMAC(body))) {
		return 0, ErrPendingInvalid
	}
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return 0, ErrPendingInvalid
	}
	userID, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil || userID == 0 {
		return 0, ErrPendingInvalid
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !s.now().Before(time.Unix(expires, 0)) {
		return 0, ErrPendingInvalid
	}
	return uint(userID), nil
}

func (s *Service) pendingMAC(body string) string {
	mac := hmac.New(sha256.New, s.keys.pending)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}
