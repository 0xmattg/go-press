package taxonomy

import "go-press/pkg/dbprefix"

// Term represents a tag/category name and slug.
type Term struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:200;not null" json:"name"`
	Slug string `gorm:"size:200;uniqueIndex;not null" json:"slug"`
}

func (Term) TableName() string { return dbprefix.Table("terms") }

// Taxonomy associates a Term with a taxonomy type (e.g. "category", "tag", "product_cat").
type Taxonomy struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TermID      uint   `gorm:"not null" json:"term_id"`
	Taxonomy    string `gorm:"size:50;not null" json:"taxonomy"`
	Description string `gorm:"type:text" json:"description"`
	ParentID    *uint  `json:"parent_id"`
	Count       int    `gorm:"default:0" json:"count"`

	Term     Term       `gorm:"foreignKey:TermID" json:"term"`
	Children []Taxonomy `gorm:"-" json:"children,omitempty"`
}

func (Taxonomy) TableName() string { return dbprefix.Table("taxonomies") }

// TermRelationship links content to taxonomy.
type TermRelationship struct {
	ContentID  uint `gorm:"primaryKey" json:"content_id"`
	TaxonomyID uint `gorm:"primaryKey" json:"taxonomy_id"`
	SortOrder  int  `gorm:"default:0" json:"sort_order"`
}

func (TermRelationship) TableName() string { return dbprefix.Table("term_relationships") }
