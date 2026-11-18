// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package taxonomy

import (
	"context"
	"fmt"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Repository provides CRUD operations for terms and taxonomies.
type Repository struct {
	db  *gorm.DB
	ctx context.Context
}

// NewRepository creates a new taxonomy Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// WithContext returns a request-scoped repository clone. Existing callers that
// do not opt in keep the historical, unscoped behaviour.
func (r *Repository) WithContext(ctx context.Context) *Repository {
	if r == nil {
		return nil
	}
	clone := *r
	clone.ctx = nonNilContext(ctx)
	return &clone
}

func (r *Repository) context(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	if r != nil && r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// --- Term operations ---

// CreateTerm creates a new term.
func (r *Repository) CreateTerm(t *Term) error {
	return r.db.Create(t).Error
}

// GetTermBySlug finds a term by its slug.
func (r *Repository) GetTermBySlug(slug string) (*Term, error) {
	return r.GetTermBySlugContext(nil, slug)
}

// GetTermBySlugContext resolves a term inside the active taxonomy scope.
func (r *Repository) GetTermBySlugContext(ctx context.Context, slug string) (*Term, error) {
	ctx = r.context(ctx)
	var t Term
	q := r.db.WithContext(ctx)
	if len(Scopes(ctx)) > 0 {
		taxTable := Taxonomy{}.TableName()
		termTable := Term{}.TableName()
		q = ScopedDBContext(ctx, q.Model(&Taxonomy{})).
			Select(termTable+".*").
			Joins("JOIN "+termTable+" ON "+termTable+".id = "+taxTable+".term_id").
			Where(termTable+".slug = ?", slug)
	} else {
		q = q.Model(&Term{}).Where("slug = ?", slug)
	}
	err := q.First(&t).Error
	return &t, err
}

// --- Taxonomy operations ---

// CreateTaxonomy creates a new taxonomy entry linked to a term.
func (r *Repository) CreateTaxonomy(tax *Taxonomy) error {
	return r.db.Create(tax).Error
}

// GetTaxonomy returns a taxonomy by ID with its term loaded.
func (r *Repository) GetTaxonomy(id uint) (*Taxonomy, error) {
	return r.GetTaxonomyContext(nil, id)
}

// GetTaxonomyContext returns a taxonomy only when it belongs to the active
// request scope.
func (r *Repository) GetTaxonomyContext(ctx context.Context, id uint) (*Taxonomy, error) {
	ctx = r.context(ctx)
	var tax Taxonomy
	err := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).
		Preload("Term").Where(Taxonomy{}.TableName()+".id = ?", id).First(&tax).Error
	return &tax, err
}

// FindByTypeAndSlugContext resolves a taxonomy archive identity inside the
// active scope without exposing extension-specific language concepts to core.
func (r *Repository) FindByTypeAndSlugContext(ctx context.Context, taxonomyType, slug string) (*Taxonomy, error) {
	ctx = r.context(ctx)
	var item Taxonomy
	taxTable := Taxonomy{}.TableName()
	termTable := Term{}.TableName()
	err := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).
		Preload("Term").
		Joins("JOIN "+termTable+" ON "+termTable+".id = "+taxTable+".term_id").
		Where(taxTable+".taxonomy = ? AND "+termTable+".slug = ?", taxonomyType, slug).
		First(&item).Error
	return &item, err
}

// ListByTaxonomy returns all taxonomy entries of a given type (e.g. "category").
func (r *Repository) ListByTaxonomy(taxonomyType string) ([]Taxonomy, error) {
	return r.ListByTaxonomyContext(nil, taxonomyType)
}

// ListByTaxonomyContext propagates cancellation and deadlines from protocol-
// neutral request contexts.
func (r *Repository) ListByTaxonomyContext(ctx context.Context, taxonomyType string) ([]Taxonomy, error) {
	ctx = r.context(ctx)
	var items []Taxonomy
	err := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).Preload("Term").
		Where("taxonomy = ?", taxonomyType).
		Order("count DESC").
		Find(&items).Error
	return items, err
}

// ContentReferenceCounts returns the current number of active content rows
// referencing each term in a taxonomy. Unlike Taxonomy.Count (the published
// front-end count), this admin-facing aggregate includes drafts and archived
// content while excluding soft-deleted and trashed content.
func (r *Repository) ContentReferenceCounts(taxonomyType string) (map[uint]int64, error) {
	return r.ContentReferenceCountsContext(nil, taxonomyType)
}

