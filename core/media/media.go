package media

import (
	"strings"
	"time"

	"go-press/pkg/dbprefix"
)

// Media represents an uploaded file tracked by the media library.
//
// Path is the public or site-relative file path. Image dimensions are stored on
// the original row, while generated responsive derivatives are stored separately
// in MediaVariant rows.
type Media struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Filename     string    `gorm:"size:255;not null" json:"filename"`
	OriginalName string    `gorm:"size:255" json:"original_name"`
	MimeType     string    `gorm:"size:100" json:"mime_type"`
	Size         int64     `json:"size"`
	Path         string    `gorm:"size:500;not null" json:"path"`
	AltText      string    `gorm:"size:255" json:"alt_text"`
	Title        string    `gorm:"size:255" json:"title"`
	Caption      string    `gorm:"type:text" json:"caption"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	UploadedBy   uint      `json:"uploaded_by"`
	CreatedAt    time.Time `json:"created_at"`
}

func (Media) TableName() string { return dbprefix.Table("media") }

// IsImage checks if the media is an image type.
func (m *Media) IsImage() bool {
	switch m.MimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/svg+xml":
		return true
	}
	return false
}

// URL returns the public URL for the media file.
//
// Absolute and protocol-relative paths are returned unchanged. Relative upload
// paths are joined with baseURL when provided, which lets templates generate
// canonical media URLs without hard-coding the site origin.
func (m *Media) URL(baseURL string) string {
	if m == nil || m.Path == "" {
		return ""
	}
	if strings.HasPrefix(m.Path, "http://") || strings.HasPrefix(m.Path, "https://") || strings.HasPrefix(m.Path, "//") {
		return m.Path
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return m.Path
	}

	path := m.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}
