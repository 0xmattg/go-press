// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrNotFound reports an unknown package ID.
	ErrNotFound = errors.New("extension: package not found")
	// ErrCatalogRollback reports a catalog older than the one already stored.
	ErrCatalogRollback = errors.New("extension: catalog sequence is older than the stored catalog")
	// ErrNotMigrated reports that the ext_site row is missing, which means
	// core migration step 2 has not run.
	ErrNotMigrated = errors.New("extension: ext_site row missing; run the core migrations")
)

// Store reads and writes extension package metadata.
//
// Every write that changes installed packages runs in one transaction that
// first increments the site generation. That UPDATE locks the ext_site row,
// so concurrent writers from any process queue up instead of interleaving.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store over db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Generation returns the site generation, which every change to installed
// packages increments.
func (s *Store) Generation(ctx context.Context) (int64, error) {
	state, err := s.siteState(s.db.WithContext(ctx))
	return state.Generation, err
}

// Packages returns every installed version of kind, ordered by slug and then
// newest first.
func (s *Store) Packages(ctx context.Context, kind string) ([]Package, error) {
	var out []Package
	err := s.db.WithContext(ctx).Where("kind = ?", kind).
		Order("slug, installed_at DESC, id DESC").Find(&out).Error
	return out, err
}

// Versions returns the installed versions of one package, newest first.
func (s *Store) Versions(ctx context.Context, kind, slug string) ([]Package, error) {
	var out []Package
	err := s.db.WithContext(ctx).Where("kind = ? AND slug = ?", kind, slug).
		Order("installed_at DESC, id DESC").Find(&out).Error
	return out, err
}

