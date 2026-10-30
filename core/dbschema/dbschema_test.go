package dbschema

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestSiteIndexNameIsTableScopedAndBounded(t *testing.T) {
	t.Parallel()
	if got := SiteIndexName("gp_bitcuz_user_identities", "idx_user_identity_key"); got != "gp_bitcuz_user_identities_idx_user_identity_key" {
		t.Fatalf("SiteIndexName() = %q", got)
	}

	table := "gp_gopress_plgn_gopress_analytics_daily_dimension_visitors"
	long := SiteIndexName(table, "idx_gpa_daily_dimension_visitor")
	if len(long) != maxIdentifierLen {
		t.Fatalf("long name length = %d, want %d", len(long), maxIdentifierLen)
	}
	if long != SiteIndexName(table, "idx_gpa_daily_dimension_visitor") {
		t.Fatal("SiteIndexName() is not deterministic")
	}
	other := SiteIndexName(strings.Replace(table, "gp_gopress_", "gp_gopresx_", 1), "idx_gpa_daily_dimension_visitor")
	if other == long {
		t.Fatalf("different tables produced the same truncated name %q", long)
	}
}

type declaredModel struct {
	ID       uint
	Provider string `gorm:"uniqueIndex:idx_declared_key,priority:1"`
	Subject  string `gorm:"uniqueIndex:idx_declared_key,priority:2"`
	Remote   string `gorm:"uniqueIndex:idx_declared_remote,where:remote <> ''"`
	Day      string `gorm:"index:idx_declared_day"`
}

func parsedIndexes(t *testing.T) map[string]schema.Index {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=unused"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&declaredModel{}); err != nil {
		t.Fatal(err)
	}
	return stmt.Schema.ParseIndexes()
}

func TestDeclaredIndexKeepsColumnOrderUniquenessAndPredicate(t *testing.T) {
	t.Parallel()
	indexes := parsedIndexes(t)

	key, ok := declaredIndex(indexes["idx_declared_key"])
	if !ok || !key.unique || strings.Join(key.columns, ",") != "provider,subject" || key.where != "" {
		t.Fatalf("idx_declared_key = %+v, %v", key, ok)
	}
	remote, ok := declaredIndex(indexes["idx_declared_remote"])
	if !ok || !remote.unique || remote.where != "remote <> ''" {
		t.Fatalf("idx_declared_remote = %+v, %v", remote, ok)
	}
	day, ok := declaredIndex(indexes["idx_declared_day"])
	if !ok || day.unique || strings.Join(day.columns, ",") != "day" {
		t.Fatalf("idx_declared_day = %+v, %v", day, ok)
	}
}

func TestHasEquivalentRequiresSameCoverage(t *testing.T) {
	t.Parallel()
	unique := wantedIndex{columns: []string{"provider", "subject"}, unique: true}
	partial := wantedIndex{columns: []string{"remote"}, unique: true, where: "remote <> ''"}
	plain := wantedIndex{columns: []string{"day"}}

	tests := []struct {
		name     string
		existing []tableIndex
		want     wantedIndex
		ok       bool
	}{
		{"same unique index", []tableIndex{{columns: []string{"provider", "subject"}, unique: true}}, unique, true},
		{"non-unique cannot enforce uniqueness", []tableIndex{{columns: []string{"provider", "subject"}}}, unique, false},
		{"different column order", []tableIndex{{columns: []string{"subject", "provider"}, unique: true}}, unique, false},
		{"partial cannot replace a full index", []tableIndex{{columns: []string{"provider", "subject"}, unique: true, partial: true}}, unique, false},
		{"partial matches partial", []tableIndex{{columns: []string{"remote"}, unique: true, partial: true}}, partial, true},
		{"unique serves a plain lookup", []tableIndex{{columns: []string{"day"}, unique: true}}, plain, true},
		{"nothing on the table", nil, plain, false},
	}
	for _, tt := range tests {
		if got := hasEquivalent(tt.existing, tt.want); got != tt.ok {
			t.Errorf("%s: hasEquivalent() = %v, want %v", tt.name, got, tt.ok)
		}
	}
}

// Unit tests elsewhere run the postgres dialector over SQLite; index repair must
// stay out of their way instead of failing on the missing PostgreSQL catalog.
func TestEnsureSiteIndexesSkipsDatabasesWithoutPostgresCatalog(t *testing.T) {
	t.Parallel()
	sqlDB, err := sql.Open("sqlite3", "file:dbschema_skip?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureSiteIndexes(db, &declaredModel{}); err != nil {
		t.Fatalf("EnsureSiteIndexes() on SQLite = %v, want nil", err)
	}
	if err := EnsureSiteIndexes(nil, &declaredModel{}); err != nil {
		t.Fatalf("EnsureSiteIndexes(nil) = %v, want nil", err)
	}
}
