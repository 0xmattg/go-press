package taxonomy

import (
	"fmt"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Repository provides CRUD operations for terms and taxonomies.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new taxonomy Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// --- Term operations ---

// CreateTerm creates a new term.
func (r *Repository) CreateTerm(t *Term) error {
	return r.db.Create(t).Error
}

// GetTermBySlug finds a term by its slug.
func (r *Repository) GetTermBySlug(slug string) (*Term, error) {
	var t Term
	err := r.db.Where("slug = ?", slug).First(&t).Error
	return &t, err
}

// --- Taxonomy operations ---

// CreateTaxonomy creates a new taxonomy entry linked to a term.
func (r *Repository) CreateTaxonomy(tax *Taxonomy) error {
	return r.db.Create(tax).Error
}

// GetTaxonomy returns a taxonomy by ID with its term loaded.
func (r *Repository) GetTaxonomy(id uint) (*Taxonomy, error) {
	var tax Taxonomy
	err := r.db.Preload("Term").First(&tax, id).Error
	return &tax, err
}

// ListByTaxonomy returns all taxonomy entries of a given type (e.g. "category").
func (r *Repository) ListByTaxonomy(taxonomyType string) ([]Taxonomy, error) {
	var items []Taxonomy
	err := r.db.Preload("Term").
		Where("taxonomy = ?", taxonomyType).
		Order("count DESC").
		Find(&items).Error
	return items, err
}

// ListByTaxonomyTree returns a hierarchical tree for a taxonomy type.
func (r *Repository) ListByTaxonomyTree(taxonomyType string) ([]Taxonomy, error) {
	all, err := r.ListByTaxonomy(taxonomyType)
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
	var items []Taxonomy
	tr := dbprefix.Table("term_relationships")
	tax := dbprefix.Table("taxonomies")
	q := r.db.Preload("Term").
		Joins(fmt.Sprintf("JOIN %s tr ON tr.taxonomy_id = %s.id", tr, tax)).
		Where("tr.content_id = ?", contentID)
	if taxonomyType != "" {
		q = q.Where(tax+".taxonomy = ?", taxonomyType)
	}
	err := q.Find(&items).Error
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
