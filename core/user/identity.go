package user

import (
	"errors"
	"time"

	"go-press/pkg/dbprefix"
)

var (
	ErrIdentityNotFound = errors.New("external identity not found")
	ErrSessionNotFound  = errors.New("user session not found")
)

// UserIdentity binds one verified external identity to a GoPress user. Subject
// is opaque to core; provider plugins own its format and verification rules.
type UserIdentity struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	UserID   uint   `gorm:"not null;index" json:"user_id"`
	Provider string `gorm:"size:64;not null;uniqueIndex:idx_user_identity_key,priority:1" json:"provider"`
	Issuer   string `gorm:"size:255;not null;uniqueIndex:idx_user_identity_key,priority:2" json:"issuer"`
	Subject  string `gorm:"size:255;not null;uniqueIndex:idx_user_identity_key,priority:3" json:"subject"`

	Email         *string `gorm:"size:200" json:"email,omitempty"`
	EmailVerified bool    `gorm:"not null;default:false" json:"email_verified"`
	ProfileJSON   string  `gorm:"type:text" json:"profile_json,omitempty"`

	VerifiedAt time.Time  `gorm:"not null" json:"verified_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	User       User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (UserIdentity) TableName() string { return dbprefix.Table("user_identities") }

// UserSession stores a revocable public-site session. Only a SHA-256 token
// hash is persisted; the bearer token itself exists only in the browser cookie.
type UserSession struct {
	ID         uint          `gorm:"primaryKey" json:"id"`
	UserID     uint          `gorm:"not null;index" json:"user_id"`
	IdentityID *uint         `gorm:"index" json:"identity_id,omitempty"`
	TokenHash  string        `gorm:"size:64;not null;uniqueIndex" json:"-"`
	IPAddress  string        `gorm:"size:64" json:"ip_address,omitempty"`
	UserAgent  string        `gorm:"size:500" json:"user_agent,omitempty"`
	ExpiresAt  time.Time     `gorm:"not null;index" json:"expires_at"`
	LastSeenAt time.Time     `gorm:"not null" json:"last_seen_at"`
	RevokedAt  *time.Time    `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	User       User          `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Identity   *UserIdentity `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"-"`
}

func (UserSession) TableName() string { return dbprefix.Table("user_sessions") }
