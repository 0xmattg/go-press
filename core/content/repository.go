package content

import (
	"context"
	"fmt"

	"go-press/core/hook"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Repository provides persistence operations for Content and ContentMeta.
//
// The repository deliberately stays thin: it centralizes common CRUD, slug, and
// meta helpers while leaving content-type-specific behavior to the registry,
// admin service, themes, or plugins.
type Repository struct {
	db    *gorm.DB
	hooks *hook.Bus
}

// NewRepository creates a new content Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// NewRepositoryWithHooks creates a Repository that emits public content hooks
// after framework-level mutations complete.
func NewRepositoryWithHooks(db *gorm.DB, hooks *hook.Bus) *Repository {
	return &Repository{db: db, hooks: hooks}
}

// Query returns a new unscoped ContentQuery for chainable querying.
//
// Request-aware code should call content.NewQuery(content.ScopedDB(c, db)) or
// use scoped repository methods where available, otherwise plugin-provided
// filters such as language constraints will be bypassed.
func (r *Repository) Query() *ContentQuery {
	return NewQuery(r.db)
}

// FindByID returns a content item by ID, with meta preloaded.
func (r *Repository) FindByID(id uint) (*Content, error) {
	var c Content
	err := r.db.Preload("Meta").First(&c, id).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindBySlug returns a content item by type and slug, with meta preloaded.
// This is the unscoped variant — it ignores any request-scoped content
// filters. New callers should generally prefer FindBySlugScoped.
func (r *Repository) FindBySlug(contentType, slug string) (*Content, error) {
	var c Content
	err := r.db.Preload("Meta").
		Where("type = ? AND slug = ?", contentType, slug).
		First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindBySlugScoped is the scope-aware variant of FindBySlug. It applies any
// content scopes registered via AddContentScope on the gin.Context (e.g. the
// multilang plugin's per-language WHERE clause), so two contents with the
// same slug in different languages can coexist and resolve to the right row
// based on the current request context.
//
// When ctx == nil or no scopes are registered, behavior is identical to
// FindBySlug. The Session() inside ScopedDB makes it safe for repeated use.
func (r *Repository) FindBySlugScoped(ctx *gin.Context, contentType, slug string) (*Content, error) {
	db := ScopedDB(ctx, r.db)
	var c Content
	err := db.Preload("Meta").
		Where("type = ? AND slug = ?", contentType, slug).
		First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Create inserts a new content item and its meta.
func (r *Repository) Create(c *Content) error {
	sanitizeContent(c)
	return r.db.Create(c).Error
}

// CreateWithMeta inserts a new content item, persists the supplied meta keys,
// and then emits content.created. It is useful for front-end workflows such as
// contact forms where downstream notifications need the saved meta values.
func (r *Repository) CreateWithMeta(ctx context.Context, c *Content, meta map[string]string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sanitizeContent(c)
	if err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		for key, value := range meta {
			if key == "" {
				continue
			}
			row := ContentMeta{
				ContentID: c.ID,
				MetaKey:   key,
				MetaValue: value,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if r.hooks != nil {
		metaCopy := make(map[string]string, len(meta))
		for key, value := range meta {
			metaCopy[key] = value
		}
		r.hooks.DoAction(ctx, hook.ContentCreated, c, metaCopy)
	}
	return nil
}

// Update saves changes to an existing content item.
func (r *Repository) Update(c *Content) error {
	sanitizeContent(c)
	return r.db.Save(c).Error
}

// Delete soft-deletes a content item by ID.
func (r *Repository) Delete(id uint) error {
	return r.db.Delete(&Content{}, id).Error
}

// Trash moves a content item to trash status.
func (r *Repository) Trash(id uint) error {
	return r.db.Model(&Content{}).Where("id = ?", id).Update("status", StatusTrash).Error
}

// Restore restores a trashed content item to draft.
func (r *Repository) Restore(id uint) error {
	return r.db.Model(&Content{}).Where("id = ?", id).Update("status", StatusDraft).Error
}

// SaveMeta creates or updates a meta key-value pair for a content item.
func (r *Repository) SaveMeta(contentID uint, key, value string) error {
	var meta ContentMeta
	result := r.db.Where("content_id = ? AND meta_key = ?", contentID, key).First(&meta)
	if result.Error != nil {
		// Create new
		meta = ContentMeta{
			ContentID: contentID,
			MetaKey:   key,
			MetaValue: value,
		}
		return r.db.Create(&meta).Error
	}
	// Update existing
	meta.MetaValue = value
	return r.db.Save(&meta).Error
}

// DeleteMeta removes a meta entry.
func (r *Repository) DeleteMeta(contentID uint, key string) error {
	return r.db.Where("content_id = ? AND meta_key = ?", contentID, key).
		Delete(&ContentMeta{}).Error
}

// GetMeta returns all meta for a content item as a map.
func (r *Repository) GetMeta(contentID uint) (map[string]string, error) {
	var metas []ContentMeta
	err := r.db.Where("content_id = ?", contentID).Find(&metas).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(metas))
	for _, m := range metas {
		result[m.MetaKey] = m.MetaValue
	}
	return result, nil
}

// CountByType returns the count of content items of a given type and status.
func (r *Repository) CountByType(contentType, status string) (int64, error) {
	q := r.db.Model(&Content{}).Where("type = ?", contentType)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var count int64
	err := q.Count(&count).Error
	return count, err
}

// ListByType returns content items of a given type ordered by sort_order then created_at.
func (r *Repository) ListByType(contentType string, limit, offset int) ([]Content, error) {
	var items []Content
	q := r.db.Preload("Meta").
		Where("type = ? AND status = ?", contentType, StatusPublished).
		Order("sort_order ASC, created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	err := q.Find(&items).Error
	return items, err
}

// EnsureUniqueSlug appends a suffix if the slug already exists for the given type.
// Unscoped variant — checks against ALL rows regardless of language. New code
// should prefer EnsureUniqueSlugScoped so per-language uniqueness works (WPML
// semantics: same slug allowed across languages).
func (r *Repository) EnsureUniqueSlug(contentType, slug string, excludeID uint) (string, error) {
	return ensureUnique(r.db, contentType, slug, excludeID)
}

// EnsureUniqueSlugScoped is the scope-aware variant. The uniqueness check
// runs through ScopedDB(ctx), so when the multilang plugin has registered a
// language scope on the gin.Context, the suffix is only appended when a
// collision exists *within the same language*. Different languages can share
// the exact same slug — disambiguation is handled at request time by the URL
// language prefix + the same scope mechanism in FindBySlugScoped.
func (r *Repository) EnsureUniqueSlugScoped(ctx *gin.Context, contentType, slug string, excludeID uint) (string, error) {
	return ensureUnique(ScopedDB(ctx, r.db), contentType, slug, excludeID)
}

func ensureUnique(db *gorm.DB, contentType, slug string, excludeID uint) (string, error) {
	original := slug
	for i := 1; ; i++ {
		var count int64
		q := db.Model(&Content{}).Where("type = ? AND slug = ?", contentType, slug)
		if excludeID > 0 {
			q = q.Where("id != ?", excludeID)
		}
		if err := q.Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", original, i)
	}
}
