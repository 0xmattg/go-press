package taxonomy

import (
	"context"
	"errors"
	"strings"

	"github.com/0xmattg/go-press/core/content"
	"gorm.io/gorm"
)

var (
	ErrCommandUnavailable       = errors.New("taxonomy command service unavailable")
	ErrTaxonomyRequired         = errors.New("taxonomy type is required")
	ErrTaxonomyNotFound         = errors.New("taxonomy type is not registered")
	ErrTaxonomyItemNotFound     = errors.New("taxonomy item not found")
	ErrTaxonomyTypeMismatch     = errors.New("taxonomy item does not belong to the requested type")
	ErrTaxonomyNameRequired     = errors.New("taxonomy term name is required")
	ErrTaxonomySlugRequired     = errors.New("taxonomy term slug is required")
	ErrTaxonomySlugConflict     = errors.New("taxonomy term slug already exists in this scope")
	ErrInvalidTaxonomyParent    = errors.New("taxonomy parent is invalid for this scope")
	ErrInvalidTaxonomySelection = errors.New("taxonomy selection contains an invalid or out-of-scope item")
)

type MutationKind string

const (
	MutationCreated MutationKind = "created"
	MutationUpdated MutationKind = "updated"
	MutationDeleted MutationKind = "deleted"
)

type Mutation struct {
	Kind MutationKind
	Item *Taxonomy
}

type MutationObserver func(context.Context, Mutation)

// CommandService owns taxonomy mutation invariants. Transports enforce RBAC;
// this service validates registered taxonomy types, request scopes, parents,
// slug collisions, relationship IDs, and transaction boundaries.
type CommandService struct {
	db       *gorm.DB
	registry *content.Registry
	observer MutationObserver
}

func NewCommandService(db *gorm.DB, registry *content.Registry) *CommandService {
	return &CommandService{db: db, registry: registry}
}

func (s *CommandService) SetMutationObserver(observer MutationObserver) {
	if s != nil {
		s.observer = observer
	}
}

func (s *CommandService) Create(ctx context.Context, item *Taxonomy) error {
	if item == nil {
		return ErrTaxonomyItemNotFound
	}
	if err := s.validateDefinition(item.Taxonomy); err != nil {
		return err
	}
	normalize(item)
	if err := validateFields(item); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.validateParent(ctx, tx, item.Taxonomy, item.ParentID, 0); err != nil {
			return err
		}
		if err := s.ensureSlugAvailable(ctx, tx, item.Taxonomy, item.Term.Slug, 0, ""); err != nil {
			return err
		}
		if err := tx.Create(&item.Term).Error; err != nil {
			return err
		}
		item.TermID = item.Term.ID
		return tx.Omit("Term", "Children").Create(item).Error
	})
	if err == nil {
		s.notify(ctx, MutationCreated, item)
	}
	return err
}

func (s *CommandService) Update(ctx context.Context, taxonomyType string, item *Taxonomy) error {
	if item == nil || item.ID == 0 {
		return ErrTaxonomyItemNotFound
	}
	if err := s.validateDefinition(taxonomyType); err != nil {
		return err
	}
	if item.Taxonomy != "" && item.Taxonomy != taxonomyType {
		return ErrTaxonomyTypeMismatch
	}
	item.Taxonomy = taxonomyType
	normalize(item)
	if err := validateFields(item); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stored, err := getTaxonomy(ctx, tx, item.ID)
		if err != nil || stored.Taxonomy != taxonomyType {
			return ErrTaxonomyItemNotFound
		}
		if err := s.validateParent(ctx, tx, taxonomyType, item.ParentID, item.ID); err != nil {
			return err
		}
		if err := s.ensureSlugAvailable(ctx, tx, taxonomyType, item.Term.Slug, item.ID, stored.Term.Slug); err != nil {
			return err
		}
		if err := tx.Model(&Term{}).Where("id = ?", stored.TermID).Updates(map[string]interface{}{
			"name": item.Term.Name,
			"slug": item.Term.Slug,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Taxonomy{}).Where("id = ? AND taxonomy = ?", item.ID, taxonomyType).Updates(map[string]interface{}{
			"description": item.Description,
			"parent_id":   item.ParentID,
		}).Error; err != nil {
			return err
		}
		item.TermID = stored.TermID
		item.Term.ID = stored.TermID
		item.Count = stored.Count
		return nil
	})
	if err == nil {
		s.notify(ctx, MutationUpdated, item)
	}
	return err
}

