package content

import "go-press/pkg/dbprefix"

// ContentMeta stores arbitrary key-value metadata for a Content item.
// This is equivalent to WordPress wp_postmeta table.
type ContentMeta struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ContentID uint   `gorm:"not null" json:"content_id"`
	MetaKey   string `gorm:"size:255;not null" json:"meta_key"`
	MetaValue string `gorm:"type:text" json:"meta_value"`
}

// TableName overrides the default table name.
func (ContentMeta) TableName() string { return dbprefix.Table("content_meta") }
