package media

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrMediaNotFound  = errors.New("media not found")
	ErrOptimisticLock = errors.New("media changed since it was read")
)

type MetadataObserver func(context.Context, *Media)

// Repository provides CRUD operations for media files.
type Repository struct {
	db       *gorm.DB
	observer MetadataObserver
}

func (r *Repository) SetMetadataObserver(observer MetadataObserver) {
	if r != nil {
		r.observer = observer
	}
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
	return r.ListContext(context.Background(), mimeType, page, perPage)
}

// ListContext is the request-aware media listing used by Agent and other
// non-Gin transports.
func (r *Repository) ListContext(ctx context.Context, mimeType string, page, perPage int) ([]Media, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if ctx == nil {
		ctx = context.Background()
	}
	q := r.db.WithContext(ctx).Model(&Media{})
	if mimeType != "" {
		q = q.Where("mime_type LIKE ?", mimeType+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

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
	_, err := r.UpdateMetadataOptimistic(context.Background(), id, time.Time{}, &altText, &title, &caption)
	return err
}

// UpdateMetadataOptimistic changes only the three safe descriptive fields.
// expectedUpdatedAt is required by Agent callers and may be zero for existing
// trusted in-process admin workflows.
func (r *Repository) UpdateMetadataOptimistic(ctx context.Context, id uint, expectedUpdatedAt time.Time, altText, title, caption *string) (*Media, error) {
	if r == nil || r.db == nil || id == 0 {
		return nil, ErrMediaNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var item Media
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&item, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMediaNotFound
			}
			return err
		}
		if !expectedUpdatedAt.IsZero() && !item.UpdatedAt.Equal(expectedUpdatedAt) {
			return ErrOptimisticLock
		}
		updates := make(map[string]any, 4)
		if altText != nil {
			item.AltText = *altText
			updates["alt_text"] = *altText
		}
		if title != nil {
			item.Title = *title
			updates["title"] = *title
		}
		if caption != nil {
			item.Caption = *caption
			updates["caption"] = *caption
		}
		if len(updates) == 0 {
			return nil
		}
		now := time.Now().UTC()
		updates["updated_at"] = now
		query := tx.Model(&Media{}).Where("id = ?", id)
		if !expectedUpdatedAt.IsZero() {
			query = query.Where("updated_at = ?", item.UpdatedAt)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrOptimisticLock
		}
		item.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	if r.observer != nil {
		copy := item
		r.observer(ctx, &copy)
	}
	return &item, nil
}
