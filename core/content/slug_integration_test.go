package content

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// integrationDB opens a real PostgreSQL when GOPRESS_TEST_PG_DSN is set (for
// example "host=localhost user=postgres dbname=testdb sslmode=disable") and
// migrates the content tables into a throwaway schema dropped afterwards.
func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	dbprefix.Set(dbprefix.DefaultPrefix)
	quiet := &gorm.Config{Logger: gormlogger.Discard}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schemaName := fmt.Sprintf("gopress_content_itest_%d", time.Now().UnixNano())
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
	if err := db.AutoMigrate(&Content{}, &ContentMeta{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestConcurrentCreatesReserveDistinctSlugs(t *testing.T) {
	db := integrationDB(t)
	registry := NewRegistry()
	registry.RegisterType(ContentTypeDef{Name: "post", Rewrite: RewriteRule{Slug: "posts"}})
	commands := NewCommandService(db, registry)
	repository := NewRepository(db)

	const writers = 24
	warmConnections(t, db, writers)
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			item := &Content{Type: "post", Status: StatusDraft, Title: "Race", Slug: "race"}
			// Mix both entry points that reserve slugs in the write transaction.
			if i%2 == 0 {
				errs <- commands.Create(context.Background(), item, nil, WithUniqueSlug())
			} else {
				errs <- repository.CreateWithUniqueSlug(context.Background(), item)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}

	var duplicates []struct {
		Slug  string
		Count int
	}
	if err := db.Raw(`SELECT slug, count(*) AS count FROM ` + Content{}.TableName() + ` GROUP BY slug HAVING count(*) > 1`).Scan(&duplicates).Error; err != nil {
		t.Fatal(err)
	}
	if len(duplicates) > 0 {
		t.Fatalf("concurrent writers produced duplicate slugs: %+v", duplicates)
	}
	var total int64
	if err := db.Model(&Content{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != writers {
		t.Fatalf("rows = %d, want %d", total, writers)
	}
}

func TestConcurrentFirstMetaSavesKeepOneRow(t *testing.T) {
	db := integrationDB(t)
	repository := NewRepository(db)
	item := &Content{Type: "post", Status: StatusDraft, Title: "Meta", Slug: "meta"}
	if err := repository.Create(item); err != nil {
		t.Fatal(err)
	}

	const writers = 24
	warmConnections(t, db, writers)
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- repository.SaveMeta(item.ID, "_seo_title", fmt.Sprintf("writer-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent SaveMeta: %v", err)
		}
	}

	var rows int64
	if err := db.Model(&ContentMeta{}).Where("content_id = ? AND meta_key = ?", item.ID, "_seo_title").Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("concurrent first saves left %d rows, want 1", rows)
	}
}

// warmConnections opens n pooled connections up front. Otherwise most writers
// first wait to dial a connection, which staggers them enough that a race
// window as short as one meta upsert is rarely hit.
func warmConnections(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxIdleConns(n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = db.Exec("SELECT pg_sleep(0.05)").Error
		}()
	}
	wg.Wait()
}

// Plugins compose repository writes inside their own transaction (a
// translation clone plus its meta, or several SEO fields). On PostgreSQL those
// nested writes become savepoints and must still roll back with the outer
// transaction, advisory locks included.
func TestNestedRepositoryWritesRollBackWithOuterTransaction(t *testing.T) {
	db := integrationDB(t)
	failure := errors.New("later step failed")

	err := db.Transaction(func(tx *gorm.DB) error {
		contents := NewRepository(tx)
		item := &Content{Type: "post", Status: StatusDraft, Title: "Nested", Slug: "nested"}
		if err := contents.CreateWithUniqueSlug(context.Background(), item); err != nil {
			return err
		}
		if err := contents.SaveMeta(item.ID, "_copied", "value"); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("outer transaction error = %v, want the injected failure", err)
	}
	var contents, meta int64
	if err := db.Model(&Content{}).Count(&contents).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ContentMeta{}).Count(&meta).Error; err != nil {
		t.Fatal(err)
	}
	if contents != 0 || meta != 0 {
		t.Fatalf("rows after rollback: contents=%d meta=%d, want 0 and 0", contents, meta)
	}

	// The same nested writes commit normally once the outer transaction does.
	if err := db.Transaction(func(tx *gorm.DB) error {
		contents := NewRepository(tx)
		item := &Content{Type: "post", Status: StatusDraft, Title: "Nested", Slug: "nested"}
		if err := contents.CreateWithUniqueSlug(context.Background(), item); err != nil {
			return err
		}
		return contents.SaveMeta(item.ID, "_copied", "value")
	}); err != nil {
		t.Fatalf("committed nested writes: %v", err)
	}
	if err := db.Model(&ContentMeta{}).Where("meta_key = ?", "_copied").Count(&meta).Error; err != nil {
		t.Fatal(err)
	}
	if meta != 1 {
		t.Fatalf("committed meta rows = %d, want 1", meta)
	}
}
