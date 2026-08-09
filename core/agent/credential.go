package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

const (
	credentialTokenPrefix = "gp_agent_"
	defaultCredentialTTL  = 30 * 24 * time.Hour
	maximumCredentialTTL  = 90 * 24 * time.Hour
)

// ServiceAccount is a non-human Principal with a current Core role. It has no
// password or browser session and can only authenticate through Agent credentials.
type ServiceAccount struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	Name       string     `gorm:"size:100;not null" json:"name"`
	Role       string     `gorm:"size:30;not null" json:"role"`
	IsActive   bool       `gorm:"default:true" json:"is_active"`
	CreatedBy  uint       `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
}

func (ServiceAccount) TableName() string { return dbprefix.Table("agent_service_accounts") }

// Credential stores only a one-way digest of a high-entropy Bearer secret.
type Credential struct {
	ID          uint          `gorm:"primaryKey" json:"id"`
	SubjectKind PrincipalKind `gorm:"size:30;not null;index" json:"subject_kind"`
	SubjectID   uint          `gorm:"not null;index" json:"subject_id"`
	Name        string        `gorm:"size:100;not null" json:"name"`
	TokenPrefix string        `gorm:"size:24;not null;index" json:"token_prefix"`
	SecretHash  string        `gorm:"size:64;not null;uniqueIndex" json:"-"`
	ScopesJSON  string        `gorm:"column:scopes;type:text;not null" json:"-"`
	Audience    string        `gorm:"size:500;not null;index" json:"audience"`
	ExpiresAt   time.Time     `gorm:"not null;index" json:"expires_at"`
	LastUsedAt  *time.Time    `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time    `gorm:"index" json:"revoked_at,omitempty"`
	CreatedBy   uint          `json:"created_by"`
	CreatedAt   time.Time     `json:"created_at"`
}

func (Credential) TableName() string { return dbprefix.Table("agent_credentials") }

func (c Credential) Scopes() []string {
	var scopes []string
	if json.Unmarshal([]byte(c.ScopesJSON), &scopes) != nil {
		return nil
	}
	return NormalizeScopes(scopes)
}

type CreateCredentialInput struct {
	SubjectKind PrincipalKind
	SubjectID   uint
	Name        string
	Scopes      []string
	Audience    string
	ExpiresAt   time.Time
	CreatedBy   uint
}

type IssuedCredential struct {
	Credential Credential `json:"credential"`
	Token      string     `json:"token"`
}

// CredentialService creates, revokes, authenticates, and refreshes Agent
// Principals. It deliberately has no HTTP or MCP dependency.
type CredentialService struct {
	db   *gorm.DB
	rbac *user.RBAC
	now  func() time.Time
}

func NewCredentialService(db *gorm.DB, rbac *user.RBAC) *CredentialService {
	return &CredentialService{db: db, rbac: rbac, now: func() time.Time { return time.Now().UTC() }}
}

func (s *CredentialService) CreateServiceAccount(ctx context.Context, name, role string, createdBy uint) (*ServiceAccount, error) {
	if s == nil || s.db == nil || s.rbac == nil {
		return nil, NewError(CodeInternal, "credential service unavailable")
	}
	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	if name == "" || s.rbac.GetRole(role) == nil {
		return nil, NewError(CodeInvalidRequest, "valid service account name and role are required")
	}
	account := &ServiceAccount{Name: name, Role: role, IsActive: true, CreatedBy: createdBy}
	if err := s.db.WithContext(nonNilContext(ctx)).Create(account).Error; err != nil {
		return nil, WrapError(CodeInternal, "failed to create service account", err)
	}
	return account, nil
}

func (s *CredentialService) DisableServiceAccount(ctx context.Context, id uint) error {
	if s == nil || s.db == nil || id == 0 {
		return NewError(CodeInvalidRequest, "valid service account is required")
	}
	now := s.now()
	result := s.db.WithContext(nonNilContext(ctx)).Model(&ServiceAccount{}).Where("id = ?", id).
		Updates(map[string]any{"is_active": false, "disabled_at": now})
	if result.Error != nil {
		return WrapError(CodeInternal, "failed to disable service account", result.Error)
	}
	if result.RowsAffected == 0 {
		return NewError(CodeNotFound, "service account not found")
	}
	return nil
}

