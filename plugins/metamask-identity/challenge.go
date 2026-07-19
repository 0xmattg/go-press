package metamaskidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	siwe "github.com/signinwithethereum/siwe-go"

	"go-press/core/user"
)

const challengeLifetime = 5 * time.Minute

var errInvalidWalletProof = errors.New("invalid wallet proof")

type challengeResponse struct {
	Token     string    `json:"challenge_token"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}

func buildWalletChallenge(config providerConfig, address, returnTo string, now time.Time) (*walletChallenge, challengeResponse, error) {
	token, err := randomOpaqueToken(32)
	if err != nil {
		return nil, challengeResponse{}, err
	}
	now = now.UTC().Truncate(time.Millisecond)
	expiresAt := now.Add(challengeLifetime)
	nonce := siwe.GenerateNonce()
	message, err := siwe.NewMessage(config.Domain, trimmed(address, 64), config.URI, nonce, map[string]interface{}{
		"scheme":         config.Scheme,
		"statement":      "Sign in to GoPress.",
		"chainId":        config.ChainID,
		"issuedAt":       now,
		"expirationTime": expiresAt,
	})
	if err != nil {
		return nil, challengeResponse{}, errInvalidWalletProof
	}
	prepared := message.String()
	challenge := &walletChallenge{
		TokenHash: hashValue(token), NonceHash: hashValue(nonce), MessageHash: hashValue(prepared),
		Address: strings.ToLower(message.Address.Hex()), Scheme: config.Scheme,
		Domain: config.Domain, URI: config.URI, ChainID: config.ChainID,
		ReturnTo: user.SafeReturnTo(returnTo, "/"), IssuedAt: now, ExpiresAt: expiresAt,
	}
	if len(challenge.ReturnTo) > 1024 {
		challenge.ReturnTo = "/"
	}
	return challenge, challengeResponse{Token: token, Message: prepared, ExpiresAt: expiresAt}, nil
}

func verifyWalletChallenge(ctx context.Context, challenge *walletChallenge, messageText, signature string, now time.Time) (string, error) {
	if challenge == nil || len(messageText) == 0 || len(messageText) > 8192 || len(signature) == 0 || len(signature) > 2048 {
		return "", errInvalidWalletProof
	}
	if subtle.ConstantTimeCompare([]byte(hashValue(messageText)), []byte(challenge.MessageHash)) != 1 {
		return "", errInvalidWalletProof
	}
	message, err := siwe.ParseMessage(messageText)
	if err != nil || message == nil {
		return "", errInvalidWalletProof
	}
	if subtle.ConstantTimeCompare([]byte(hashValue(message.Nonce)), []byte(challenge.NonceHash)) != 1 ||
		!strings.EqualFold(message.Address.Hex(), challenge.Address) {
		return "", errInvalidWalletProof
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, message.IssuedAt)
	if err != nil || !issuedAt.Equal(challenge.IssuedAt) || issuedAt.After(now.Add(time.Minute)) {
		return "", errInvalidWalletProof
	}
	scheme, domain, nonce, uri, chainID := challenge.Scheme, challenge.Domain, message.Nonce, challenge.URI, challenge.ChainID
	verifyAt := now.UTC()
	result, err := message.VerifyWith(ctx, signature, siwe.VerifyParams{
		Scheme: &scheme, Domain: &domain, Nonce: &nonce, URI: &uri, ChainID: &chainID, Time: &verifyAt,
	}, siwe.VerifyOptions{})
	if err != nil || result == nil || result.ECDSAPublicKey == nil || result.ContractVerified {
		return "", errInvalidWalletProof
	}
	return strings.ToLower(message.Address.Hex()), nil
}

func randomOpaqueToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func shortAddress(address string) string {
	address = strings.TrimSpace(address)
	if len(address) <= 12 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}
