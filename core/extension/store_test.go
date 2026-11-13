// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// sqliteStore opens SQLite behind the postgres dialector, like other core
// unit tests, and creates the tables with SQLite DDL equivalent to
// CreateTablesV1 (the step itself needs PostgreSQL; see store_pg_test.go).
func sqliteStore(t *testing.T, withSiteRow bool) (*Store, *gorm.DB) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	sqlDB, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	ddl := []string{
		`CREATE TABLE gp_ext_packages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, slug TEXT NOT NULL,
			version TEXT NOT NULL, digest TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL, signed BOOLEAN NOT NULL DEFAULT 0, publisher_kid TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL, is_current BOOLEAN NOT NULL DEFAULT 0, installed_by INTEGER,
			installed_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE UNIQUE INDEX gp_ext_packages_uniq_version ON gp_ext_packages (kind, slug, version, digest)`,
		`CREATE UNIQUE INDEX gp_ext_packages_uniq_current ON gp_ext_packages (kind, slug) WHERE is_current`,
		`CREATE TABLE gp_ext_site (
			id INTEGER PRIMARY KEY, generation INTEGER NOT NULL DEFAULT 0,
			catalog_sequence INTEGER NOT NULL DEFAULT 0, catalog_body TEXT NOT NULL DEFAULT '',
			catalog_fetched_at DATETIME, updated_at DATETIME NOT NULL)`,
	}
	if withSiteRow {
		ddl = append(ddl, `INSERT INTO gp_ext_site (id, updated_at) VALUES (1, CURRENT_TIMESTAMP)`)
	}
	for _, statement := range ddl {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("extension store tests require CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	return NewStore(db), db
}

func theme(slug, version, digest string) *Package {
	return &Package{Kind: KindTheme, Slug: slug, Version: version, Digest: digest, Size: 1024, Source: SourceMarket, Signed: true}
}

func mustGeneration(t *testing.T, s *Store) int64 {
	t.Helper()
	gen, err := s.Generation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return gen
}

func TestStoreTracksVersionsCurrentAndGeneration(t *testing.T) {
	s, _ := sqliteStore(t, true)
	ctx := context.Background()
	if gen := mustGeneration(t, s); gen != 0 {
		t.Fatalf("initial generation = %d", gen)
	}

	v1 := theme("aurora", "1.0.0", "h1:aaa=")
	if err := s.Add(ctx, v1, true); err != nil {
		t.Fatal(err)
	}
	if v1.ID == 0 || v1.Status != StatusReady || v1.InstalledAt.IsZero() || !v1.IsCurrent {
		t.Fatalf("Add did not fill in the package: %+v", v1)
	}
	time.Sleep(10 * time.Millisecond) // distinct install times for ordering
	v2 := theme("aurora", "1.1.0", "h1:bbb=")
	if err := s.Add(ctx, v2, false); err != nil {
		t.Fatal(err)
	}
	other := theme("borealis", "2.0.0", "h1:ccc=")
	if err := s.Add(ctx, other, true); err != nil {
		t.Fatal(err)
	}
	if gen := mustGeneration(t, s); gen != 3 {
		t.Fatalf("generation after 3 adds = %d", gen)
	}

	current, err := s.Current(ctx, KindTheme, "aurora")
	if err != nil || current == nil || current.ID != v1.ID {
		t.Fatalf("Current = %+v, %v; want v1", current, err)
	}
	if err := s.SetCurrent(ctx, v2.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Current(ctx, KindTheme, "aurora")
	if current == nil || current.ID != v2.ID {
		t.Fatalf("after SetCurrent, Current = %+v", current)
	}
	if old, _ := s.Get(ctx, v1.ID); old.IsCurrent {
		t.Fatal("previous current version is still current")
	}
	if c, _ := s.Current(ctx, KindTheme, "borealis"); c == nil || c.ID != other.ID {
		t.Fatal("another package's current version changed")
	}
	if err := s.SetCurrent(ctx, v2.ID); err != nil { // already current: no-op, still a write
		t.Fatal(err)
	}

	versions, err := s.Versions(ctx, KindTheme, "aurora")
	if err != nil || len(versions) != 2 || versions[0].ID != v2.ID {
		t.Fatalf("Versions = %+v, %v; want newest first", versions, err)
	}
	all, _ := s.Packages(ctx, KindTheme)
	if len(all) != 3 || all[0].Slug != "aurora" || all[2].Slug != "borealis" {
		t.Fatalf("Packages = %+v", all)
	}
	if none, err := s.Current(ctx, KindTheme, "missing"); none != nil || err != nil {
		t.Fatalf("Current(missing) = %+v, %v", none, err)
	}
	if _, err := s.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) = %v", err)
	}
	if err := s.SetCurrent(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetCurrent(unknown) = %v", err)
	}
}

