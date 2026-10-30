// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package taxonomy stores categories, tags, and any other taxonomy declared in
// the content registry.
//
// The schema follows WordPress: a Term holds the name and slug, a Taxonomy row
// places a term in one taxonomy (with an optional parent for hierarchical
// ones), and TermRelationship links content items to taxonomy rows. Which
// taxonomies exist, and which content types may use them, is decided by
// content.Registry, not by this package.
//
// Repository handles reads and CommandService handles writes. CommandService
// requires a registered taxonomy, a valid parent, and a free slug, and
// relationship updates may only reference taxonomy rows of the types the
// caller allows for the content item. SetContentTaxonomiesTx lets a content
// save update relationships inside its own transaction. Like content,
// taxonomy queries honor request Scopes, so plugins such as multilang can keep
// per-language terms apart or share them.
package taxonomy

import "github.com/0xmattg/go-press/pkg/dbprefix"

// Term represents a tag/category name and slug.
type Term struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:200;not null" json:"name"`
	Slug string `gorm:"size:200;index;not null" json:"slug"`
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
	TaxonomyID uint `gorm:"primaryKey;index" json:"taxonomy_id"`
	SortOrder  int  `gorm:"default:0" json:"sort_order"`
}

func (TermRelationship) TableName() string { return dbprefix.Table("term_relationships") }