func (s *CommandService) Delete(ctx context.Context, taxonomyType string, id uint) error {
	if id == 0 {
		return ErrTaxonomyItemNotFound
	}
	if err := s.validateDefinition(taxonomyType); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	var deleted *Taxonomy
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := getTaxonomy(ctx, tx, id)
		if err != nil || item.Taxonomy != taxonomyType {
			return ErrTaxonomyItemNotFound
		}
		deleted = item
		if err := tx.Where("taxonomy_id = ?", id).Delete(&TermRelationship{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Taxonomy{}).
			Where("taxonomy = ? AND parent_id = ?", taxonomyType, id).
			Update("parent_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND taxonomy = ?", id, taxonomyType).Delete(&Taxonomy{}).Error; err != nil {
			return err
		}
		var references int64
		if err := tx.Model(&Taxonomy{}).Where("term_id = ?", item.TermID).Count(&references).Error; err != nil {
			return err
		}
		if references == 0 {
			return tx.Delete(&Term{}, item.TermID).Error
		}
		return nil
	})
	if err == nil && deleted != nil {
		s.notify(ctx, MutationDeleted, deleted)
	}
	return err
}

// SetContentTaxonomies replaces relationships owned by allowedTypes after
// verifying every submitted taxonomy ID belongs to one of those types and the
// active request scope. Relationships for unrelated taxonomy types survive.
func (s *CommandService) SetContentTaxonomies(ctx context.Context, contentID uint, allowedTypes []string, taxonomyIDs []uint) error {
	if contentID == 0 {
		return ErrInvalidTaxonomySelection
	}
	allowedTypes = uniqueStrings(allowedTypes)
	if len(allowedTypes) == 0 && len(taxonomyIDs) > 0 {
		return ErrInvalidTaxonomySelection
	}
	for _, taxonomyType := range allowedTypes {
		if err := s.validateDefinition(taxonomyType); err != nil {
			return err
		}
	}
	taxonomyIDs = uniqueUint(taxonomyIDs)
	ctx = nonNilContext(ctx)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateContentTaxonomyIDs(ctx, tx, allowedTypes, taxonomyIDs); err != nil {
			return err
		}

		deleteQuery := tx.Where("content_id = ?", contentID)
		if len(allowedTypes) > 0 {
			deleteQuery = deleteQuery.Where("taxonomy_id IN (?)",
				tx.Model(&Taxonomy{}).Select("id").Where("taxonomy IN ?", allowedTypes))
		}
		if err := deleteQuery.Delete(&TermRelationship{}).Error; err != nil {
			return err
		}
		if len(taxonomyIDs) == 0 {
			return nil
		}
		relations := make([]TermRelationship, 0, len(taxonomyIDs))
		for index, taxonomyID := range taxonomyIDs {
			relations = append(relations, TermRelationship{ContentID: contentID, TaxonomyID: taxonomyID, SortOrder: index})
		}
		return tx.Create(&relations).Error
	})
}

// ValidateContentTaxonomies performs the same type/scope checks as the setter
// without writing. Admin create flows use it before persisting a new content
// row, then SetContentTaxonomies validates again inside its transaction.
func (s *CommandService) ValidateContentTaxonomies(ctx context.Context, allowedTypes []string, taxonomyIDs []uint) error {
	if s == nil || s.db == nil {
		return ErrCommandUnavailable
	}
	allowedTypes = uniqueStrings(allowedTypes)
	taxonomyIDs = uniqueUint(taxonomyIDs)
	if len(allowedTypes) == 0 && len(taxonomyIDs) > 0 {
		return ErrInvalidTaxonomySelection
	}
	for _, taxonomyType := range allowedTypes {
		if err := s.validateDefinition(taxonomyType); err != nil {
			return err
		}
	}
	return validateContentTaxonomyIDs(nonNilContext(ctx), s.db, allowedTypes, taxonomyIDs)
}

