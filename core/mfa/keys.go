// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// keyIDJWT names the keys derived from cms.jwt_secret. A record stores the
// ID of the key that sealed it, so another key source can be added later
// without guessing.
const keyIDJWT = "jwt-v1"

// keys are derived from the site's cms.jwt_secret with HKDF-SHA256, one per
// purpose. They protect the second factor against a leak of the database
// alone (a backup, an injection that reads tables): whoever also has the
// config file, or the server, can use them. Changing jwt_secret makes sealed
// TOTP secrets unreadable; people then sign in with a recovery code, whose
// hashes are also keyed, and set up two-step verification again.
type keys struct {
	id       string
	seal     cipher.AEAD
	recovery []byte
	pending  []byte
}

func deriveKeys(jwtSecret string) (*keys, error) {
	secret := []byte(strings.TrimSpace(jwtSecret))
	if len(secret) == 0 {
		return nil, errors.New("mfa: cms.jwt_secret is empty")
	}
	derive := func(info string) ([]byte, error) {
		return hkdf.Key(sha256.New, secret, nil, "gopress/admin-mfa/v1/"+info, 32)
	}
	sealKey, err := derive("seal")
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(sealKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	recovery, err := derive("recovery")
	if err != nil {
		return nil, err
	}
	pending, err := derive("pending-login")
	if err != nil {
		return nil, err
	}
	return &keys{id: keyIDJWT, seal: aead, recovery: recovery, pending: pending}, nil
}

// errUnreadable reports a sealed secret that this key cannot open: it was
// sealed under another jwt_secret, or the row was altered.
var errUnreadable = errors.New("mfa: the stored secret cannot be read with the current key")

// sealFor encrypts a TOTP secret for one user. The user ID is bound as
// additional data, so a sealed secret copied to another user's row does not
// open.
func (k *keys) sealFor(userID uint, plain []byte) (string, error) {
	nonce := make([]byte, k.seal.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := k.seal.Seal(nonce, nonce, plain, userAAD(userID))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (k *keys) openFor(userID uint, keyID, sealed string) ([]byte, error) {
	if keyID != k.id {
		return nil, errUnreadable
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < k.seal.NonceSize() {
		return nil, errUnreadable
	}
	nonce, body := raw[:k.seal.NonceSize()], raw[k.seal.NonceSize():]
	plain, err := k.seal.Open(nil, nonce, body, userAAD(userID))
	if err != nil {
		return nil, errUnreadable
	}
	return plain, nil
}

func userAAD(userID uint) []byte {
	return []byte("gopress-mfa-user:" + strconv.FormatUint(uint64(userID), 10))
}

// recoveryHash is the stored form of a normalized recovery code.
func (k *keys) recoveryHash(code string) string {
	mac := hmac.New(sha256.New, k.recovery)
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}
