package seoextras

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func seoSaveTestPlugin(t *testing.T) (*Plugin, *gorm.DB, *content.Repository) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	sqlDB, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE gp_content_meta (id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER NOT NULL, meta_key TEXT NOT NULL, meta_value TEXT)`).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("seo-extras save tests require CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	repository := content.NewRepository(db)
	engine := &core.Engine{DB: db, Hooks: hook.New(), Content: repository, PluginManager: plugin.NewManager()}
	p := New()
	engine.PluginManager.Register(p)
	engine.PluginManager.Activate(PluginName, engine)
	return p, db, repository
}

func seoSaveContext(form url.Values) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/posts/1/edit", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c
}

func TestSaveFieldsStoresValuesAndClearsEmptyOnes(t *testing.T) {
	p, _, repository := seoSaveTestPlugin(t)
	item := &content.Content{ID: 1, Type: "post"}
	if err := repository.SaveMeta(item.ID, metaKeyRobots, "noindex"); err != nil {
		t.Fatal(err)
	}

	c := seoSaveContext(url.Values{"_seo_title": {" Custom title "}, "_seo_description": {"Summary"}, "_seo_robots": {""}})
	p.saveFields(context.Background(), c, item)

	if len(c.Errors) != 0 {
		t.Fatalf("unexpected save errors: %v", c.Errors)
	}
	meta, err := repository.GetMeta(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if meta[metaKeyTitle] != "Custom title" || meta[metaKeyDescription] != "Summary" {
		t.Fatalf("meta = %v, want saved title and description", meta)
	}
	if _, ok := meta[metaKeyRobots]; ok {
		t.Fatalf("empty robots value should delete the override, meta = %v", meta)
	}
}

// The four fields are one save: a failure keeps every previous value and is
// reported through c.Error so the admin does not show a success notice.
func TestSaveFieldsIsAtomicAndReportsFailures(t *testing.T) {
	p, db, repository := seoSaveTestPlugin(t)
	item := &content.Content{ID: 1, Type: "post"}
	if err := repository.SaveMeta(item.ID, metaKeyTitle, "Old title"); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_seo_insert", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("insert failed"))
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:fail_seo_insert") })

	// The title update succeeds on its own, then inserting the new
	// description fails; the title change must be rolled back with it.
	c := seoSaveContext(url.Values{"_seo_title": {"New title"}, "_seo_description": {"New description"}})
	p.saveFields(context.Background(), c, item)

	if len(c.Errors) == 0 {
		t.Fatal("save failure was not reported through c.Error")
	}
	meta, err := repository.GetMeta(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if meta[metaKeyTitle] != "Old title" {
		t.Fatalf("title = %q, want the previous value after a failed save", meta[metaKeyTitle])
	}
	if _, ok := meta[metaKeyDescription]; ok {
		t.Fatalf("description was partially saved: %v", meta)
	}
}
