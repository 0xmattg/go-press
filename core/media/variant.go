package media

import (
	"time"

	"go-press/pkg/dbprefix"
)

// MediaVariant stores one generated derivative of an uploaded image.
//
// The unique key is (media_id, name, format), for example thumb/webp or
// 768w/jpeg. Themes should not construct these rows directly; they are created
// by the image pipeline and consumed by responsive image helpers.
type MediaVariant struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MediaID   uint      `gorm:"index;not null;uniqueIndex:idx_media_variant" json:"media_id"`
	Name      string    `gorm:"size:64;not null;uniqueIndex:idx_media_variant" json:"name"`
	Format    string    `gorm:"size:20;not null;uniqueIndex:idx_media_variant" json:"format"`
	MimeType  string    `gorm:"size:100;not null" json:"mime_type"`
	Path      string    `gorm:"size:500;not null" json:"path"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func (MediaVariant) TableName() string { return dbprefix.Table("media_variants") }
