package taxonomy

import (
	"database/sql"
	"strings"
	"testing"

	"go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func taxonomyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_foreign_keys=1"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE gp_taxonomies (id INTEGER PRIMARY KEY, taxonomy TEXT NOT NULL)`,
		`CREATE TABLE gp_contents (id INTEGER PRIMARY KEY, status TEXT NOT NULL, deleted_at DATETIME)`,
		`CREATE TABLE gp_term_relationships (content_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, PRIMARY KEY (content_id, taxonomy_id))`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("taxonomy repository tests require CGO-backed SQLite")
			}
			t.Fatalf("create test schema: %v", err)
		}
	}
	return db
}

func TestContentReferenceCountsUsesCurrentActiveRelationships(t *testing.T) {
	db := taxonomyTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO gp_taxonomies (id, taxonomy) VALUES (1, 'category'), (2, 'category'), (3, 'category'), (4, 'tag')`,
		`INSERT INTO gp_contents (id, status, deleted_at) VALUES
			(10, 'published', NULL),
			(11, 'draft', NULL),
			(12, 'archived', NULL),
			(13, 'trash', NULL),
			(14, 'published', '2026-08-01 00:00:00')`,
		`INSERT INTO gp_term_relationships (content_id, taxonomy_id) VALUES
			(10, 1), (11, 1), (12, 2), (13, 1), (14, 1), (10, 4)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed taxonomy references: %v", err)
		}
	}

	counts, err := NewRepository(db).ContentReferenceCounts("category")
	if err != nil {
		t.Fatal(err)
	}
	if got := counts[1]; got != 2 {
		t.Fatalf("category 1 count = %d, want published + draft = 2", got)
	}
	if got := counts[2]; got != 1 {
		t.Fatalf("category 2 count = %d, want archived reference = 1", got)
	}
	if got := counts[3]; got != 0 {
		t.Fatalf("unreferenced category count = %d, want 0", got)
	}
	if _, ok := counts[4]; ok {
		t.Fatal("tag count leaked into category aggregate")
	}
}
