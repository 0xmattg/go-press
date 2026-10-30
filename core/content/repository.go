package content

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/dblock"
	"github.com/0xmattg/go-press/core/hook"

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

// QueryContext creates a query with standard-context content scopes applied.
// It is the preferred entry point for Agent, worker, and other non-Gin callers.
func (r *Repository) QueryContext(ctx context.Context) *ContentQuery {
	return NewQuery(ScopedDBContext(ctx, r.db))
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
	return r.FindBySlugContext(RequestContext(ctx), contentType, slug)
}

// FindBySlugContext is the framework-neutral scope-aware variant used by
// domain services and non-HTTP callers.
func (r *Repository) FindBySlugContext(ctx context.Context, contentType, slug string) (*Content, error) {
	db := ScopedDBContext(ctx, r.db)
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
	return r.createWithMeta(ctx, c, meta, nil)
}

// createWithMeta is CreateWithMeta with an optional step that runs inside the
// insert transaction first, such as reserving a unique slug.
func (r *Repository) createWithMeta(ctx context.Context, c *Content, meta map[string]string, beforeInsert func(tx *gorm.DB) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sanitizeContent(c)
	if err := r.db.Transaction(func(tx *gorm.DB) error {
		if beforeInsert != nil {
			if err := beforeInsert(tx); err != nil {
				return err
			}
		}
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
	return r.db.Transaction(func(tx *gorm.DB) error {
		return saveMeta(tx, contentID, map[string]string{key: value})
	})
}

// saveMeta upserts meta for contentID inside tx. It first advisory-locks every
// (content, key) pair, in a stable order so concurrent multi-key writers cannot
// deadlock, so two first-time saves of the same key cannot both insert a row.
// It then updates every existing row for each key and inserts one only when
// none exists: legacy duplicate rows stay in agreement instead of diverging,
// and a failed write is returned rather than mistaken for a missing row.
func saveMeta(tx *gorm.DB, contentID uint, meta map[string]string) error {
	if len(meta) == 0 {
		return nil
	}
	keys := make([]string, 0, len(meta))
	lockKeys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
		lockKeys = append(lockKeys, metaLockKey(contentID, key))
	}
	sort.Strings(keys)
	if err := dblock.XactLock(tx, lockKeys...); err != nil {
		return err
	}
	for _, key := range keys {
		result := tx.Model(&ContentMeta{}).
			Where("content_id = ? AND meta_key = ?", contentID, key).
			Update("meta_value", meta[key])
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			continue
		}
		if err := tx.Create(&ContentMeta{ContentID: contentID, MetaKey: key, MetaValue: meta[key]}).Error; err != nil {
			return err
		}
	}
	return nil
}

// metaLockKey names the advisory lock for one meta key of one content item.
func metaLockKey(contentID uint, key string) string {
	return fmt.Sprintf("gopress:content-meta:%s:%d:%s", ContentMeta{}.TableName(), contentID, key)
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

// CountRecentByAuthorAndType counts rows created by one author since the
// supplied time. Public authoring services use it for per-type rate limiting;
// trashed rows still count so deleting a submission cannot bypass the limit.
func (r *Repository) CountRecentByAuthorAndType(authorID uint, contentType string, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&Content{}).
		Where("author_id = ? AND type = ? AND created_at >= ?", authorID, contentType, since).
		Count(&count).Error
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
// Unscoped variant — checks against ALL rows regardless of language.
//
// Deprecated: the check runs outside the write transaction, so concurrent
// writers can both receive the same slug. Use ReserveUniqueSlug inside the
// write transaction, CreateWithUniqueSlug, or CommandService with
// WithUniqueSlug.
func (r *Repository) EnsureUniqueSlug(contentType, slug string, excludeID uint) (string, error) {
	return ensureUnique(r.db, contentType, slug, excludeID)
}

// EnsureUniqueSlugScoped is the scope-aware variant. The uniqueness check
// runs through ScopedDB(ctx), so when the multilang plugin has registered a
// language scope on the gin.Context, the suffix is only appended when a
// collision exists *within the same language*. Different languages can share
// the exact same slug — disambiguation is handled at request time by the URL
// language prefix + the same scope mechanism in FindBySlugScoped.
//
// Deprecated: racy for the same reason as EnsureUniqueSlug; use
// ReserveUniqueSlug or CreateWithUniqueSlug with RequestContext(ctx).
func (r *Repository) EnsureUniqueSlugScoped(ctx *gin.Context, contentType, slug string, excludeID uint) (string, error) {
	return r.EnsureUniqueSlugContext(RequestContext(ctx), contentType, slug, excludeID)
}

// EnsureUniqueSlugContext applies request scopes carried by a standard
// context, allowing command services to remain independent from Gin.
//
// Deprecated: racy for the same reason as EnsureUniqueSlug; use
// ReserveUniqueSlug or CommandService with WithUniqueSlug.
func (r *Repository) EnsureUniqueSlugContext(ctx context.Context, contentType, slug string, excludeID uint) (string, error) {
	return ensureUnique(ScopedDBContext(ctx, r.db), contentType, slug, excludeID)
}

// ReserveUniqueSlug picks the first free slug derived from slug for
// contentType, applying the request scopes carried by ctx exactly like
// EnsureUniqueSlugContext. It must run in the transaction that then writes the
// row: every candidate it examines is advisory-locked until that transaction
// ends, so concurrent writers of the same slug are serialized instead of both
// seeing it free.
func ReserveUniqueSlug(ctx context.Context, tx *gorm.DB, contentType, slug string, excludeID uint) (string, error) {
	if strings.TrimSpace(slug) == "" {
		return "", ErrSlugRequired
	}
	return reserveUnique(ScopedDBContext(nonNilContext(ctx), tx), tx, contentType, slug, excludeID)
}

// CreateWithUniqueSlug inserts c after reserving a unique slug derived from
// c.Slug within the request scopes carried by ctx, in one transaction.
func (r *Repository) CreateWithUniqueSlug(ctx context.Context, c *Content) error {
	sanitizeContent(c)
	return r.db.WithContext(nonNilContext(ctx)).Transaction(func(tx *gorm.DB) error {
		slug, err := ReserveUniqueSlug(ctx, tx, c.Type, c.Slug, 0)
		if err != nil {
			return err
		}
		c.Slug = slug
		return tx.Create(c).Error
	})
}

func ensureUnique(db *gorm.DB, contentType, slug string, excludeID uint) (string, error) {
	return reserveUnique(db, nil, contentType, slug, excludeID)
}

// reserveUnique returns slug or the first free "slug-N" for contentType. When
// lockTx is set, each candidate is locked in that transaction before it is
// checked, so a concurrent writer cannot claim it between check and write.
func reserveUnique(db, lockTx *gorm.DB, contentType, slug string, excludeID uint) (string, error) {
	original := slug
	for i := 1; ; i++ {
		if lockTx != nil {
			if err := dblock.XactLock(lockTx, slugLockKey(contentType, slug)); err != nil {
				return "", err
			}
		}
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

// slugLockKey names the advisory lock for one candidate slug. The table name
// carries the site prefix, so sites sharing a database never wait on each
// other; scopes such as language are left out on purpose, which only
// serializes same-slug writes across languages without changing the result.
func slugLockKey(contentType, slug string) string {
	return "gopress:content-slug:" + Content{}.TableName() + ":" + contentType + ":" + slug
}
