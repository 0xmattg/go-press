package content

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrCommandUnavailable  = errors.New("content command service unavailable")
	ErrContentTypeRequired = errors.New("content type is required")
	ErrContentTypeNotFound = errors.New("content type is not registered")
	ErrContentTypeMismatch = errors.New("content does not belong to the requested type")
	ErrContentNotFound     = errors.New("content not found")
	ErrContentReadOnly     = errors.New("content type is read-only")
	ErrReservedSlug        = errors.New("content slug conflicts with a reserved route")
	ErrUnsupportedMeta     = errors.New("content meta key is not declared by the content type")
	ErrInvalidStatus       = errors.New("content status is invalid")
	ErrReorderUnsupported  = errors.New("content type does not support sorting")
	ErrOptimisticLock      = errors.New("content changed since it was read")
	ErrInvalidTransition   = errors.New("content status transition is invalid")
)

type MutationKind string

const (
	MutationCreated   MutationKind = "created"
	MutationUpdated   MutationKind = "updated"
	MutationPublished MutationKind = "published"
	MutationTrashed   MutationKind = "trashed"
	MutationRestored  MutationKind = "restored"
)

type Mutation struct {
	Kind MutationKind
	Item *Content
	Meta map[string]string
}

type MutationObserver func(context.Context, Mutation)

var reservedRootlessSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "static": {}, "uploads": {},
	"auth": {}, "swagger": {}, "health": {},
	"sitemap.xml": {}, "robots.txt": {}, "favicon.ico": {},
}

// RelatedDeleteFunc removes rows owned by another core subsystem during a
// permanent content deletion. It keeps the content package independent from
// comments, taxonomies, admin, and transport implementations.
type RelatedDeleteFunc func(tx *gorm.DB, contentIDs []uint) error

// CommandService owns framework-level content mutation invariants. Transport
// layers parse input and enforce authorization; this service validates content
// type boundaries, reserved routes, core meta declarations, and transactions.
type CommandService struct {
	db       *gorm.DB
	registry *Registry
	now      func() time.Time
	observer MutationObserver
}

func (s *CommandService) SetMutationObserver(observer MutationObserver) {
	if s != nil {
		s.observer = observer
	}
}

func NewCommandService(db *gorm.DB, registry *Registry) *CommandService {
	return &CommandService{db: db, registry: registry, now: func() time.Time { return time.Now().UTC() }}
}

// IsReservedRootlessSlug reports whether a root-level slug would be shadowed
// by a system route, content archive, or taxonomy route.
func IsReservedRootlessSlug(registry *Registry, slug string) bool {
	slug = strings.ToLower(strings.Trim(strings.TrimSpace(slug), "/"))
	if slug == "" {
		return false
	}
	if _, reserved := reservedRootlessSlugs[slug]; reserved {
		return true
	}
	if registry == nil {
		return false
	}
	for _, definition := range registry.AllTypes() {
		if definition == nil || definition.Rewrite.Rootless {
			continue
		}
		prefix := definition.Rewrite.Slug
		if prefix == "" {
			prefix = definition.Name
		}
		if strings.EqualFold(prefix, slug) {
			return true
		}
	}
	for _, definition := range registry.AllTaxonomies() {
		if definition != nil && strings.EqualFold(definition.Name, slug) {
			return true
		}
	}
	return false
}

// Create validates and atomically persists a content row and its core-owned
// meta. Extension-owned fields continue to use extension hooks after this
// command completes.
func (s *CommandService) Create(ctx context.Context, item *Content, meta map[string]string) error {
	if item == nil {
		return errors.New("content is required")
	}
	definition, err := s.definition(item.Type)
	if err != nil {
		return err
	}
	if definition.ReadOnly {
		return ErrContentReadOnly
	}
	if err := s.validateMutation(definition, item, meta); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	sanitizeContent(item)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Meta").Create(item).Error; err != nil {
			return err
		}
		return saveDeclaredMeta(tx, item.ID, meta)
	})
	if err == nil {
		s.notify(ctx, MutationCreated, item, meta)
	}
	return err
}

// Update atomically persists a content row and its core-owned meta after
// verifying both the supplied and stored rows belong to contentType.
func (s *CommandService) Update(ctx context.Context, contentType string, item *Content, meta map[string]string) error {
	return s.UpdateOptimistic(ctx, contentType, item, meta, time.Time{})
}

