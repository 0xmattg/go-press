package media

import (
	"gorm.io/gorm"
)

// Repository provides CRUD operations for media files.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new media Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new media record.
func (r *Repository) Create(m *Media) error {
	return r.db.Create(m).Error
}

// UpdateDimensions stores intrinsic image dimensions for a media item.
func (r *Repository) UpdateDimensions(id uint, width, height int) error {
	return r.db.Model(&Media{}).Where("id = ?", id).Updates(map[string]interface{}{
		"width":  width,
		"height": height,
	}).Error
}

// FindByID returns a media record by ID.
func (r *Repository) FindByID(id uint) (*Media, error) {
	var m Media
	err := r.db.First(&m, id).Error
	return &m, err
}

// FindByPath returns a media record for a public media path.
func (r *Repository) FindByPath(path string) (*Media, error) {
	var m Media
	err := r.db.Where("path = ?", path).First(&m).Error
	return &m, err
}

// Delete removes a media record.
func (r *Repository) Delete(id uint) error {
	return r.db.Delete(&Media{}, id).Error
}

// List returns paginated media, optionally filtered by mime type.
func (r *Repository) List(mimeType string, page, perPage int) ([]Media, int64, error) {
	q := r.db.Model(&Media{})
	if mimeType != "" {
		q = q.Where("mime_type LIKE ?", mimeType+"%")
	}
	var total int64
	q.Count(&total)

	var items []Media
	err := q.Order("created_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&items).Error
	return items, total, err
}

// ListByUploader returns media uploaded by a specific user.
func (r *Repository) ListByUploader(userID uint, page, perPage int) ([]Media, int64, error) {
	q := r.db.Model(&Media{}).Where("uploaded_by = ?", userID)
	var total int64
	q.Count(&total)

	var items []Media
	err := q.Order("created_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&items).Error
	return items, total, err
}

// ListAllImages returns all image media records for maintenance jobs.
func (r *Repository) ListAllImages() ([]Media, error) {
	var items []Media
	err := r.db.Where("mime_type LIKE ?", "image/%").Order("id ASC").Find(&items).Error
	return items, err
}

// UpsertVariant stores or updates a generated image variant.
func (r *Repository) UpsertVariant(v *MediaVariant) error {
	var existing MediaVariant
	err := r.db.Where("media_id = ? AND name = ? AND format = ?", v.MediaID, v.Name, v.Format).First(&existing).Error
	if err == nil {
		return r.db.Model(&existing).Updates(map[string]interface{}{
			"mime_type": v.MimeType,
			"path":      v.Path,
			"width":     v.Width,
			"height":    v.Height,
			"size":      v.Size,
		}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return r.db.Create(v).Error
}

// ListVariants returns generated variants for a media record.
func (r *Repository) ListVariants(mediaID uint) ([]MediaVariant, error) {
	var items []MediaVariant
	err := r.db.Where("media_id = ?", mediaID).Order("width ASC, format ASC").Find(&items).Error
	return items, err
}

// DeleteVariants removes variant metadata for a media record.
func (r *Repository) DeleteVariants(mediaID uint) error {
	return r.db.Where("media_id = ?", mediaID).Delete(&MediaVariant{}).Error
}

// UpdateAltText updates the alt text of a media item.
func (r *Repository) UpdateAltText(id uint, altText string) error {
	return r.db.Model(&Media{}).Where("id = ?", id).Update("alt_text", altText).Error
}

// UpdateMeta updates SEO metadata (alt text, title, caption) of a media item.
func (r *Repository) UpdateMeta(id uint, altText, title, caption string) error {
	return r.db.Model(&Media{}).Where("id = ?", id).Updates(map[string]interface{}{
		"alt_text": altText,
		"title":    title,
		"caption":  caption,
	}).Error
}
