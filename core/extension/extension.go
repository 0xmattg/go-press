// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package extension records which extension packages a site has installed.
//
// Extension packages (theme packages first) are delivered as signed zip files.
// Their files live on disk under the site's packages directory; this package
// keeps only the metadata, in two tables created by the versioned core
// migration "create_extension_tables":
//
//   - <prefix>ext_packages has one row per installed version: identity,
//     content digest, origin, signature state, status, and whether it is the
//     slug's current version.
//   - <prefix>ext_site is a single row holding the site's generation counter,
//     which every change to installed packages increments so other processes
//     know to reload, and the last verified market catalog.
//
// Store reads and writes both. Policy (signature checks, retention, which
// theme is active) belongs to its callers.
package extension

import (
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"
)

// Package kinds.
const (
	KindTheme = "theme"
)

// Where a package came from.
const (
	SourceMarket = "market"
	SourceUpload = "upload"
)

// Package statuses.
const (
	// StatusReady packages can be activated.
	StatusReady = "ready"
	// StatusBroken packages failed their on-disk check and cannot be used
	// until reinstalled.
	StatusBroken = "broken"
	// StatusRevoked packages were withdrawn by the market and must not be
	// activated or upgraded.
	StatusRevoked = "revoked"
)

// siteStateID is the primary key of the single ext_site row.
const siteStateID = 1

// Package is one installed version of an extension package.
type Package struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	Kind         string    `gorm:"column:kind"`
	Slug         string    `gorm:"column:slug"`
	Version      string    `gorm:"column:version"`
	Digest       string    `gorm:"column:digest"`
	Size         int64     `gorm:"column:size"`
	Source       string    `gorm:"column:source"`
	Signed       bool      `gorm:"column:signed"`
	PublisherKID string    `gorm:"column:publisher_kid"`
	Status       string    `gorm:"column:status"`
	IsCurrent    bool      `gorm:"column:is_current"`
	InstalledBy  *uint     `gorm:"column:installed_by"`
	InstalledAt  time.Time `gorm:"column:installed_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

// TableName returns the site's ext_packages table.
func (Package) TableName() string { return dbprefix.Table("ext_packages") }

// SiteState is the single ext_site row.
type SiteState struct {
	ID               int        `gorm:"column:id;primaryKey"`
	Generation       int64      `gorm:"column:generation"`
	CatalogSequence  int64      `gorm:"column:catalog_sequence"`
	CatalogBody      string     `gorm:"column:catalog_body"`
	CatalogFetchedAt *time.Time `gorm:"column:catalog_fetched_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

// TableName returns the site's ext_site table.
func (SiteState) TableName() string { return dbprefix.Table("ext_site") }

func validSource(source string) bool {
	return source == SourceMarket || source == SourceUpload
}

func validStatus(status string) bool {
	return status == StatusReady || status == StatusBroken || status == StatusRevoked
}
