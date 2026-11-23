// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"math/big"
	"strings"
)

// RecoveryCodeCount is how many recovery codes a user gets.
const RecoveryCodeCount = 10

// recoveryAlphabet leaves out 0, 1, i, l and o, which are easy to misread.
// Ten characters from it carry about 49 bits.
const (
	recoveryAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	recoveryLength   = 10
)

// newRecoveryCodes returns codes to show once, formatted "xxxxx-xxxxx", and
// their stored hashes in the same order.
func (k *keys) newRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, RecoveryCodeCount)
	hashes := make([]string, RecoveryCodeCount)
	limit := big.NewInt(int64(len(recoveryAlphabet)))
	for i := range codes {
		var b strings.Builder
		for j := 0; j < recoveryLength; j++ {
			n, err := rand.Int(rand.Reader, limit)
			if err != nil {
				return nil, nil, err
			}
			b.WriteByte(recoveryAlphabet[n.Int64()])
		}
		plain := b.String()
		codes[i] = plain[:recoveryLength/2] + "-" + plain[recoveryLength/2:]
		hashes[i] = k.recoveryHash(plain)
	}
	return codes, hashes, nil
}

// normalizeRecovery lowercases a typed recovery code and removes spaces and
// dashes. It returns "" when the result cannot be a recovery code.
func normalizeRecovery(code string) string {
	code = strings.ToLower(normalizeCode(code))
	if len(code) != recoveryLength {
		return ""
	}
	for _, r := range code {
		if !strings.ContainsRune(recoveryAlphabet, r) {
			return ""
		}
	}
	return code
}

func encodeHashes(hashes []string) string {
	raw, _ := json.Marshal(hashes)
	return string(raw)
}

func decodeHashes(raw string) []string {
	var hashes []string
	if raw == "" || json.Unmarshal([]byte(raw), &hashes) != nil {
		return nil
	}
	return hashes
}

// useRecovery returns the hashes with the matching one cleared, or false
// when code matches none of the unused ones.
func (k *keys) useRecovery(hashes []string, code string) ([]string, bool) {
	normalized := normalizeRecovery(code)
	if normalized == "" {
		return nil, false
	}
	want := []byte(k.recoveryHash(normalized))
	out := append([]string(nil), hashes...)
	found := false
	for i, h := range out {
		if h != "" && subtle.ConstantTimeCompare([]byte(h), want) == 1 && !found {
			out[i] = ""
			found = true
		}
	}
	return out, found
}

func countUnused(hashes []string) int {
	n := 0
	for _, h := range hashes {
		if h != "" {
			n++
		}
	}
	return n
}
