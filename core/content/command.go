// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Content write path.
//
// CommandService is the framework-owned way to change content. Transports
// (admin handlers, public submission, Agent tools) parse input and enforce
// RBAC and ownership; this service enforces what must hold for every caller:
// the item belongs to a registered, writable type, a rootless slug does not
// shadow a system route, archive, or taxonomy, the status is valid, meta keys
// are declared by the type, and published items carry a publish time.
//
// Create and Update write the row, its declared meta, and any TxParticipant
// (for example taxonomy relationships) in one transaction. WithUniqueSlug
// reserves a free slug inside that transaction under an advisory lock.
// UpdateOptimistic and the single-item status transitions reject stale writes
// with ErrOptimisticLock, and a failed save leaves the caller's item as
// submitted. The MutationObserver runs only after commit, and only for Create,
// Update, and single-item transitions; the Engine turns it into content.*
// hooks and page-cache invalidation. Bulk operations do not notify.
//
// Repository write methods remain for legacy and extension-owned paths, but
// they bypass this validation and most content events, so new code that
// mutates content should come through here.

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
	ErrSlugRequired        = errors.New("content slug is required")
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

// MutationOption adjusts a single Create or Update command.
type MutationOption func(*mutationOptions)

type mutationOptions struct {
	uniqueSlug   bool
	participants []TxParticipant
}

// TxParticipant writes data that belongs to the same save as a content row,
// such as taxonomy relationships, inside the command's transaction. It runs
// after the row and its core meta are written; returning an error rolls the
// whole save back and no content event fires.
type TxParticipant func(tx *gorm.DB, item *Content) error

// WithTxParticipant adds a TxParticipant to one Create or Update command.
// Participants run in the order they were added.
func WithTxParticipant(participant TxParticipant) MutationOption {
	return func(o *mutationOptions) {
		if participant != nil {
			o.participants = append(o.participants, participant)
		}
	}
}

func runParticipants(tx *gorm.DB, item *Content, participants []TxParticipant) error {
	for _, participant := range participants {
		if err := participant(tx, item); err != nil {
			return err
		}
	}
	return nil
}

// WithUniqueSlug makes the command replace item.Slug with the first free slug
// derived from it within the request scopes of ctx, reserved inside the write
// transaction so concurrent writers cannot both claim it. Use it when the slug
// is new or was changed; leave it off for edits that keep the stored slug.
func WithUniqueSlug() MutationOption {
	return func(o *mutationOptions) { o.uniqueSlug = true }
}

func applyMutationOptions(opts []MutationOption) mutationOptions {
	var o mutationOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// Create validates and atomically persists a content row, its core-owned meta,
// and any TxParticipant writes. Extension-owned fields that are not
// participants continue to use extension hooks after this command completes.
// When the transaction rolls back, item keeps its submitted slug and no ID.
func (s *CommandService) Create(ctx context.Context, item *Content, meta map[string]string, opts ...MutationOption) error {
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
	options := applyMutationOptions(opts)
	ctx = nonNilContext(ctx)
	sanitizeContent(item)
	submittedSlug := item.Slug
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if options.uniqueSlug {
			slug, err := ReserveUniqueSlug(ctx, tx, item.Type, item.Slug, 0)
			if err != nil {
				return err
			}
			item.Slug = slug
		}
		if err := tx.Omit("Meta").Create(item).Error; err != nil {
			return err
		}
		if err := saveMeta(tx, item.ID, meta); err != nil {
			return err
		}
		return runParticipants(tx, item, options.participants)
	})
	if err != nil {
		// The insert was rolled back; do not let callers treat the ID GORM
		// assigned during the transaction as an existing row.
		item.ID = 0
		item.Slug = submittedSlug
		return err
	}
	s.notify(ctx, MutationCreated, item, meta)
	return nil
}

// Update atomically persists a content row and its core-owned meta after
// verifying both the supplied and stored rows belong to contentType.
func (s *CommandService) Update(ctx context.Context, contentType string, item *Content, meta map[string]string, opts ...MutationOption) error {
	return s.UpdateOptimistic(ctx, contentType, item, meta, time.Time{}, opts...)
}

// UpdateOptimistic applies a full framework Content update in one transaction.
// A non-zero expectedUpdatedAt prevents an Agent from overwriting a newer edit.
func (s *CommandService) UpdateOptimistic(ctx context.Context, contentType string, item *Content, meta map[string]string, expectedUpdatedAt time.Time, opts ...MutationOption) error {
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
	options := applyMutationOptions(opts)
	ctx = nonNilContext(ctx)
	sanitizeContent(item)
	submittedSlug, submittedUpdatedAt := item.Slug, item.UpdatedAt
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
		if options.uniqueSlug {
			slug, err := ReserveUniqueSlug(ctx, tx, contentType, item.Slug, item.ID)
			if err != nil {
				return err
			}
			item.Slug = slug
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
		if err := saveMeta(tx, item.ID, meta); err != nil {
			return err
		}
		return runParticipants(tx, item, options.participants)
	})
	if err != nil {
		item.Slug, item.UpdatedAt = submittedSlug, submittedUpdatedAt
		return err
	}
	s.notify(ctx, MutationUpdated, item, meta)
	return nil
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