func (s *CredentialService) Issue(ctx context.Context, input CreateCredentialInput) (*IssuedCredential, error) {
	if s == nil || s.db == nil {
		return nil, NewError(CodeInternal, "credential service unavailable")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Audience = strings.TrimSpace(input.Audience)
	input.Scopes = NormalizeScopes(input.Scopes)
	if input.Name == "" || len(input.Name) > 100 || input.SubjectID == 0 || input.Audience == "" || len(input.Audience) > 500 ||
		len(input.Scopes) == 0 || len(input.Scopes) > 32 || !validCredentialScopes(input.Scopes) {
		return nil, NewError(CodeInvalidRequest, "credential subject, name, scopes, and audience are required")
	}
	now := s.now()
	if input.ExpiresAt.IsZero() {
		input.ExpiresAt = now.Add(defaultCredentialTTL)
	}
	input.ExpiresAt = input.ExpiresAt.UTC()
	if !input.ExpiresAt.After(now) || input.ExpiresAt.After(now.Add(maximumCredentialTTL)) {
		return nil, NewError(CodeInvalidRequest, "credential expiration must be within 90 days")
	}
	if _, err := s.resolveSubject(nonNilContext(ctx), input.SubjectKind, input.SubjectID); err != nil {
		return nil, err
	}
	token, err := generateCredentialToken()
	if err != nil {
		return nil, WrapError(CodeInternal, "failed to generate credential", err)
	}
	scopesJSON, _ := json.Marshal(input.Scopes)
	credential := Credential{
		SubjectKind: input.SubjectKind,
		SubjectID:   input.SubjectID,
		Name:        input.Name,
		TokenPrefix: visibleTokenPrefix(token),
		SecretHash:  hashCredentialToken(token),
		ScopesJSON:  string(scopesJSON),
		Audience:    input.Audience,
		ExpiresAt:   input.ExpiresAt,
		CreatedBy:   input.CreatedBy,
	}
	if err := s.db.WithContext(nonNilContext(ctx)).Create(&credential).Error; err != nil {
		return nil, WrapError(CodeInternal, "failed to store credential", err)
	}
	return &IssuedCredential{Credential: credential, Token: token}, nil
}

func (s *CredentialService) Revoke(ctx context.Context, id uint) error {
	if s == nil || s.db == nil || id == 0 {
		return NewError(CodeInvalidRequest, "valid credential is required")
	}
	now := s.now()
	result := s.db.WithContext(nonNilContext(ctx)).Model(&Credential{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", now)
	if result.Error != nil {
		return WrapError(CodeInternal, "failed to revoke credential", result.Error)
	}
	if result.RowsAffected == 0 {
		return NewError(CodeNotFound, "credential not found")
	}
	return nil
}

// RevokeForSubject revokes a credential only when both its ID and subject
// match. Admin and protocol adapters should prefer this method for user-owned
// credential management so an attacker cannot revoke another user's token by
// guessing an ID.
func (s *CredentialService) RevokeForSubject(ctx context.Context, id uint, kind PrincipalKind, subjectID uint) error {
	if s == nil || s.db == nil || id == 0 || subjectID == 0 || !validPrincipalKind(kind) {
		return NewError(CodeInvalidRequest, "valid credential subject is required")
	}
	now := s.now()
	result := s.db.WithContext(nonNilContext(ctx)).Model(&Credential{}).
		Where("id = ? AND subject_kind = ? AND subject_id = ? AND revoked_at IS NULL", id, kind, subjectID).
		Update("revoked_at", now)
	if result.Error != nil {
		return WrapError(CodeInternal, "failed to revoke credential", result.Error)
	}
	if result.RowsAffected == 0 {
		return NewError(CodeNotFound, "credential not found")
	}
	return nil
}

// ListForSubject returns the most recent credentials owned by one Principal.
// Secret digests and raw scope storage remain hidden by the model's JSON tags.
func (s *CredentialService) ListForSubject(ctx context.Context, kind PrincipalKind, subjectID uint, limit int) ([]Credential, error) {
	if s == nil || s.db == nil || subjectID == 0 || !validPrincipalKind(kind) {
		return nil, NewError(CodeInvalidRequest, "valid credential subject is required")
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var credentials []Credential
	if err := s.db.WithContext(nonNilContext(ctx)).
		Where("subject_kind = ? AND subject_id = ?", kind, subjectID).
		Order("id DESC").Limit(limit).Find(&credentials).Error; err != nil {
		return nil, WrapError(CodeInternal, "failed to list credentials", err)
	}
	return credentials, nil
}

func (s *CredentialService) Authenticate(ctx context.Context, token, audience string) (Principal, error) {
	principal, _, err := s.AuthenticateWithCredential(ctx, token, audience)
	return principal, err
}

// AuthenticateWithCredential validates a Bearer secret and also returns its
// persisted expiry metadata for protocol authentication middleware. The raw
// secret is never returned or stored.
func (s *CredentialService) AuthenticateWithCredential(ctx context.Context, token, audience string) (Principal, Credential, error) {
	if s == nil || s.db == nil {
		return Principal{}, Credential{}, NewError(CodeUnauthenticated, "credential service unavailable")
	}
	token = strings.TrimSpace(token)
	audience = strings.TrimSpace(audience)
	if token == "" || len(token) > 512 || !strings.HasPrefix(token, credentialTokenPrefix) || audience == "" {
		return Principal{}, Credential{}, NewError(CodeUnauthenticated, "invalid agent credential")
	}
	var credential Credential
	err := s.db.WithContext(nonNilContext(ctx)).Where("secret_hash = ?", hashCredentialToken(token)).First(&credential).Error
	if err != nil {
		return Principal{}, Credential{}, NewError(CodeUnauthenticated, "invalid agent credential")
	}
	if credential.Audience != audience {
		return Principal{}, Credential{}, NewError(CodeUnauthenticated, "credential audience mismatch")
	}
	principal, err := s.principalForCredential(nonNilContext(ctx), credential)
	if err != nil {
		return Principal{}, Credential{}, err
	}
	now := s.now()
	_ = s.db.WithContext(nonNilContext(ctx)).Model(&Credential{}).Where("id = ?", credential.ID).Update("last_used_at", now).Error
	credential.LastUsedAt = &now
	return principal, credential, nil
}

func (s *CredentialService) ValidatePrincipal(ctx context.Context, supplied Principal) (Principal, error) {
	if s == nil || s.db == nil || supplied.CredentialID == 0 || supplied.Audience == "" {
		return Principal{}, NewError(CodeUnauthenticated, "valid agent principal required")
	}
	var credential Credential
	if err := s.db.WithContext(nonNilContext(ctx)).First(&credential, supplied.CredentialID).Error; err != nil {
		return Principal{}, NewError(CodeUnauthenticated, "agent credential is no longer valid")
	}
	if credential.SubjectKind != supplied.Kind || credential.SubjectID != supplied.SubjectID || credential.Audience != supplied.Audience {
		return Principal{}, NewError(CodeUnauthenticated, "agent principal does not match credential")
	}
	return s.principalForCredential(nonNilContext(ctx), credential)
}

func (s *CredentialService) principalForCredential(ctx context.Context, credential Credential) (Principal, error) {
	now := s.now()
	if credential.RevokedAt != nil || !credential.ExpiresAt.After(now) {
		return Principal{}, NewError(CodeUnauthenticated, "agent credential is expired or revoked")
	}
	subject, err := s.resolveSubject(ctx, credential.SubjectKind, credential.SubjectID)
	if err != nil {
		return Principal{}, err
	}
	return Principal{
		Kind: credential.SubjectKind, SubjectID: credential.SubjectID,
		Username: subject.username, Role: subject.role, Scopes: credential.Scopes(),
		Audience: credential.Audience, CredentialID: credential.ID,
	}, nil
}

type resolvedSubject struct {
	username string
	role     string
}

func (s *CredentialService) resolveSubject(ctx context.Context, kind PrincipalKind, id uint) (resolvedSubject, error) {
	switch kind {
	case PrincipalUser:
		var account user.User
		if err := s.db.WithContext(ctx).First(&account, id).Error; err != nil || !account.IsActive {
			return resolvedSubject{}, NewError(CodeUnauthenticated, "agent user is unavailable")
		}
		if s.rbac == nil || s.rbac.GetRole(account.Role) == nil {
			return resolvedSubject{}, NewError(CodeUnauthenticated, "agent user role is unavailable")
		}
		return resolvedSubject{username: account.Username, role: account.Role}, nil
	case PrincipalServiceAccount:
		var account ServiceAccount
		if err := s.db.WithContext(ctx).First(&account, id).Error; err != nil || !account.IsActive {
			return resolvedSubject{}, NewError(CodeUnauthenticated, "agent service account is unavailable")
		}
		if s.rbac == nil || s.rbac.GetRole(account.Role) == nil {
			return resolvedSubject{}, NewError(CodeUnauthenticated, "agent service account role is unavailable")
		}
		return resolvedSubject{username: account.Name, role: account.Role}, nil
	default:
		return resolvedSubject{}, NewError(CodeInvalidRequest, "unsupported agent principal kind")
	}
}

func generateCredentialToken() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return credentialTokenPrefix + base64.RawURLEncoding.EncodeToString(secret), nil
}

func hashCredentialToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func visibleTokenPrefix(token string) string {
	const visible = 20
	if len(token) <= visible {
		return token
	}
	return token[:visible]
}

func validCredentialScopes(scopes []string) bool {
	for _, scope := range scopes {
		if len(scope) == 0 || len(scope) > 100 || strings.ContainsAny(scope, " \t\r\n") {
			return false
		}
	}
	return true
}

func validPrincipalKind(kind PrincipalKind) bool {
	return kind == PrincipalUser || kind == PrincipalServiceAccount
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

var _ PrincipalValidator = (*CredentialService)(nil)