// UpdateOptimistic applies a full framework Content update in one transaction.
// A non-zero expectedUpdatedAt prevents an Agent from overwriting a newer edit.
func (s *CommandService) UpdateOptimistic(ctx context.Context, contentType string, item *Content, meta map[string]string, expectedUpdatedAt time.Time) error {
	if item == nil || item.ID == 0 {
		return ErrContentNotFound
	}
	definition, err := s.definition(contentType)
	if err != nil {
		return err
	}
	if item.Type != contentType {
		return ErrContentTypeMismatch
	}
	if err := s.validateMutation(definition, item, meta); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	sanitizeContent(item)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored Content
		if err := tx.First(&stored, item.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentNotFound
			}
			return err
		}
		if stored.Type != contentType {
			return ErrContentNotFound
		}
		if !expectedUpdatedAt.IsZero() && !stored.UpdatedAt.Equal(expectedUpdatedAt) {
			return ErrOptimisticLock
		}
		now := s.now()
		updates := contentUpdateColumns(item, now)
		query := tx.Model(&Content{}).Where("id = ? AND type = ?", item.ID, contentType)
		if !expectedUpdatedAt.IsZero() {
			query = query.Where("updated_at = ?", stored.UpdatedAt)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrOptimisticLock
		}
		item.CreatedAt = stored.CreatedAt
		item.UpdatedAt = now
		if err := saveDeclaredMeta(tx, item.ID, meta); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		s.notify(ctx, MutationUpdated, item, meta)
	}
	return err
}

// PublishOne, MoveToTrash, and Restore are intentionally separate commands so
// transports cannot smuggle a higher-risk status change through generic update.
func (s *CommandService) PublishOne(ctx context.Context, contentType string, id uint, expectedUpdatedAt time.Time) (*Content, error) {
	return s.transition(ctx, contentType, id, expectedUpdatedAt, MutationPublished, func(item *Content, now time.Time) error {
		if item.Status == StatusTrash {
			return ErrInvalidTransition
		}
		item.Status = StatusPublished
		if item.PublishedAt == nil {
			item.PublishedAt = &now
		}
		return nil
	})
}

func (s *CommandService) MoveToTrash(ctx context.Context, contentType string, id uint, expectedUpdatedAt time.Time) (*Content, error) {
	return s.transition(ctx, contentType, id, expectedUpdatedAt, MutationTrashed, func(item *Content, _ time.Time) error {
		item.Status = StatusTrash
		return nil
	})
}

func (s *CommandService) Restore(ctx context.Context, contentType string, id uint, expectedUpdatedAt time.Time) (*Content, error) {
	return s.transition(ctx, contentType, id, expectedUpdatedAt, MutationRestored, func(item *Content, _ time.Time) error {
		if item.Status != StatusTrash {
			return ErrInvalidTransition
		}
		item.Status = StatusDraft
		return nil
	})
}

func (s *CommandService) transition(ctx context.Context, contentType string, id uint, expectedUpdatedAt time.Time, kind MutationKind, change func(*Content, time.Time) error) (*Content, error) {
	if _, err := s.definition(contentType); err != nil {
		return nil, err
	}
	if id == 0 || expectedUpdatedAt.IsZero() {
		return nil, ErrContentNotFound
	}
	ctx = nonNilContext(ctx)
	var item Content
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND type = ?", id, contentType).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentNotFound
			}
			return err
		}
		if !item.UpdatedAt.Equal(expectedUpdatedAt) {
			return ErrOptimisticLock
		}
		now := s.now()
		if err := change(&item, now); err != nil {
			return err
		}
		result := tx.Model(&Content{}).
			Where("id = ? AND type = ? AND updated_at = ?", id, contentType, item.UpdatedAt).
			Updates(map[string]any{"status": item.Status, "published_at": item.PublishedAt, "updated_at": now})
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
	s.notify(ctx, kind, &item, nil)
	return &item, nil
}

