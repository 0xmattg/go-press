package taxonomy

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/pkg/dbprefix"

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
		`CREATE TABLE gp_terms (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL)`,
		`CREATE TABLE gp_taxonomies (id INTEGER PRIMARY KEY AUTOINCREMENT, term_id INTEGER NOT NULL DEFAULT 0, taxonomy TEXT NOT NULL, description TEXT, parent_id INTEGER, count INTEGER DEFAULT 0)`,
		`CREATE TABLE gp_contents (id INTEGER PRIMARY KEY, status TEXT NOT NULL, deleted_at DATETIME)`,
		`CREATE TABLE gp_term_relationships (content_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, PRIMARY KEY (content_id, taxonomy_id))`,
		`CREATE TABLE gp_test_taxonomy_languages (taxonomy_id INTEGER PRIMARY KEY, language_code TEXT NOT NULL)`,
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

func taxonomyLanguageContext(lang string) context.Context {
	return WithScope(context.Background(), Scope{
		Key: lang,
		Apply: func(db *gorm.DB) *gorm.DB {
			return db.Where("gp_taxonomies.id IN (SELECT taxonomy_id FROM gp_test_taxonomy_languages WHERE language_code = ?)", lang)
		},
	})
}

func taxonomyCommandRuntime(t *testing.T) (*gorm.DB, *CommandService) {
	t.Helper()
	db := taxonomyTestDB(t)
	registry := content.NewRegistry()
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "category", Hierarchical: true})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "tag"})
	commands := NewCommandService(db, registry)
	commands.SetMutationObserver(func(ctx context.Context, mutation Mutation) {
		if mutation.Kind == MutationCreated && mutation.Item != nil && ScopeKey(ctx) != "" {
			if err := db.Exec(`INSERT INTO gp_test_taxonomy_languages (taxonomy_id, language_code) VALUES (?, ?)`, mutation.Item.ID, ScopeKey(ctx)).Error; err != nil {
				t.Fatalf("record taxonomy language: %v", err)
			}
		}
	})
	return db, commands
}

func TestCommandServiceAllowsSameSlugAcrossScopesButNotInsideOneScope(t *testing.T) {
	_, commands := taxonomyCommandRuntime(t)
	en := &Taxonomy{Taxonomy: "category", Term: Term{Name: "News", Slug: "news"}}
	zh := &Taxonomy{Taxonomy: "category", Term: Term{Name: "新闻", Slug: "news"}}
	if err := commands.Create(taxonomyLanguageContext("en"), en); err != nil {
		t.Fatal(err)
	}
	if err := commands.Create(taxonomyLanguageContext("zh"), zh); err != nil {
		t.Fatalf("cross-language slug was rejected: %v", err)
	}
	duplicate := &Taxonomy{Taxonomy: "tag", Term: Term{Name: "重复", Slug: "news"}}
	if err := commands.Create(taxonomyLanguageContext("zh"), duplicate); !errors.Is(err, ErrTaxonomySlugConflict) {
		t.Fatalf("same-scope duplicate error = %v", err)
	}
}

func TestCommandServiceRejectsOutOfScopeRelationshipID(t *testing.T) {
	db, commands := taxonomyCommandRuntime(t)
	en := &Taxonomy{Taxonomy: "category", Term: Term{Name: "News", Slug: "news"}}
	zh := &Taxonomy{Taxonomy: "category", Term: Term{Name: "新闻", Slug: "xinwen"}}
	if err := commands.Create(taxonomyLanguageContext("en"), en); err != nil {
		t.Fatal(err)
	}
	if err := commands.Create(taxonomyLanguageContext("zh"), zh); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO gp_contents (id, status) VALUES (10, 'draft')`).Error; err != nil {
		t.Fatal(err)
	}
	err := commands.SetContentTaxonomies(taxonomyLanguageContext("zh"), 10, []string{"category"}, []uint{en.ID})
	if !errors.Is(err, ErrInvalidTaxonomySelection) {
		t.Fatalf("out-of-scope relationship error = %v", err)
	}
	var count int64
	if err := db.Model(&TermRelationship{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected relationship persisted: count=%d err=%v", count, err)
	}
}

func TestCommandServiceKeepsHierarchyInsideLanguageScope(t *testing.T) {
	_, commands := taxonomyCommandRuntime(t)
	enParent := &Taxonomy{Taxonomy: "category", Term: Term{Name: "Parent", Slug: "parent"}}
	zhParent := &Taxonomy{Taxonomy: "category", Term: Term{Name: "父级", Slug: "fuji"}}
	if err := commands.Create(taxonomyLanguageContext("en"), enParent); err != nil {
		t.Fatal(err)
	}
	if err := commands.Create(taxonomyLanguageContext("zh"), zhParent); err != nil {
		t.Fatal(err)
	}
	child := &Taxonomy{
		Taxonomy: "category", ParentID: &zhParent.ID, Description: "中文描述",
		Term: Term{Name: "子级", Slug: "ziji"},
	}
	if err := commands.Create(taxonomyLanguageContext("zh"), child); err != nil {
		t.Fatal(err)
	}
	child.ParentID = &enParent.ID
	if err := commands.Update(taxonomyLanguageContext("zh"), "category", child); !errors.Is(err, ErrInvalidTaxonomyParent) {
		t.Fatalf("cross-language parent error = %v", err)
	}
	zhParent.ParentID = &child.ID
	if err := commands.Update(taxonomyLanguageContext("zh"), "category", zhParent); !errors.Is(err, ErrInvalidTaxonomyParent) {
		t.Fatalf("hierarchy cycle error = %v", err)
	}
	tag := &Taxonomy{Taxonomy: "tag", ParentID: &zhParent.ID, Term: Term{Name: "标签", Slug: "biaoqian"}}
	if err := commands.Create(taxonomyLanguageContext("zh"), tag); !errors.Is(err, ErrInvalidTaxonomyParent) {
		t.Fatalf("non-hierarchical parent error = %v", err)
	}
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
