// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package dbschema

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Three sites in one schema: each type maps the same fixed index names onto a
// different prefixed table, exactly like GoPress models under table prefixes.
type siteAItem struct {
	ID       uint
	Code     string `gorm:"size:40;uniqueIndex:idx_itest_item_key,priority:1"`
	Kind     string `gorm:"size:40;uniqueIndex:idx_itest_item_key,priority:2"`
	RemoteID string `gorm:"size:40;uniqueIndex:idx_itest_item_remote,where:remote_id <> ''"`
	Day      string `gorm:"size:10;index:idx_itest_item_day"`
}

func (siteAItem) TableName() string { return "a_items" }

type siteBItem siteAItem

func (siteBItem) TableName() string { return "b_items" }

type siteCItem siteAItem

func (siteCItem) TableName() string { return "c_items" }

// TestEnsureSiteIndexesRepairsSharedSchema runs against a real PostgreSQL when
// GOPRESS_TEST_PG_DSN is set (for example
// "host=localhost user=postgres dbname=testdb sslmode=disable"). It works in a
// throwaway schema that it drops afterwards.
func TestEnsureSiteIndexesRepairsSharedSchema(t *testing.T) {
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	quiet := &gorm.Config{Logger: gormlogger.Discard}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schemaName := fmt.Sprintf("gopress_dbschema_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), quiet)
	if err != nil {
		t.Fatalf("connect to test schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// Reproduce the bug: plain AutoMigrate gives the fixed names to site A only.
	if err := db.AutoMigrate(&siteAItem{}, &siteBItem{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if indexCount(t, db, "a_items") != 4 {
		t.Fatalf("site A indexes = %d, want 4 (pkey + 3 declared)", indexCount(t, db, "a_items"))
	}
	if got := indexCount(t, db, "b_items"); got != 1 {
		t.Fatalf("site B indexes before repair = %d, want only the primary key", got)
	}

	if err := EnsureSiteIndexes(db, &siteAItem{}, &siteBItem{}); err != nil {
		t.Fatalf("EnsureSiteIndexes: %v", err)
	}
	if got := indexCount(t, db, "a_items"); got != 4 {
		t.Fatalf("site A indexes after repair = %d, want unchanged 4", got)
	}
	if got := indexCount(t, db, "b_items"); got != 4 {
		t.Fatalf("site B indexes after repair = %d, want 4", got)
	}

	// Site B now enforces its own uniqueness, including the partial index.
	mustCreate(t, db, &siteBItem{Code: "x", Kind: "k", RemoteID: "r1"})
	if err := db.Create(&siteBItem{Code: "x", Kind: "k"}).Error; err == nil {
		t.Fatal("duplicate (code, kind) accepted on site B after repair")
	}
	if err := db.Create(&siteBItem{Code: "y", Kind: "k", RemoteID: "r1"}).Error; err == nil {
		t.Fatal("duplicate non-empty remote_id accepted on site B after repair")
	}
	mustCreate(t, db, &siteBItem{Code: "z1", Kind: "k"})
	mustCreate(t, db, &siteBItem{Code: "z2", Kind: "k"}) // empty remote_id stays outside the partial index

	// Idempotent, and the previous binary's plain AutoMigrate still succeeds.
	if err := EnsureSiteIndexes(db, &siteAItem{}, &siteBItem{}); err != nil {
		t.Fatalf("second EnsureSiteIndexes: %v", err)
	}
	if got := indexCount(t, db, "b_items"); got != 4 {
		t.Fatalf("site B indexes after second repair = %d, want 4", got)
	}
	if err := db.AutoMigrate(&siteAItem{}, &siteBItem{}); err != nil {
		t.Fatalf("plain AutoMigrate after repair (rollback path): %v", err)
	}

	// Existing duplicates block a unique index: report it, create nothing
	// partial, and keep AutoMigrate non-fatal so the site still boots.
	if err := db.AutoMigrate(&siteCItem{}); err != nil {
		t.Fatalf("AutoMigrate site C: %v", err)
	}
	mustCreate(t, db, &siteCItem{Code: "dup", Kind: "k"})
	mustCreate(t, db, &siteCItem{Code: "dup", Kind: "k"})
	err = EnsureSiteIndexes(db, &siteCItem{})
	if err == nil || !strings.Contains(err.Error(), "c_items") {
		t.Fatalf("EnsureSiteIndexes with duplicates = %v, want an error naming c_items", err)
	}
	var keyIndex *string
	if err := db.Raw("SELECT to_regclass(?)::text", SiteIndexName("c_items", "idx_itest_item_key")).Scan(&keyIndex).Error; err != nil {
		t.Fatal(err)
	}
	if keyIndex != nil {
		t.Fatalf("unique index %q was created despite duplicate rows", *keyIndex)
	}
	if got := indexCount(t, db, "c_items"); got != 3 {
		t.Fatalf("site C indexes = %d, want pkey + remote + day (key blocked by duplicates)", got)
	}
	if err := AutoMigrate(db, &siteCItem{}); err != nil {
		t.Fatalf("AutoMigrate must not fail the boot on index repair problems: %v", err)
	}
}

func indexCount(t *testing.T, db *gorm.DB, table string) int {
	t.Helper()
	var count int
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ?`, table).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func mustCreate(t *testing.T, db *gorm.DB, value interface{}) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatalf("create %T: %v", value, err)
	}
}