func validateContentTaxonomyIDs(ctx context.Context, db *gorm.DB, allowedTypes []string, taxonomyIDs []uint) error {
	if len(taxonomyIDs) == 0 {
		return nil
	}
	var count int64
	q := ScopedDBContext(ctx, db.Model(&Taxonomy{})).
		Where("id IN ? AND taxonomy IN ?", taxonomyIDs, allowedTypes)
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(taxonomyIDs)) {
		return ErrInvalidTaxonomySelection
	}
	return nil
}

func (s *CommandService) validateDefinition(taxonomyType string) error {
	taxonomyType = strings.TrimSpace(taxonomyType)
	if taxonomyType == "" {
		return ErrTaxonomyRequired
	}
	if s == nil || s.db == nil || s.registry == nil {
		return ErrCommandUnavailable
	}
	if s.registry.GetTaxonomy(taxonomyType) == nil {
		return ErrTaxonomyNotFound
	}
	return nil
}

func (s *CommandService) validateParent(ctx context.Context, tx *gorm.DB, taxonomyType string, parentID *uint, selfID uint) error {
	if parentID == nil {
		return nil
	}
	definition := s.registry.GetTaxonomy(taxonomyType)
	if definition == nil || !definition.Hierarchical {
		return ErrInvalidTaxonomyParent
	}
	if *parentID == 0 || *parentID == selfID {
		return ErrInvalidTaxonomyParent
	}
	parent, err := getTaxonomy(ctx, tx, *parentID)
	if err != nil || parent.Taxonomy != taxonomyType {
		return ErrInvalidTaxonomyParent
	}
	seen := map[uint]struct{}{parent.ID: {}}
	for parent.ParentID != nil {
		if *parent.ParentID == selfID {
			return ErrInvalidTaxonomyParent
		}
		if _, exists := seen[*parent.ParentID]; exists {
			return ErrInvalidTaxonomyParent
		}
		seen[*parent.ParentID] = struct{}{}
		parent, err = getTaxonomy(ctx, tx, *parent.ParentID)
		if err != nil || parent.Taxonomy != taxonomyType {
			return ErrInvalidTaxonomyParent
		}
	}
	return nil
}

func (s *CommandService) ensureSlugAvailable(ctx context.Context, tx *gorm.DB, taxonomyType, slug string, excludeID uint, storedSlug string) error {
	// Preserve the legacy global uniqueness rule when no extension scope is
	// active. An unchanged slug remains editable after translated rows with the
	// same slug have been added through scoped requests.
	if len(Scopes(ctx)) == 0 {
		if excludeID > 0 && slug == storedSlug {
			return nil
		}
		var count int64
		q := tx.Model(&Term{}).Where("slug = ?", slug)
		if excludeID > 0 {
			q = q.Where("id NOT IN (?)", tx.Model(&Taxonomy{}).Select("term_id").Where("id = ?", excludeID))
		}
		if err := q.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrTaxonomySlugConflict
		}
		return nil
	}

	var count int64
	q := ScopedDBContext(ctx, tx.Model(&Taxonomy{})).
		Joins("JOIN "+Term{}.TableName()+" scoped_terms ON scoped_terms.id = "+Taxonomy{}.TableName()+".term_id").
		Where("scoped_terms.slug = ?", slug)
	if excludeID > 0 {
		q = q.Where(Taxonomy{}.TableName()+".id <> ?", excludeID)
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrTaxonomySlugConflict
	}
	return nil
}

func getTaxonomy(ctx context.Context, db *gorm.DB, id uint) (*Taxonomy, error) {
	var item Taxonomy
	err := ScopedDBContext(ctx, db.Model(&Taxonomy{})).Preload("Term").Where("id = ?", id).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func normalize(item *Taxonomy) {
	item.Taxonomy = strings.TrimSpace(item.Taxonomy)
	item.Description = strings.TrimSpace(item.Description)
	item.Term.Name = strings.TrimSpace(item.Term.Name)
	item.Term.Slug = strings.Trim(strings.TrimSpace(item.Term.Slug), "/")
}

func validateFields(item *Taxonomy) error {
	if item.Term.Name == "" {
		return ErrTaxonomyNameRequired
	}
	if item.Term.Slug == "" {
		return ErrTaxonomySlugRequired
	}
	return nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueUint(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *CommandService) notify(ctx context.Context, kind MutationKind, item *Taxonomy) {
	if s != nil && s.observer != nil {
		s.observer(ctx, Mutation{Kind: kind, Item: item})
	}
}
