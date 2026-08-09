package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestForceSeedClearTablesPreservesUsers(t *testing.T) {
	prev := dbprefix.Get()
	dbprefix.Set("test_")
	defer dbprefix.Set(prev)

	forbidden := map[string]bool{
		dbprefix.Table("users"):     true,
		dbprefix.Table("user_meta"): true,
	}
	for _, table := range forceSeedClearTables() {
		if forbidden[table] {
			t.Fatalf("force seed must not clear user table %q", table)
		}
	}
}

func TestNormalizedSeedTaxonomiesSupportsGenericAndLegacyDeclarations(t *testing.T) {
	data := SeedData{
		Taxonomies: []SeedTaxonomy{
			{Taxonomy: "product_cat", Name: "Audio", Slug: "audio"},
			{Taxonomy: "product_tag", Name: "Hot", Slug: "hot"},
		},
		Categories: []SeedTaxonomy{{Name: "Audio", Slug: "audio"}},
		Tags:       []SeedTaxonomy{{Name: "Hot", Slug: "hot"}},
	}

	got, err := normalizedSeedTaxonomies(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []SeedTaxonomy{
		{Taxonomy: "product_cat", Name: "Audio", Slug: "audio"},
		{Taxonomy: "product_tag", Name: "Hot", Slug: "hot"},
		{Taxonomy: "category", Name: "Audio", Slug: "audio"},
		{Taxonomy: "tag", Name: "Hot", Slug: "hot"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized taxonomies = %#v, want %#v", got, want)
	}
}

func TestNormalizedSeedTaxonomiesRejectsConflictingGlobalTermNames(t *testing.T) {
	data := SeedData{Taxonomies: []SeedTaxonomy{
		{Taxonomy: "product_cat", Name: "Audio", Slug: "audio"},
		{Taxonomy: "category", Name: "Sound", Slug: "audio"},
	}}
	if _, err := normalizedSeedTaxonomies(data); err == nil {
		t.Fatal("expected conflicting term names to be rejected")
	}
}

func TestSeedContentTaxonomyRefsMergesGenericAndLegacyValues(t *testing.T) {
	item := SeedContent{
		Category: "news",
		Tags:     []string{"hot", "new", "hot"},
		Taxonomies: map[string][]string{
			"product_cat": {"audio", "audio"},
			"tag":         {"featured", "hot"},
		},
	}

	got := seedContentTaxonomyRefs(item)
	want := map[string][]string{
		"category":    {"news"},
		"product_cat": {"audio"},
		"tag":         {"featured", "hot", "new"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("content taxonomy refs = %#v, want %#v", got, want)
	}
}

func TestValidateSeedContentTaxonomiesRejectsUndefinedTerm(t *testing.T) {
	contents := []SeedContent{{
		Title:      "Product",
		Taxonomies: map[string][]string{"product_cat": {"missing"}},
	}}
	definitions := []SeedTaxonomy{{Taxonomy: "product_cat", Name: "Audio", Slug: "audio"}}
	if err := validateSeedContentTaxonomies(contents, definitions); err == nil {
		t.Fatal("expected an undefined taxonomy term to be rejected")
	}
}

func TestValidateSeedRegistryRequiresRegisteredCompatibleExtensions(t *testing.T) {
	prepared := &preparedSeedData{
		data: SeedData{Contents: []SeedContent{{
			Type:       "listing",
			Title:      "Example",
			Taxonomies: map[string][]string{"listing_region": {"north"}},
		}}},
		taxonomyDefs: []SeedTaxonomy{{Taxonomy: "listing_region", Name: "North", Slug: "north"}},
	}
	registry := content.NewRegistry()
	registry.RegisterTaxonomy(content.TaxonomyDef{
		Name: "listing_region", ContentTypes: []string{"listing"},
	})
	registry.RegisterType(content.ContentTypeDef{
		Name: "listing", Taxonomies: []string{"listing_region"},
	})
	if err := validateSeedRegistry(prepared, registry); err != nil {
		t.Fatalf("compatible registry rejected: %v", err)
	}

	registry.Clear()
	registry.RegisterType(content.ContentTypeDef{Name: "listing"})
	err := validateSeedRegistry(prepared, registry)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("missing extension taxonomy error = %v", err)
	}
}

func TestForceSeedPreflightFailsBeforeStorageAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-seed.toml")
	if err := os.WriteFile(path, []byte("[[contents]\ninvalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	// DB and Options deliberately remain nil. Reaching either would panic; a
	// normal parse error proves destructive storage work never started.
	err := (&Engine{}).ForceSeedFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "failed to parse seed file") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestForceSeedRollsBackWhenClearFailsMidTransaction(t *testing.T) {
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 user=test dbname=test sslmode=disable"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := &failingSeedPool{failAtExec: 2}
	db.Config.ConnPool = pool
	db.Statement.ConnPool = pool

	path := filepath.Join(t.TempDir(), "seed.toml")
	if err := os.WriteFile(path, []byte("[[settings]]\nkey = \"site_name\"\nvalue = \"Replacement\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post"})
	engine := &Engine{
		DB:       db,
		Options:  option.NewMemoryStore(map[string]string{"site_name": "Existing"}),
		Registry: registry,
	}

	err = engine.ForceSeedFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "forced seed exec failure") {
		t.Fatalf("ForceSeedFromFile error = %v", err)
	}
	if !pool.rolledBack || pool.committed {
		t.Fatalf("transaction state: rolledBack=%v committed=%v", pool.rolledBack, pool.committed)
	}
	if pool.execCount != 2 {
		t.Fatalf("clear exec count = %d, want 2", pool.execCount)
	}
}

type failingSeedPool struct {
	execCount  int
	failAtExec int
	rolledBack bool
	committed  bool
}

func (p *failingSeedPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	return &failingSeedTx{pool: p}, nil
}

func (p *failingSeedPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}

func (p *failingSeedPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("unexpected root exec")
}

func (p *failingSeedPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unexpected root query")
}

func (p *failingSeedPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

type failingSeedTx struct {
	pool *failingSeedPool
}

func (tx *failingSeedTx) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}

func (tx *failingSeedTx) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	tx.pool.execCount++
	if tx.pool.execCount == tx.pool.failAtExec {
		return nil, errors.New("forced seed exec failure")
	}
	return seedSQLResult{}, nil
}

func (tx *failingSeedTx) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (tx *failingSeedTx) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

func (tx *failingSeedTx) Commit() error {
	tx.pool.committed = true
	return nil
}

func (tx *failingSeedTx) Rollback() error {
	tx.pool.rolledBack = true
	return nil
}

type seedSQLResult struct{}

func (seedSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (seedSQLResult) RowsAffected() (int64, error) { return 1, nil }
