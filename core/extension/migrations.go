// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"fmt"
	"time"

	"github.com/0xmattg/go-press/core/dbschema"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The structs below freeze the table layout as of the migration that creates
// it. They never change: later schema changes get their own migration step,
// while Package and SiteState may grow with them. The tables are not part of
// the AutoMigrate that runs on every start, so no binary, newer or older,
// rewrites their columns behind the migration history.

type extPackageV1 struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Kind         string    `gorm:"column:kind;size:16;not null"`
	Slug         string    `gorm:"column:slug;size:64;not null"`
	Version      string    `gorm:"column:version;size:64;not null"`
	Digest       string    `gorm:"column:digest;size:80;not null"`
	Size         int64     `gorm:"column:size;not null;default:0"`
	Source       string    `gorm:"column:source;size:16;not null"`
	Signed       bool      `gorm:"column:signed;not null;default:false"`
	PublisherKID string    `gorm:"column:publisher_kid;size:16;not null;default:''"`
	Status       string    `gorm:"column:status;size:16;not null"`
	IsCurrent    bool      `gorm:"column:is_current;not null;default:false"`
	InstalledBy  *uint     `gorm:"column:installed_by"`
	InstalledAt  time.Time `gorm:"column:installed_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (extPackageV1) TableName() string { return dbprefix.Table("ext_packages") }

type extSiteV1 struct {
	ID               int        `gorm:"column:id;primaryKey;autoIncrement:false"`
	Generation       int64      `gorm:"column:generation;not null;default:0"`
	CatalogSequence  int64      `gorm:"column:catalog_sequence;not null;default:0"`
	CatalogBody      string     `gorm:"column:catalog_body;type:text;not null;default:''"`
	CatalogFetchedAt *time.Time `gorm:"column:catalog_fetched_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;not null"`
}

func (extSiteV1) TableName() string { return dbprefix.Table("ext_site") }

// CreateTablesV1 is core migration step 2, "create_extension_tables". It
// creates ext_packages and ext_site, their indexes (named per site with
// dbschema.SiteIndexName so sites sharing a schema never collide), and the
// single ext_site row. Every part is skipped when already present, so the
// step is safe to run again.
func CreateTablesV1(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	migrator := db.Migrator()
	for _, model := range []interface{}{&extPackageV1{}, &extSiteV1{}} {
		if migrator.HasTable(model) {
			continue
		}
		if err := migrator.CreateTable(model); err != nil {
			return fmt.Errorf("create %T table: %w", model, err)
		}
	}

	packages := (extPackageV1{}).TableName()
	indexes := []struct{ name, sql string }{
		// One row per distinct package content.
		{"uniq_version", "CREATE UNIQUE INDEX IF NOT EXISTS ? ON ? (kind, slug, version, digest)"},
		// At most one current version per package.
		{"uniq_current", "CREATE UNIQUE INDEX IF NOT EXISTS ? ON ? (kind, slug) WHERE is_current"},
	}
	for _, index := range indexes {
		name := dbschema.SiteIndexName(packages, index.name)
		if err := db.Exec(index.sql, clause.Column{Name: name}, clause.Table{Name: packages}).Error; err != nil {
			return fmt.Errorf("create index %s: %w", name, err)
		}
	}

	site := (extSiteV1{}).TableName()
	err := db.Exec("INSERT INTO ? (id, generation, catalog_sequence, catalog_body, updated_at) SELECT ?, 0, 0, '', ? WHERE NOT EXISTS (SELECT 1 FROM ? WHERE id = ?)",
		clause.Table{Name: site}, siteStateID, time.Now().UTC(), clause.Table{Name: site}, siteStateID).Error
	if err != nil {
		return fmt.Errorf("create %s row: %w", site, err)
	}
	return nil
}
