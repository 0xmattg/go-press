package user

import (
	"strings"
	"time"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Role constants
const (
	RoleSuperAdmin  = "super_admin"
	RoleEditor      = "editor"
	RoleAuthor      = "author"
	RoleContributor = "contributor"
	RoleSubscriber  = "subscriber"
)

// User represents a system user.
type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"size:50;uniqueIndex;not null" json:"username"`
	Email        *string        `gorm:"size:200;uniqueIndex" json:"email,omitempty"`
	PasswordHash string         `gorm:"size:255" json:"-"`
	DisplayName  string         `gorm:"size:100" json:"display_name"`
	AvatarURL    string         `gorm:"size:500" json:"avatar_url"`
	Role         string         `gorm:"size:30;not null;default:subscriber" json:"role"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	LastLoginAt  *time.Time     `json:"last_login_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	Meta         []UserMeta     `gorm:"foreignKey:UserID" json:"meta,omitempty"`
}

func (User) TableName() string { return dbprefix.Table("users") }

// GetMeta returns the value for a meta key, or empty string if not found.
func (u *User) GetMeta(key string) string {
	for _, m := range u.Meta {
		if m.MetaKey == key {
			return m.MetaValue
		}
	}
	return ""
}

// EmailValue returns the user's email or an empty string when the account has
// no email identity, such as a wallet-only account.
func (u *User) EmailValue() string {
	if u == nil || u.Email == nil {
		return ""
	}
	return *u.Email
}

// EmailPointer normalizes an optional email for persistence.
func EmailPointer(email string) *string {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	return &email
}

// UserMeta is a key-value extension for User.
type UserMeta struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	UserID    uint   `gorm:"not null" json:"user_id"`
	MetaKey   string `gorm:"size:255;not null" json:"meta_key"`
	MetaValue string `gorm:"type:text" json:"meta_value"`
}

func (UserMeta) TableName() string { return dbprefix.Table("user_meta") }
