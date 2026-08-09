package content

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
)

func contentCommandTestRuntime(t *testing.T) (*gorm.DB, *CommandService, *Repository, *Registry) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
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
	statements := []string{
		`CREATE TABLE gp_contents (
			id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL, status TEXT NOT NULL,
			title TEXT NOT NULL, slug TEXT NOT NULL, content TEXT, excerpt TEXT, image_url TEXT,
			author_id INTEGER, parent_id INTEGER, sort_order INTEGER DEFAULT 0,
			comment_status TEXT DEFAULT 'open', published_at DATETIME,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE gp_content_meta (
			id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER NOT NULL,
			meta_key TEXT NOT NULL, meta_value TEXT
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("content command tests require CGO-backed SQLite")
			}
			t.Fatalf("prepare command schema: %v", err)
		}
	}

	registry := NewRegistry()
	registry.RegisterType(ContentTypeDef{
		Name: "post", Supports: []string{"thumbnail", "sort_order"},
		MetaFields: []MetaFieldDef{{Key: "subtitle"}}, Rewrite: RewriteRule{Slug: "posts"},
	})
	registry.RegisterType(ContentTypeDef{Name: "service", Rewrite: RewriteRule{Slug: "services"}})
	registry.RegisterType(ContentTypeDef{Name: "page", Rewrite: RewriteRule{Rootless: true}})
	registry.RegisterTaxonomy(TaxonomyDef{Name: "category"})

	return db, NewCommandService(db, registry), NewRepository(db), registry
}

func TestCommandServiceCreateWorksWithoutGinAndPersistsDeclaredMeta(t *testing.T) {
	_, commands, repository, _ := contentCommandTestRuntime(t)
	item := &Content{
		Type: "post", Status: StatusPublished, Title: "Phase Zero", Slug: "phase-zero",
		Content: `<p>safe</p><script>alert("x")</script>`,
	}
	err := commands.Create(context.Background(), item, map[string]string{
		"subtitle": "Domain command", "gallery_images": `["/one.jpg"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID == 0 || item.PublishedAt == nil {
		t.Fatalf("created item missing ID or publish time: %+v", item)
	}
	saved, err := repository.FindByID(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.Content, "<script") {
		t.Fatalf("content was not sanitized: %q", saved.Content)
	}
	if saved.GetMeta("subtitle") != "Domain command" || saved.GetMeta("gallery_images") != `["/one.jpg"]` {
		t.Fatalf("declared meta was not persisted: %#v", saved.Meta)
	}
}

func TestCommandServiceRejectsReservedSlugAndUndeclaredMeta(t *testing.T) {
	db, commands, _, _ := contentCommandTestRuntime(t)
	err := commands.Create(context.Background(), &Content{
		Type: "page", Status: StatusDraft, Title: "Admin", Slug: "admin",
	}, nil)
	if !errors.Is(err, ErrReservedSlug) {
		t.Fatalf("reserved slug error = %v", err)
	}
	err = commands.Create(context.Background(), &Content{
		Type: "post", Status: StatusDraft, Title: "Post", Slug: "post",
	}, map[string]string{"plugin_private_key": "forged"})
	if !errors.Is(err, ErrUnsupportedMeta) {
		t.Fatalf("undeclared meta error = %v", err)
	}
	var count int64
	if err := db.Model(&Content{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected commands must not persist rows: count=%d err=%v", count, err)
	}
}

func TestCommandServiceHidesCrossTypeIDsOnUpdateAndDelete(t *testing.T) {
	db, commands, _, _ := contentCommandTestRuntime(t)
	service := &Content{Type: "service", Status: StatusDraft, Title: "Original", Slug: "original"}
	if err := db.Create(service).Error; err != nil {
		t.Fatal(err)
	}
	forged := *service
	forged.Type = "post"
	forged.Title = "Forged"
	if err := commands.Update(context.Background(), "post", &forged, nil); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("cross-type update error = %v, want not found", err)
	}
	if err := commands.Delete(context.Background(), "post", service.ID); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("cross-type delete error = %v, want not found", err)
	}
	var saved Content
	if err := db.First(&saved, service.ID).Error; err != nil || saved.Title != "Original" {
		t.Fatalf("cross-type mutation changed row: %+v err=%v", saved, err)
	}
}

