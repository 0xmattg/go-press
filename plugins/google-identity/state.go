package googleidentity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
)

const (
	stateCookieName = "gopress_google_oidc_state"
	stateLifetime   = 10 * time.Minute
)

var errInvalidState = errors.New("invalid oidc state")

type loginChallenge struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	ReturnTo string `json:"return_to"`
	IssuedAt int64  `json:"issued_at"`
}

func newLoginChallenge(returnTo, verifier string, now time.Time) (loginChallenge, error) {
	state, err := randomToken(32)
	if err != nil {
		return loginChallenge{}, err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return loginChallenge{}, err
	}
	returnTo = user.SafeReturnTo(returnTo, "/")
	if len(returnTo) > 1024 {
		returnTo = "/"
	}
	return loginChallenge{State: state, Nonce: nonce, Verifier: verifier, ReturnTo: returnTo, IssuedAt: now.UTC().Unix()}, nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func encodeChallenge(challenge loginChallenge, secret string) (string, error) {
	payload, err := json.Marshal(challenge)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func decodeChallenge(value, secret string, now time.Time) (loginChallenge, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || secret == "" {
		return loginChallenge{}, errInvalidState
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return loginChallenge{}, errInvalidState
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return loginChallenge{}, errInvalidState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return loginChallenge{}, errInvalidState
	}
	var challenge loginChallenge
	if err := json.Unmarshal(payload, &challenge); err != nil {
		return loginChallenge{}, errInvalidState
	}
	issuedAt := time.Unix(challenge.IssuedAt, 0)
	if challenge.State == "" || challenge.Nonce == "" || challenge.Verifier == "" || issuedAt.After(now.Add(time.Minute)) || now.Sub(issuedAt) > stateLifetime {
		return loginChallenge{}, errInvalidState
	}
	challenge.ReturnTo = user.SafeReturnTo(challenge.ReturnTo, "/")
	return challenge, nil
}

func setChallengeCookie(c *gin.Context, value string, secure bool, now time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: stateCookieName, Value: value, Path: "/auth/google", MaxAge: int(stateLifetime.Seconds()),
		Expires: now.Add(stateLifetime), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func clearChallengeCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: stateCookieName, Value: "", Path: "/auth/google", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}
