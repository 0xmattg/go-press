// Package comment owns GoPress comments independently from editorial content.
// Comments remain available across theme switches and are rendered by themes
// through the generic core/theme comment contract.
package comment

import (
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusSpam     = "spam"
	StatusTrash    = "trash"

	MinBodyRunes = 2
	MaxBodyRunes = 4000
)

// Comment is an authenticated user's response to one content row. ParentID is
// nil for a top-level comment and points to another Comment for a direct reply.
type Comment struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ContentID uint           `gorm:"not null;index:idx_comments_content_status,priority:1" json:"content_id"`
	UserID    uint           `gorm:"not null;index:idx_comments_user_created,priority:1" json:"user_id"`
	ParentID  *uint          `gorm:"index" json:"parent_id,omitempty"`
	Body      string         `gorm:"type:text;not null" json:"body"`
	Status    string         `gorm:"size:20;not null;default:pending;index:idx_comments_content_status,priority:2" json:"status"`
	CreatedAt time.Time      `gorm:"index:idx_comments_user_created,priority:2" json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Target content.Content `gorm:"foreignKey:ContentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Author user.User       `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Parent *Comment        `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"-"`
}

func (Comment) TableName() string { return dbprefix.Table("comments") }

// AuthorView is the public, credential-free author projection attached to a
// rendered comment.
type AuthorView struct {
	ID          uint
	Username    string
	DisplayName string
	AvatarURL   string
}

// View is the template-safe comment shape used by themes.
type View struct {
	ID          uint
	ContentID   uint
	ParentID    *uint
	Body        string
	Status      string
	CreatedAt   time.Time
	Author      AuthorView
	IsOwn       bool
	TargetType  string
	TargetTitle string
	TargetSlug  string
	Replies     []View
}

// Page is one paginated admin result set.
type Page struct {
	Items      []Comment
	Total      int64
	Page       int
	PerPage    int
	TotalPages int
}
