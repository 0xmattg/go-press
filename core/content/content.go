package content

import (
	"time"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Status constants describe the editorial lifecycle for Content rows.
//
// Published content is eligible for front-end queries once PublishedAt is set
// and not in the future. Draft, archived, and trash statuses remain available
// to admin workflows but are excluded by Published queries.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
	StatusTrash     = "trash"
)

// Content is the unified row model for all GoPress content types.
//
// Type links the row to a ContentTypeDef in the active Registry. Core keeps the
// table intentionally broad: common editorial fields live directly on Content,
// while theme/plugin-specific fields live in ContentMeta. This lets themes add
// products, services, showcases, landing-page sections, and other models without
// adding new database tables.
type Content struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	Type          string         `gorm:"size:50;not null;default:post" json:"type"`
	Status        string         `gorm:"size:20;not null;default:draft" json:"status"`
	Title         string         `gorm:"size:500;not null" json:"title"`
	Slug          string         `gorm:"size:500;not null" json:"slug"`
	Content       string         `gorm:"type:text" json:"content"`
	Excerpt       string         `gorm:"type:text" json:"excerpt"`
	ImageURL      string         `gorm:"size:500" json:"image_url"`
	AuthorID      uint           `json:"author_id"`
	ParentID      *uint          `json:"parent_id"`
	SortOrder     int            `gorm:"default:0" json:"sort_order"`
	CommentStatus string         `gorm:"size:20;default:open" json:"comment_status"`
	PublishedAt   *time.Time     `json:"published_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`

	// Meta is loaded explicitly through GORM Preload when callers need custom
	// fields. Repository methods avoid always preloading it on list paths to keep
	// archive and API queries cheap.
	Meta []ContentMeta `gorm:"foreignKey:ContentID" json:"meta,omitempty"`
}

// TableName overrides the default table name.
func (Content) TableName() string { return dbprefix.Table("contents") }

// GetMeta returns the value of a meta key, or empty string if not found.
func (c *Content) GetMeta(key string) string {
	for _, m := range c.Meta {
		if m.MetaKey == key {
			return m.MetaValue
		}
	}
	return ""
}

// SetMeta sets a meta key-value pair in the in-memory Meta slice.
// Call Repository.Save to persist changes.
func (c *Content) SetMeta(key, value string) {
	for i, m := range c.Meta {
		if m.MetaKey == key {
			c.Meta[i].MetaValue = value
			return
		}
	}
	c.Meta = append(c.Meta, ContentMeta{
		ContentID: c.ID,
		MetaKey:   key,
		MetaValue: value,
	})
}

// IsPublished returns true if the content is published.
func (c *Content) IsPublished() bool {
	return c.Status == StatusPublished
}