func TestStoreFailedWritesLeaveGenerationAlone(t *testing.T) {
	s, _ := sqliteStore(t, true)
	ctx := context.Background()
	v1 := theme("aurora", "1.0.0", "h1:aaa=")
	if err := s.Add(ctx, v1, true); err != nil {
		t.Fatal(err)
	}
	gen := mustGeneration(t, s)

	invalid := []*Package{
		{Kind: KindTheme, Version: "1", Digest: "h1:x=", Source: SourceMarket},
		{Kind: KindTheme, Slug: "a", Digest: "h1:x=", Source: SourceMarket},
		{Kind: KindTheme, Slug: "a", Version: "1", Digest: "sha256:x", Source: SourceMarket},
		{Kind: KindTheme, Slug: "a", Version: "1", Digest: "h1:x=", Source: "ftp"},
		{Kind: KindTheme, Slug: "a", Version: "1", Digest: "h1:x=", Source: SourceUpload, Status: "weird"},
		{Kind: KindTheme, Slug: "a", Version: "1", Digest: "h1:x=", Source: SourceUpload, Size: -1},
	}
	for _, pkg := range invalid {
		if err := s.Add(ctx, pkg, false); err == nil {
			t.Errorf("Add accepted %+v", pkg)
		}
	}
	// The same content twice violates the unique index; the transaction,
	// including its generation increment, rolls back.
	if err := s.Add(ctx, theme("aurora", "1.0.0", "h1:aaa="), false); err == nil {
		t.Fatal("duplicate content accepted")
	}
	if err := s.SetStatus(ctx, v1.ID, "weird"); err == nil {
		t.Fatal("unknown status accepted")
	}
	if err := s.SetStatus(ctx, 9999, StatusBroken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetStatus(unknown) = %v", err)
	}
	if got := mustGeneration(t, s); got != gen {
		t.Fatalf("failed writes changed the generation from %d to %d", gen, got)
	}
	// Same version with different content may coexist (D1 4.6).
	if err := s.Add(ctx, theme("aurora", "1.0.0", "h1:zzz="), false); err != nil {
		t.Fatalf("same version, different content rejected: %v", err)
	}
}

func TestStoreStatusAndDelete(t *testing.T) {
	s, _ := sqliteStore(t, true)
	ctx := context.Background()
	v1, v2 := theme("aurora", "1.0.0", "h1:aaa="), theme("aurora", "1.1.0", "h1:bbb=")
	for _, pkg := range []*Package{v1, v2} {
		if err := s.Add(ctx, pkg, pkg == v2); err != nil {
			t.Fatal(err)
		}
	}
	gen := mustGeneration(t, s)
	if err := s.SetStatus(ctx, v1.ID, StatusRevoked); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, v1.ID); got.Status != StatusRevoked {
		t.Fatalf("status = %q", got.Status)
	}
	if err := s.Delete(ctx, v1.ID, 9999); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, v1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted version still present")
	}
	if got := mustGeneration(t, s); got != gen+2 {
		t.Fatalf("generation = %d, want %d", got, gen+2)
	}
	if err := s.Delete(ctx); err != nil || mustGeneration(t, s) != gen+2 {
		t.Fatal("Delete with no IDs must do nothing")
	}
}