func TestCommandServiceBulkOperationsStayInsideContentType(t *testing.T) {
	db, commands, _, _ := contentCommandTestRuntime(t)
	fixedNow := time.Date(2026, 8, 9, 2, 3, 4, 0, time.UTC)
	commands.now = func() time.Time { return fixedNow }
	oldTime := fixedNow.Add(-24 * time.Hour)
	postOne := Content{Type: "post", Status: StatusDraft, Title: "One", Slug: "one", PublishedAt: &oldTime}
	postTwo := Content{Type: "post", Status: StatusDraft, Title: "Two", Slug: "two"}
	service := Content{Type: "service", Status: StatusDraft, Title: "Service", Slug: "service"}
	for _, item := range []*Content{&postOne, &postTwo, &service} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	count, err := commands.Publish(context.Background(), "post", []uint{postOne.ID, service.ID, postTwo.ID, postTwo.ID})
	if err != nil || count != 2 {
		t.Fatalf("publish count=%d err=%v", count, err)
	}
	if err := commands.Reorder(context.Background(), "post", []uint{postTwo.ID, service.ID, postOne.ID}, 0); err != nil {
		t.Fatal(err)
	}
	var gotOne, gotTwo, gotService Content
	_ = db.First(&gotOne, postOne.ID).Error
	_ = db.First(&gotTwo, postTwo.ID).Error
	_ = db.First(&gotService, service.ID).Error
	if gotOne.PublishedAt == nil || !gotOne.PublishedAt.Equal(oldTime) {
		t.Fatalf("existing publish time changed: %v", gotOne.PublishedAt)
	}
	if gotTwo.PublishedAt == nil || !gotTwo.PublishedAt.Equal(fixedNow) {
		t.Fatalf("missing publish time was not filled: %v", gotTwo.PublishedAt)
	}
	if gotOne.SortOrder != 2 || gotTwo.SortOrder != 1 || gotService.SortOrder != 0 {
		t.Fatalf("cross-type reorder result: post1=%d post2=%d service=%d", gotOne.SortOrder, gotTwo.SortOrder, gotService.SortOrder)
	}
}

func TestCommandServiceHardDeleteUsesGenericRelatedCleanup(t *testing.T) {
	db, commands, repository, _ := contentCommandTestRuntime(t)
	post := Content{Type: "post", Status: StatusDraft, Title: "Post", Slug: "post"}
	service := Content{Type: "service", Status: StatusDraft, Title: "Service", Slug: "service"}
	if err := db.Create(&post).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveMeta(post.ID, "subtitle", "delete me"); err != nil {
		t.Fatal(err)
	}
	var cleaned []uint
	count, err := commands.HardDelete(context.Background(), "post", []uint{post.ID, service.ID}, func(_ *gorm.DB, ids []uint) error {
		cleaned = append(cleaned, ids...)
		return nil
	})
	if err != nil || count != 1 || len(cleaned) != 1 || cleaned[0] != post.ID {
		t.Fatalf("hard delete count=%d cleaned=%v err=%v", count, cleaned, err)
	}
	var postCount, serviceCount, metaCount int64
	_ = db.Unscoped().Model(&Content{}).Where("id = ?", post.ID).Count(&postCount).Error
	_ = db.Model(&Content{}).Where("id = ?", service.ID).Count(&serviceCount).Error
	_ = db.Model(&ContentMeta{}).Where("content_id = ?", post.ID).Count(&metaCount).Error
	if postCount != 0 || serviceCount != 1 || metaCount != 0 {
		t.Fatalf("hard delete leaked across type: post=%d service=%d meta=%d", postCount, serviceCount, metaCount)
	}
}
