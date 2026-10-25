package multilang

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const translationTestSecret = "translation-route-secret"

func multilangTranslationTestPlugin(t *testing.T) (*Plugin, *gorm.DB) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE gp_contents (id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL DEFAULT 'post', status TEXT NOT NULL DEFAULT 'draft', title TEXT NOT NULL, slug TEXT NOT NULL, content TEXT, excerpt TEXT, image_url TEXT, author_id INTEGER, parent_id INTEGER, sort_order INTEGER DEFAULT 0, comment_status TEXT DEFAULT 'open', published_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE gp_content_meta (id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER NOT NULL, meta_key TEXT NOT NULL, meta_value TEXT)`,
		`CREATE TABLE gp_plgn_multilang_languages (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL, name TEXT NOT NULL, flag TEXT, is_default BOOLEAN, sort_order INTEGER, active BOOLEAN)`,
		`CREATE TABLE gp_plgn_multilang_translations (id INTEGER PRIMARY KEY AUTOINCREMENT, trid INTEGER NOT NULL, content_id INTEGER NOT NULL UNIQUE, language_code TEXT NOT NULL, source_content_id INTEGER, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("multilang translation tests require CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post", Label: "Post", LabelPlural: "Posts", HasArchive: true})
	registry.RegisterType(content.ContentTypeDef{Name: "contact_message", Label: "Message", LabelPlural: "Messages", ReadOnly: true})
	engine := &core.Engine{
		DB: db, Registry: registry, RBAC: user.NewRBAC(),
		Content: content.NewRepository(db),
		Auth:    user.NewAuth(translationTestSecret, 1, nil),
	}
	p := &Plugin{engine: engine, repo: NewRepository(db), defaultTag: "en"}
	if err := db.Create(&Language{Code: "en", Name: "English", IsDefault: true, Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Language{Code: "zh", Name: "中文", Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	return p, db
}

func seedTranslationSource(t *testing.T, db *gorm.DB, item *content.Content) uint {
	t.Helper()
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&content.ContentMeta{ContentID: item.ID, MetaKey: "email", MetaValue: "private@example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	return item.ID
}

// postTranslation drives the route through the same RequirePermission
// middleware the plugin registers, so route-level and handler-level checks
// are both exercised.
func postTranslation(t *testing.T, p *Plugin, account *user.User, contentID uint, publish bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	token, err := p.engine.Auth.GenerateToken(account)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/admin/plugins/multi-language/translate",
		admin.RequirePermission(p.engine.Auth, p.engine.RBAC, "content", "create"),
		p.handleCreateTranslation,
	)
	form := url.Values{"content_id": {strconv.FormatUint(uint64(contentID), 10)}, "target_lang": {"zh"}}
	if publish {
		form.Set("publish", "1")
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/multi-language/translate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func countContents(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&content.Content{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func findClone(t *testing.T, db *gorm.DB, sourceID uint) content.Content {
	t.Helper()
	var link Translation
	if err := db.Where("source_content_id = ?", sourceID).First(&link).Error; err != nil {
		t.Fatalf("translation link missing: %v", err)
	}
	var clone content.Content
	if err := db.First(&clone, link.ContentID).Error; err != nil {
		t.Fatal(err)
	}
	return clone
}

func TestCreateTranslationRejectsContributorCloningOthersDraft(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusDraft, Title: "Admin draft", Slug: "admin-draft", AuthorID: 1})
	before := countContents(t, db)

	rec := postTranslation(t, p, &user.User{ID: 7, Username: "contrib", Role: user.RoleContributor}, sourceID, false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if after := countContents(t, db); after != before {
		t.Fatalf("content rows = %d, want %d (no clone)", after, before)
	}
}

func TestCreateTranslationRejectsPublishWithoutPublishPermission(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusDraft, Title: "Own draft", Slug: "own-draft", AuthorID: 7})
	before := countContents(t, db)

	rec := postTranslation(t, p, &user.User{ID: 7, Username: "author", Role: user.RoleAuthor}, sourceID, true)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if after := countContents(t, db); after != before {
		t.Fatalf("content rows = %d, want %d (no clone)", after, before)
	}
}

func TestCreateTranslationRejectsReadOnlyType(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "contact_message", Status: content.StatusPublished, Title: "Inbound", Slug: "inbound", AuthorID: 0})

	rec := postTranslation(t, p, &user.User{ID: 3, Username: "editor", Role: user.RoleEditor}, sourceID, false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCreateTranslationRejectsTrashedSource(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusTrash, Title: "Trashed", Slug: "trashed", AuthorID: 3})

	rec := postTranslation(t, p, &user.User{ID: 3, Username: "editor", Role: user.RoleEditor}, sourceID, false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCreateTranslationAllowsOwnDraftForUpdateOwnRole(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusDraft, Title: "Own draft", Slug: "own-draft", AuthorID: 7})

	rec := postTranslation(t, p, &user.User{ID: 7, Username: "contrib", Role: user.RoleContributor}, sourceID, false)

	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "success=") {
		t.Fatalf("status = %d location = %q, want success redirect", rec.Code, rec.Header().Get("Location"))
	}
	clone := findClone(t, db, sourceID)
	if clone.Status != content.StatusDraft || clone.AuthorID != 7 {
		t.Fatalf("clone status/author = %q/%d, want draft/7", clone.Status, clone.AuthorID)
	}
}

func TestCreateTranslationEditorPublishesCloneAsTranslator(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusPublished, Title: "Shared post", Slug: "shared-post", AuthorID: 1})

	rec := postTranslation(t, p, &user.User{ID: 3, Username: "editor", Role: user.RoleEditor}, sourceID, true)

	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "success=") {
		t.Fatalf("status = %d location = %q, want success redirect", rec.Code, rec.Header().Get("Location"))
	}
	clone := findClone(t, db, sourceID)
	if clone.Status != content.StatusPublished || clone.PublishedAt == nil || clone.AuthorID != 3 {
		t.Fatalf("clone status/published/author = %q/%v/%d, want published/set/3", clone.Status, clone.PublishedAt, clone.AuthorID)
	}
}

func TestCreateTranslationEscapesErrorRedirect(t *testing.T) {
	p, _ := multilangTranslationTestPlugin(t)

	rec := postTranslation(t, p, &user.User{ID: 3, Username: "editor", Role: user.RoleEditor}, 9999, false)

	location := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || strings.ContainsAny(location, " ") {
		t.Fatalf("status = %d location = %q, want escaped redirect", rec.Code, location)
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.Query().Get("error") != "内容不存在" {
		t.Fatalf("redirect error = %q (parse err %v)", parsed.Query().Get("error"), err)
	}
}