// ContentReferenceCountsContext restricts aggregates to taxonomy identities
// visible in the active scope.
func (r *Repository) ContentReferenceCountsContext(ctx context.Context, taxonomyType string) (map[uint]int64, error) {
	ctx = r.context(ctx)
	var visibleIDs []uint
	if err := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).
		Where("taxonomy = ?", taxonomyType).Pluck("id", &visibleIDs).Error; err != nil {
		return nil, err
	}
	if len(visibleIDs) == 0 {
		return map[uint]int64{}, nil
	}
	tax := dbprefix.Table("taxonomies")
	tr := dbprefix.Table("term_relationships")
	ct := dbprefix.Table("contents")

	type referenceCountRow struct {
		TaxonomyID     uint
		ReferenceCount int64
	}
	var rows []referenceCountRow
	err := r.db.WithContext(ctx).Raw(fmt.Sprintf(`
		SELECT t.id AS taxonomy_id, COUNT(c.id) AS reference_count
		FROM %s t
		LEFT JOIN %s tr ON tr.taxonomy_id = t.id
		LEFT JOIN %s c ON c.id = tr.content_id
			AND c.deleted_at IS NULL
			AND c.status <> 'trash'
		WHERE t.taxonomy = ? AND t.id IN ?
		GROUP BY t.id`, tax, tr, ct), taxonomyType, visibleIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	counts := make(map[uint]int64, len(rows))
	for _, row := range rows {
		counts[row.TaxonomyID] = row.ReferenceCount
	}
	return counts, nil
}

// ListByTaxonomyTree returns a hierarchical tree for a taxonomy type.
func (r *Repository) ListByTaxonomyTree(taxonomyType string) ([]Taxonomy, error) {
	return r.ListByTaxonomyTreeContext(nil, taxonomyType)
}

// ListByTaxonomyTreeContext returns the scoped hierarchy.
func (r *Repository) ListByTaxonomyTreeContext(ctx context.Context, taxonomyType string) ([]Taxonomy, error) {
	all, err := r.ListByTaxonomyContext(ctx, taxonomyType)
	if err != nil {
		return nil, err
	}
	return buildTaxTree(all, nil), nil
}

// DeleteTaxonomy deletes a taxonomy entry and its relationships.
func (r *Repository) DeleteTaxonomy(id uint) error {
	r.db.Where("taxonomy_id = ?", id).Delete(&TermRelationship{})
	return r.db.Delete(&Taxonomy{}, id).Error
}

// --- Relationship operations ---

// SetContentTaxonomies replaces all taxonomy relationships for a content item.
func (r *Repository) SetContentTaxonomies(contentID uint, taxonomyIDs []uint) error {
	// Remove existing
	if err := r.db.Where("content_id = ?", contentID).Delete(&TermRelationship{}).Error; err != nil {
		return err
	}
	// Add new
	for i, taxID := range taxonomyIDs {
		rel := TermRelationship{
			ContentID:  contentID,
			TaxonomyID: taxID,
			SortOrder:  i,
		}
		if err := r.db.Create(&rel).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetContentTaxonomies returns all taxonomies for a content item.
func (r *Repository) GetContentTaxonomies(contentID uint, taxonomyType string) ([]Taxonomy, error) {
	return r.GetContentTaxonomiesContext(nil, contentID, taxonomyType)
}

// GetContentTaxonomiesContext returns relationships whose taxonomy identities
// are visible in the active request scope.
func (r *Repository) GetContentTaxonomiesContext(ctx context.Context, contentID uint, taxonomyType string) ([]Taxonomy, error) {
	ctx = r.context(ctx)
	var items []Taxonomy
	tr := dbprefix.Table("term_relationships")
	tax := dbprefix.Table("taxonomies")
	q := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).Preload("Term").
		Joins(fmt.Sprintf("JOIN %s tr ON tr.taxonomy_id = %s.id", tr, tax)).
		Where("tr.content_id = ?", contentID)
	if taxonomyType != "" {
		q = q.Where(tax+".taxonomy = ?", taxonomyType)
	}
	err := q.Find(&items).Error
	return items, err
}

// relationBatchSize bounds the number of content IDs bound into one
// ContentTaxonomiesForIDsContext query.
const relationBatchSize = 500

// ContentTaxonomiesForIDsContext returns the taxonomy rows attached to several
// content items, keyed by content ID, in one query per 500 IDs. Only rows
// visible in the active scope and, when names is not empty, of those taxonomy
// types are returned. Each item's rows are ordered by relationship sort order,
// then taxonomy ID, and carry their Term.
func (r *Repository) ContentTaxonomiesForIDsContext(ctx context.Context, contentIDs []uint, names []string) (map[uint][]Taxonomy, error) {
	ctx = r.context(ctx)
	result := make(map[uint][]Taxonomy, len(contentIDs))
	if len(contentIDs) == 0 {
		return result, nil
	}
	tax := dbprefix.Table("taxonomies")
	tr := dbprefix.Table("term_relationships")
	tm := dbprefix.Table("terms")
	type relationRow struct {
		ContentID   uint
		ID          uint
		TermID      uint
		Taxonomy    string
		Description string
		ParentID    *uint
		Count       int
		TermName    string
		TermSlug    string
	}
	for start := 0; start < len(contentIDs); start += relationBatchSize {
		end := min(start+relationBatchSize, len(contentIDs))
		q := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).
			Select(fmt.Sprintf("tr.content_id, %[1]s.id, %[1]s.term_id, %[1]s.taxonomy, %[1]s.description, %[1]s.parent_id, %[1]s.count, tm.name AS term_name, tm.slug AS term_slug", tax)).
			Joins(fmt.Sprintf("JOIN %s tr ON tr.taxonomy_id = %s.id", tr, tax)).
			Joins(fmt.Sprintf("JOIN %s tm ON tm.id = %s.term_id", tm, tax)).
			Where("tr.content_id IN ?", contentIDs[start:end])
		if len(names) > 0 {
			q = q.Where(tax+".taxonomy IN ?", names)
		}
		var rows []relationRow
		if err := q.Order("tr.content_id, tr.sort_order, " + tax + ".id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ContentID] = append(result[row.ContentID], Taxonomy{
				ID:          row.ID,
				TermID:      row.TermID,
				Taxonomy:    row.Taxonomy,
				Description: row.Description,
				ParentID:    row.ParentID,
				Count:       row.Count,
				Term:        Term{ID: row.TermID, Name: row.TermName, Slug: row.TermSlug},
			})
		}
	}
	return result, nil
}

