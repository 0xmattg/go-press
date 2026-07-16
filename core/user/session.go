package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const PublicSessionCookie = "gopress_user_session"

type SessionMetadata struct {
	IdentityID *uint
	IPAddress  string
	UserAgent  string
}

type SessionToken struct {
	Token     string
	ExpiresAt time.Time
}

// SessionManager creates and validates revocable public-site sessions.
type SessionManager struct {
	repository *SessionRepository
	users      *Repository
	lifetime   time.Duration
	now        func() time.Time
}

func NewSessionManager(repository *SessionRepository, users *Repository, lifetime time.Duration) *SessionManager {
	if lifetime <= 0 {
		lifetime = 24 * time.Hour
	}
	return &SessionManager{repository: repository, users: users, lifetime: lifetime, now: time.Now}
}

func (m *SessionManager) Create(ctx context.Context, userID uint, metadata SessionMetadata) (*SessionToken, error) {
	if m == nil || m.repository == nil || m.users == nil {
		return nil, ErrSessionNotFound
	}
	account, err := m.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if !account.IsActive {
		return nil, ErrAccountDisabled
	}

	token, err := randomSessionToken()
	if err != nil {
		return nil, err
	}
	now := m.now().UTC()
	expiresAt := now.Add(m.lifetime)
	session := &UserSession{
		UserID:     userID,
		IdentityID: metadata.IdentityID,
		TokenHash:  hashSessionToken(token),
		IPAddress:  truncateString(strings.TrimSpace(metadata.IPAddress), 64),
		UserAgent:  truncateString(strings.TrimSpace(metadata.UserAgent), 500),
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
	}
	if err := m.repository.Create(ctx, session); err != nil {
		return nil, err
	}
	_ = m.users.UpdateLastLogin(userID)
	return &SessionToken{Token: token, ExpiresAt: expiresAt}, nil
}

func (m *SessionManager) Authenticate(ctx context.Context, token string) (*User, *UserSession, error) {
	if m == nil || m.repository == nil || m.users == nil || len(token) < 32 || len(token) > 256 {
		return nil, nil, ErrSessionNotFound
	}
	now := m.now().UTC()
	session, err := m.repository.FindActiveByHash(ctx, hashSessionToken(token), now)
	if err != nil {
		return nil, nil, err
	}
	account, err := m.users.FindByID(session.UserID)
	if err != nil {
		return nil, nil, err
	}
	if !account.IsActive {
		_ = m.repository.RevokeByHash(ctx, session.TokenHash, now)
		return nil, nil, ErrAccountDisabled
	}
	if now.Sub(session.LastSeenAt) >= 5*time.Minute {
		_ = m.repository.Touch(ctx, session.ID, now)
		session.LastSeenAt = now
	}
	return account, session, nil
}

func (m *SessionManager) Revoke(ctx context.Context, token string) error {
	if m == nil || m.repository == nil || strings.TrimSpace(token) == "" {
		return ErrSessionNotFound
	}
	return m.repository.RevokeByHash(ctx, hashSessionToken(token), m.now().UTC())
}

func (m *SessionManager) RevokeAllForUser(ctx context.Context, userID uint) error {
	if m == nil || m.repository == nil {
		return ErrSessionNotFound
	}
	return m.repository.RevokeAllForUser(ctx, userID, m.now().UTC())
}

func (m *SessionManager) Rotate(ctx context.Context, token string, metadata SessionMetadata) (*SessionToken, error) {
	account, _, err := m.Authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := m.Revoke(ctx, token); err != nil && !errors.Is(err, ErrSessionNotFound) {
		return nil, err
	}
	return m.Create(ctx, account.ID, metadata)
}

func randomSessionToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