func TestStoreCatalogRejectsRollback(t *testing.T) {
	s, _ := sqliteStore(t, true)
	ctx := context.Background()
	state, err := s.Catalog(ctx)
	if err != nil || state.CatalogSequence != 0 || state.CatalogBody != "" || state.CatalogFetchedAt != nil {
		t.Fatalf("initial catalog = %+v, %v", state, err)
	}
	fetched := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	if err := s.SaveCatalog(ctx, 5, `{"sequence":5}`, fetched); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCatalog(ctx, 5, `{"sequence":5,"again":true}`, fetched); err != nil {
		t.Fatalf("same sequence rejected: %v", err)
	}
	if err := s.SaveCatalog(ctx, 4, `{"sequence":4}`, fetched); !errors.Is(err, ErrCatalogRollback) {
		t.Fatalf("older catalog accepted: %v", err)
	}
	if err := s.SaveCatalog(ctx, 0, `{}`, fetched); err == nil {
		t.Fatal("sequence 0 accepted")
	}
	state, _ = s.Catalog(ctx)
	if state.CatalogSequence != 5 || state.CatalogBody != `{"sequence":5,"again":true}` || state.CatalogFetchedAt == nil {
		t.Fatalf("stored catalog = %+v", state)
	}
	if gen := mustGeneration(t, s); gen != 0 {
		t.Fatalf("catalog changes must not bump the generation, got %d", gen)
	}
}

func TestStoreReplaceCatalogLetsTheCallerDecide(t *testing.T) {
	s, _ := sqliteStore(t, true)
	ctx := context.Background()
	fetched := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	if err := s.SaveCatalog(ctx, 9, "nine", fetched); err != nil {
		t.Fatal(err)
	}

	refused := errors.New("refused")
	err := s.ReplaceCatalog(ctx, fetched, func(stored SiteState) (int64, string, error) {
		if stored.CatalogSequence != 9 || stored.CatalogBody != "nine" {
			t.Errorf("decide saw %+v", stored)
		}
		return 0, "", refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	// decide may lower the sequence, e.g. after the site switched markets.
	if err := s.ReplaceCatalog(ctx, fetched, func(SiteState) (int64, string, error) { return 2, "two", nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceCatalog(ctx, fetched, func(SiteState) (int64, string, error) { return 0, "zero", nil }); err == nil {
		t.Fatal("sequence 0 accepted")
	}
	state, _ := s.Catalog(ctx)
	if state.CatalogSequence != 2 || state.CatalogBody != "two" {
		t.Fatalf("stored = %d %q", state.CatalogSequence, state.CatalogBody)
	}
	if gen := mustGeneration(t, s); gen != 0 {
		t.Fatalf("generation = %d", gen)
	}
}

func TestStoreReportsMissingMigration(t *testing.T) {
	s, _ := sqliteStore(t, false)
	ctx := context.Background()
	if _, err := s.Generation(ctx); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("Generation = %v", err)
	}
	if err := s.Add(ctx, theme("aurora", "1.0.0", "h1:aaa="), true); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("Add = %v", err)
	}
	if err := s.SaveCatalog(ctx, 1, "{}", time.Now()); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("SaveCatalog = %v", err)
	}
	if err := s.ReplaceCatalog(ctx, time.Now(), func(SiteState) (int64, string, error) {
		t.Error("decide called without an ext_site row")
		return 1, "{}", nil
	}); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("ReplaceCatalog = %v", err)
	}
	if packages, _ := s.Packages(ctx, KindTheme); len(packages) != 0 {
		t.Fatal("failed Add left a row behind")
	}
}