// Orders accepted by ListPublicContext.
const (
	PublicOrderCount = "count" // published count descending
	PublicOrderName  = "name"  // term name ascending
)

// ListPublicContext returns at most limit taxonomy rows of one type visible
// in the active scope, for public listings. order is PublicOrderCount or
// PublicOrderName, with taxonomy ID as the tie-breaker. hideEmpty leaves out
// rows whose published count is zero.
func (r *Repository) ListPublicContext(ctx context.Context, taxonomyType, order string, limit int, hideEmpty bool) ([]Taxonomy, error) {
	if limit < 1 {
		return nil, fmt.Errorf("taxonomy: list limit %d must be positive", limit)
	}
	ctx = r.context(ctx)
	tax := dbprefix.Table("taxonomies")
	tm := dbprefix.Table("terms")
	q := ScopedDBContext(ctx, r.db.WithContext(ctx).Model(&Taxonomy{})).Preload("Term").
		Where(tax+".taxonomy = ?", taxonomyType)
	if hideEmpty {
		q = q.Where(tax + ".count > 0")
	}
	switch order {
	case PublicOrderCount:
		q = q.Order(tax + ".count DESC").Order(tax + ".id")
	case PublicOrderName:
		q = q.Joins(fmt.Sprintf("JOIN %[1]s ON %[1]s.id = %[2]s.term_id", tm, tax)).
			Order(tm + ".name").Order(tax + ".id")
	default:
		return nil, fmt.Errorf("taxonomy: unknown list order %q", order)
	}
	var items []Taxonomy
	err := q.Limit(limit).Find(&items).Error
	return items, err
}

// UpdateCounts recalculates the count field for all taxonomies of a given type.
func (r *Repository) UpdateCounts(taxonomyType string) error {
	tax := dbprefix.Table("taxonomies")
	tr := dbprefix.Table("term_relationships")
	ct := dbprefix.Table("contents")
	return r.db.Exec(fmt.Sprintf(`
		UPDATE %s SET count = (
			SELECT COUNT(*) FROM %s tr
			JOIN %s c ON c.id = tr.content_id
			WHERE tr.taxonomy_id = %s.id
			AND c.status = 'published' AND c.deleted_at IS NULL
		) WHERE taxonomy = ?`, tax, tr, ct, tax), taxonomyType).Error
}

// buildTaxTree converts a flat taxonomy list into a tree.
func buildTaxTree(items []Taxonomy, parentID *uint) []Taxonomy {
	var tree []Taxonomy
	for _, item := range items {
		if ptrEq(item.ParentID, parentID) {
			item.Children = buildTaxTree(items, &item.ID)
			tree = append(tree, item)
		}
	}
	return tree
}

func ptrEq(a, b *uint) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