// Delete soft-deletes one item only when its stored type matches contentType.
func (s *CommandService) Delete(ctx context.Context, contentType string, id uint) error {
	if _, err := s.definition(contentType); err != nil {
		return err
	}
	if id == 0 {
		return ErrContentNotFound
	}
	result := s.db.WithContext(nonNilContext(ctx)).Where("id = ? AND type = ?", id, contentType).Delete(&Content{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrContentNotFound
	}
	return nil
}

// HardDelete permanently deletes matching rows and their content meta in one
// transaction. Callers may provide cleanup for other subsystem-owned rows.
func (s *CommandService) HardDelete(ctx context.Context, contentType string, ids []uint, cleanup RelatedDeleteFunc) (int, error) {
	if _, err := s.definition(contentType); err != nil {
		return 0, err
	}
	validIDs := NormalizeIDs(ids)
	if len(validIDs) == 0 {
		return 0, nil
	}
	ctx = nonNilContext(ctx)
	var matched []uint
	if err := s.db.WithContext(ctx).Model(&Content{}).
		Where("type = ? AND id IN ?", contentType, validIDs).
		Pluck("id", &matched).Error; err != nil {
		return 0, err
	}
	if len(matched) == 0 {
		return 0, nil
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if cleanup != nil {
			if err := cleanup(tx, matched); err != nil {
				return err
			}
		}
		if err := tx.Where("content_id IN ?", matched).Delete(&ContentMeta{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("type = ? AND id IN ?", contentType, matched).Delete(&Content{}).Error
	})
	if err != nil {
		return 0, err
	}
	return len(matched), nil
}

// Publish makes matching items visible and fills only missing publish times.
func (s *CommandService) Publish(ctx context.Context, contentType string, ids []uint) (int, error) {
	if _, err := s.definition(contentType); err != nil {
		return 0, err
	}
	validIDs := NormalizeIDs(ids)
	if len(validIDs) == 0 {
		return 0, nil
	}
	result := s.db.WithContext(nonNilContext(ctx)).Model(&Content{}).
		Where("type = ? AND id IN ?", contentType, validIDs).
		Updates(map[string]interface{}{
			"status":       StatusPublished,
			"published_at": gorm.Expr("COALESCE(published_at, ?)", s.now()),
		})
	return int(result.RowsAffected), result.Error
}

// Unpublish returns matching items to draft without changing publish times.
func (s *CommandService) Unpublish(ctx context.Context, contentType string, ids []uint) (int, error) {
	if _, err := s.definition(contentType); err != nil {
		return 0, err
	}
	validIDs := NormalizeIDs(ids)
	if len(validIDs) == 0 {
		return 0, nil
	}
	result := s.db.WithContext(nonNilContext(ctx)).Model(&Content{}).
		Where("type = ? AND id IN ?", contentType, validIDs).
		Update("status", StatusDraft)
	return int(result.RowsAffected), result.Error
}

// Reorder assigns stable 1-based sort positions inside one content type.
func (s *CommandService) Reorder(ctx context.Context, contentType string, ids []uint, offset int) error {
	definition, err := s.definition(contentType)
	if err != nil {
		return err
	}
	if !definition.SupportsFeature("sort_order") {
		return ErrReorderUnsupported
	}
	ids = NormalizeIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	return s.db.WithContext(nonNilContext(ctx)).Transaction(func(tx *gorm.DB) error {
		position := offset + 1
		for _, id := range ids {
			result := tx.Model(&Content{}).
				Where("id = ? AND type = ?", id, contentType).
				Update("sort_order", position)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				position++
			}
		}
		return nil
	})
}

// NormalizeIDs removes zero values and duplicates while preserving order.
func NormalizeIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func (s *CommandService) definition(contentType string) (*ContentTypeDef, error) {
	if s == nil || s.db == nil || s.registry == nil {
		return nil, ErrCommandUnavailable
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return nil, ErrContentTypeRequired
	}
	definition := s.registry.GetType(contentType)
	if definition == nil {
		return nil, fmt.Errorf("%w: %s", ErrContentTypeNotFound, contentType)
	}
	return definition, nil
}

func (s *CommandService) validateMutation(definition *ContentTypeDef, item *Content, meta map[string]string) error {
	if definition == nil || item == nil || item.Type != definition.Name {
		return ErrContentTypeMismatch
	}
	if definition.Rewrite.Rootless && IsReservedRootlessSlug(s.registry, item.Slug) {
		return ErrReservedSlug
	}
	switch item.Status {
	case StatusDraft, StatusPending, StatusPublished, StatusArchived:
	default:
		return fmt.Errorf("%w: %s", ErrInvalidStatus, item.Status)
	}
	allowed := declaredMetaKeys(definition)
	for key := range meta {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%w: %s", ErrUnsupportedMeta, key)
		}
	}
	if item.Status == StatusPublished && item.PublishedAt == nil {
		now := s.now()
		item.PublishedAt = &now
	}
	return nil
}

func declaredMetaKeys(definition *ContentTypeDef) map[string]struct{} {
	allowed := make(map[string]struct{}, len(definition.MetaFields)+3)
	for _, field := range definition.MetaFields {
		if key := strings.TrimSpace(field.Key); key != "" {
			allowed[key] = struct{}{}
		}
	}
	if definition.SupportsFeature("thumbnail") {
		allowed["gallery_images"] = struct{}{}
	}
	if definition.Rewrite.Rootless {
		allowed["page_template"] = struct{}{}
		allowed["embed_code"] = struct{}{}
	}
	return allowed
}

func saveDeclaredMeta(tx *gorm.DB, contentID uint, meta map[string]string) error {
	for key, value := range meta {
		var row ContentMeta
		result := tx.Where("content_id = ? AND meta_key = ?", contentID, key).First(&row)
		switch {
		case result.Error == nil:
			row.MetaValue = value
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		case errors.Is(result.Error, gorm.ErrRecordNotFound):
			row = ContentMeta{ContentID: contentID, MetaKey: key, MetaValue: value}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		default:
			return result.Error
		}
	}
	return nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contentUpdateColumns(item *Content, updatedAt time.Time) map[string]any {
	return map[string]any{
		"status": item.Status, "title": item.Title, "slug": item.Slug,
		"content": item.Content, "excerpt": item.Excerpt, "image_url": item.ImageURL,
		"author_id": item.AuthorID, "parent_id": item.ParentID, "sort_order": item.SortOrder,
		"comment_status": item.CommentStatus, "published_at": item.PublishedAt, "updated_at": updatedAt,
	}
}

func (s *CommandService) notify(ctx context.Context, kind MutationKind, item *Content, meta map[string]string) {
	if s == nil || s.observer == nil || item == nil {
		return
	}
	itemCopy := *item
	metaCopy := make(map[string]string, len(meta))
	for key, value := range meta {
		metaCopy[key] = value
	}
	s.observer(nonNilContext(ctx), Mutation{Kind: kind, Item: &itemCopy, Meta: metaCopy})
}
