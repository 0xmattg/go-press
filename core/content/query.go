package content

import (
	"fmt"
	"math"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// PaginatedResult is the common response shape for list queries.
//
// It is used by front-end archive rendering and can be reused by APIs that need
// the same page/per-page/total metadata as the admin UI.
type PaginatedResult struct {
	Items      []Content `json:"items"`
	Total      int64     `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`
}

// ContentQuery provides a chainable query builder for Content.
//
// It wraps a cloned GORM statement and exposes CMS-oriented filters such as
// Type, Published, Taxonomy, and Meta. Methods mutate and return the same query
// instance, so callers should create a fresh query for independent branches.
type ContentQuery struct {
	db *gorm.DB
}

// NewQuery creates a new ContentQuery from a gorm.DB instance.
//
// The base query excludes soft-deleted rows. Callers that need request-specific
// plugin filters should pass a database returned by ScopedDB.
func NewQuery(db *gorm.DB) *ContentQuery {
	t := dbprefix.Table("contents")
	return &ContentQuery{
		db: db.Model(&Content{}).Where(t + ".deleted_at IS NULL"),
	}
}

// Type filters by content type.
func (q *ContentQuery) Type(t string) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".type = ?", t)
	return q
}

// Types filters by multiple content types.
func (q *ContentQuery) Types(types []string) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".type IN ?", types)
	return q
}

// Status filters by content status.
func (q *ContentQuery) Status(s string) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".status = ?", s)
	return q
}

// Published limits results to rows visible on the public site.
//
// A row must have StatusPublished and either no explicit publish time or a
// PublishedAt that is not in the future. NULL PublishedAt is treated as
// immediately published for custom content types that do not expose a
// publish_date field, while future timestamps still support scheduled content.
func (q *ContentQuery) Published() *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".status = ? AND ("+tbl+".published_at IS NULL OR "+tbl+".published_at <= NOW())", StatusPublished)
	return q
}

// Author filters by author ID.
func (q *ContentQuery) Author(id uint) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".author_id = ?", id)
	return q
}

// Parent filters by parent content ID.
func (q *ContentQuery) Parent(id uint) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".parent_id = ?", id)
	return q
}

// Slug filters by exact slug match.
func (q *ContentQuery) Slug(slug string) *ContentQuery {
	tbl := dbprefix.Table("contents")
	q.db = q.db.Where(tbl+".slug = ?", slug)
	return q
}

// Search performs a ILIKE search on title and content.
func (q *ContentQuery) Search(keyword string) *ContentQuery {
	tbl := dbprefix.Table("contents")
	pattern := "%" + keyword + "%"
	q.db = q.db.Where("("+tbl+".title ILIKE ? OR "+tbl+".content ILIKE ?)", pattern, pattern)
	return q
}

// Taxonomy filters by a taxonomy name and term slug.
//
// It joins through term_relationships, taxonomies, and terms. Because it adds
// joins with fixed aliases, callers should avoid applying multiple Taxonomy
// filters to the same ContentQuery until aliasing support is added.
func (q *ContentQuery) Taxonomy(taxonomy, termSlug string) *ContentQuery {
	ct := dbprefix.Table("contents")
	tr := dbprefix.Table("term_relationships")
	tax := dbprefix.Table("taxonomies")
	tm := dbprefix.Table("terms")
	q.db = q.db.
		Joins(fmt.Sprintf("JOIN %s tr ON tr.content_id = %s.id", tr, ct)).
		Joins(fmt.Sprintf("JOIN %s tax ON tax.id = tr.taxonomy_id", tax)).
		Joins(fmt.Sprintf("JOIN %s t ON t.id = tax.term_id", tm)).
		Where("tax.taxonomy = ? AND t.slug = ?", taxonomy, termSlug)
	return q
}

// Meta filters by an exact content_meta key/value pair.
//
// Meta values are stored as strings, so callers that need numeric or boolean
// comparisons should normalize values before saving or add a domain-specific
// repository method.
func (q *ContentQuery) Meta(key, value string) *ContentQuery {
	ct := dbprefix.Table("contents")
	cm := dbprefix.Table("content_meta")
	q.db = q.db.
		Joins(fmt.Sprintf("JOIN %s cm ON cm.content_id = %s.id", cm, ct)).
		Where("cm.meta_key = ? AND cm.meta_value = ?", key, value)
	return q
}

// OrderBy adds an ORDER BY clause.
func (q *ContentQuery) OrderBy(field, dir string) *ContentQuery {
	q.db = q.db.Order(field + " " + dir)
	return q
}

// Limit sets the maximum number of results.
func (q *ContentQuery) Limit(n int) *ContentQuery {
	q.db = q.db.Limit(n)
	return q
}

// Offset sets the result offset.
func (q *ContentQuery) Offset(n int) *ContentQuery {
	q.db = q.db.Offset(n)
	return q
}

// WithMeta preloads the Meta association.
func (q *ContentQuery) WithMeta() *ContentQuery {
	q.db = q.db.Preload("Meta")
	return q
}

// Get executes the query and returns all matching content.
func (q *ContentQuery) Get() ([]Content, error) {
	var results []Content
	err := q.db.Find(&results).Error
	return results, err
}

// First returns the first matching content item.
func (q *ContentQuery) First() (*Content, error) {
	var result Content
	err := q.db.First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Count returns the number of matching records.
func (q *ContentQuery) Count() (int64, error) {
	var count int64
	err := q.db.Count(&count).Error
	return count, err
}

// Paginate returns a paginated result set.
//
// Page and perPage values less than one are normalized to safe defaults. Count
// and list queries share the current filters, so call OrderBy before Paginate
// when deterministic ordering matters.
func (q *ContentQuery) Paginate(page, perPage int) (*PaginatedResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}

	var total int64
	countDB := q.db.Session(&gorm.Session{})
	if err := countDB.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []Content
	offset := (page - 1) * perPage
	if err := q.db.Offset(offset).Limit(perPage).Find(&items).Error; err != nil {
		return nil, err
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))

	return &PaginatedResult{
		Items:      items,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
}