// Current returns the current version of a package, or nil when it has none.
func (s *Store) Current(ctx context.Context, kind, slug string) (*Package, error) {
	var pkg Package
	err := s.db.WithContext(ctx).Where("kind = ? AND slug = ? AND is_current = ?", kind, slug, true).
		Take(&pkg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pkg, nil
}

// Get returns one installed version by ID.
func (s *Store) Get(ctx context.Context, id int64) (*Package, error) {
	var pkg Package
	err := s.db.WithContext(ctx).Take(&pkg, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pkg, nil
}

// Add records a newly installed version. With makeCurrent it also becomes
// the package's current version. A zero InstalledAt is set to now and an
// empty Status to StatusReady; pkg.ID is filled in.
func (s *Store) Add(ctx context.Context, pkg *Package, makeCurrent bool) error {
	if pkg == nil {
		return errors.New("extension: package is nil")
	}
	if err := validatePackage(pkg); err != nil {
		return err
	}
	now := time.Now().UTC()
	if pkg.InstalledAt.IsZero() {
		pkg.InstalledAt = now
	}
	pkg.UpdatedAt = now
	pkg.IsCurrent = makeCurrent
	return s.write(ctx, func(tx *gorm.DB) error {
		if makeCurrent {
			if err := clearCurrent(tx, pkg.Kind, pkg.Slug, 0); err != nil {
				return err
			}
		}
		pkg.ID = 0
		return tx.Create(pkg).Error
	})
}

// SetCurrent makes an installed version its package's current version.
func (s *Store) SetCurrent(ctx context.Context, id int64) error {
	return s.write(ctx, func(tx *gorm.DB) error {
		var pkg Package
		if err := tx.Take(&pkg, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if pkg.IsCurrent {
			return nil
		}
		if err := clearCurrent(tx, pkg.Kind, pkg.Slug, id); err != nil {
			return err
		}
		return tx.Model(&Package{}).Where("id = ?", id).
			Updates(map[string]interface{}{"is_current": true, "updated_at": time.Now().UTC()}).Error
	})
}

// SetStatus changes the status of an installed version.
func (s *Store) SetStatus(ctx context.Context, id int64, status string) error {
	if !validStatus(status) {
		return fmt.Errorf("extension: unknown status %q", status)
	}
	return s.write(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&Package{}).Where("id = ?", id).
			Updates(map[string]interface{}{"status": status, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Delete removes installed versions, for an uninstall or retention cleanup.
// Unknown IDs are ignored; nothing changes when none matches.
func (s *Store) Delete(ctx context.Context, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.write(ctx, func(tx *gorm.DB) error {
		return tx.Where("id IN ?", ids).Delete(&Package{}).Error
	})
}

// Catalog returns the stored catalog: its sequence (0 when none), body, and
// fetch time.
func (s *Store) Catalog(ctx context.Context) (SiteState, error) {
	return s.siteState(s.db.WithContext(ctx))
}

// SaveCatalog stores a verified catalog. A sequence lower than the stored
// one returns ErrCatalogRollback; the same sequence is accepted, since the
// market may serve the same catalog again. It does not change the
// generation: installed packages are unaffected.
func (s *Store) SaveCatalog(ctx context.Context, sequence int64, body string, fetchedAt time.Time) error {
	return s.ReplaceCatalog(ctx, fetchedAt, func(stored SiteState) (int64, string, error) {
		if sequence < stored.CatalogSequence {
			return 0, "", ErrCatalogRollback
		}
		return sequence, body, nil
	})
}

// ReplaceCatalog stores the catalog that decide returns. decide sees the
// stored catalog while the ext_site row is locked, so the comparison and the
// write are atomic across processes; an error from decide stores nothing.
// Like SaveCatalog, it does not change the generation.
func (s *Store) ReplaceCatalog(ctx context.Context, fetchedAt time.Time, decide func(stored SiteState) (sequence int64, body string, err error)) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Touch the row first to take its lock, the same way write does.
		now := time.Now().UTC()
		result := tx.Model(&SiteState{}).Where("id = ?", siteStateID).
			Updates(map[string]interface{}{"updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotMigrated
		}
		stored, err := s.siteState(tx)
		if err != nil {
			return err
		}
		sequence, body, err := decide(stored)
		if err != nil {
			return err
		}
		if sequence <= 0 {
			return fmt.Errorf("extension: catalog sequence must be positive, got %d", sequence)
		}
		return tx.Model(&SiteState{}).Where("id = ?", siteStateID).
			Updates(map[string]interface{}{
				"catalog_sequence":   sequence,
				"catalog_body":       body,
				"catalog_fetched_at": fetchedAt.UTC(),
				"updated_at":         now,
			}).Error
	})
}

// write runs fn in a transaction that first increments the generation, so
// writers are serialized on the ext_site row lock.
func (s *Store) write(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&SiteState{}).Where("id = ?", siteStateID).
			Updates(map[string]interface{}{"generation": gorm.Expr("generation + 1"), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotMigrated
		}
		return fn(tx)
	})
}

func (s *Store) siteState(db *gorm.DB) (SiteState, error) {
	var state SiteState
	err := db.Take(&state, "id = ?", siteStateID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SiteState{}, ErrNotMigrated
	}
	return state, err
}

// clearCurrent unsets the current version of a package, except keepID.
func clearCurrent(tx *gorm.DB, kind, slug string, keepID int64) error {
	return tx.Model(&Package{}).
		Where("kind = ? AND slug = ? AND is_current = ? AND id <> ?", kind, slug, true, keepID).
		Updates(map[string]interface{}{"is_current": false, "updated_at": time.Now().UTC()}).Error
}

func validatePackage(pkg *Package) error {
	if pkg.Status == "" {
		pkg.Status = StatusReady
	}
	switch {
	case strings.TrimSpace(pkg.Kind) == "":
		return errors.New("extension: package kind is required")
	case strings.TrimSpace(pkg.Slug) == "":
		return errors.New("extension: package slug is required")
	case strings.TrimSpace(pkg.Version) == "":
		return errors.New("extension: package version is required")
	case !strings.HasPrefix(pkg.Digest, "h1:"):
		return fmt.Errorf("extension: package digest %q is not an h1 digest", pkg.Digest)
	case !validSource(pkg.Source):
		return fmt.Errorf("extension: unknown package source %q", pkg.Source)
	case !validStatus(pkg.Status):
		return fmt.Errorf("extension: unknown package status %q", pkg.Status)
	case pkg.Size < 0:
		return errors.New("extension: package size is negative")
	}
	return nil
}
