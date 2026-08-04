package content

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-press/core/user"
	"go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func publicSubmissionTestRuntime(t *testing.T) (*PublicSubmissionService, *Repository, *user.Repository) {
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
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE gp_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE,
			email TEXT UNIQUE, password_hash TEXT, display_name TEXT, avatar_url TEXT,
			role TEXT NOT NULL, is_active BOOLEAN NOT NULL DEFAULT TRUE,
			last_login_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
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
		`INSERT INTO gp_users (id, username, role, is_active) VALUES
			(1, 'alice', 'subscriber', TRUE),
			(2, 'bob', 'subscriber', TRUE),
			(3, 'disabled', 'subscriber', FALSE),
			(4, 'limited', 'limited', TRUE)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("public submission tests require CGO-backed SQLite")
			}
			t.Fatalf("prepare public submission schema: %v", err)
		}
	}

	registry := NewRegistry()
	registry.RegisterType(ContentTypeDef{
		Name: "question",
		PublicSubmission: PublicSubmissionPolicy{
			Enabled: true, Roles: []string{user.RoleSubscriber}, DefaultStatus: StatusPending,
			AllowUpdateOwn: true, AllowDeleteOwn: true,
		},
	})
	rbac := user.NewRBAC()
	for _, action := range []string{"create", "update_own", "delete_own"} {
		rbac.GrantCapability(user.RoleSubscriber, "question", action)
	}
	repository := NewRepository(db)
	users := user.NewRepository(db)
	return NewPublicSubmissionService(repository, users, registry, rbac), repository, users
}

func publicSubmissionContext(account *user.User) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
	if account != nil {
		c.Set(user.CtxKeyPublicUser, account)
	}
	return c
}

func TestPublicSubmissionCreateBindsAuthenticatedOwnerAndPendingStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, repository, users := publicSubmissionTestRuntime(t)
	account, err := users.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.CreateOwn(publicSubmissionContext(account), PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "How do hooks work?",
		Content: `<p>Safe body</p><script>alert("x")</script>`, Meta: map[string]string{"source": "markdown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.AuthorID != account.ID || item.Status != StatusPending || item.Slug != "how-do-hooks-work" {
		t.Fatalf("created item = author:%d status:%q slug:%q", item.AuthorID, item.Status, item.Slug)
	}
	saved, err := repository.FindByID(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.Content, "<script") || saved.GetMeta("source") != "markdown" {
		t.Fatalf("content/meta were not safely persisted: content=%q meta=%q", saved.Content, saved.GetMeta("source"))
	}
}

func TestPublicSubmissionTrustedPolicyCanPublishCreateAndUpdate(t *testing.T) {
	service, repository, users := publicSubmissionTestRuntime(t)
	account, _ := users.FindByID(1)
	item, err := service.CreateOwn(publicSubmissionContext(account), PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "Published question", Content: "Public body",
		PublishImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusPublished || item.PublishedAt == nil {
		t.Fatalf("published create = status:%q published_at:%v", item.Status, item.PublishedAt)
	}
	updated, err := service.UpdateOwn(publicSubmissionContext(account), item.ID, PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "Still published", Content: "Updated public body",
		PublishImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusPublished || updated.PublishedAt == nil {
		t.Fatalf("published update = status:%q published_at:%v", updated.Status, updated.PublishedAt)
	}
	saved, err := repository.FindByID(item.ID)
	if err != nil || saved.Status != StatusPublished || saved.PublishedAt == nil {
		t.Fatalf("saved published item = %+v, err=%v", saved, err)
	}
}

func TestPublicSubmissionSlugUniquenessIgnoresRequestContentScopes(t *testing.T) {
	service, _, users := publicSubmissionTestRuntime(t)
	account, _ := users.FindByID(1)
	first, err := service.CreateOwn(publicSubmissionContext(account), PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "Shared question", Content: "First body",
	})
	if err != nil {
		t.Fatal(err)
	}
	scoped := publicSubmissionContext(account)
	AddContentScope(scoped, func(db *gorm.DB) *gorm.DB { return db.Where("1 = 0") })
	second, err := service.CreateOwn(scoped, PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "Shared question", Content: "Second body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Slug != "shared-question" || second.Slug != "shared-question-1" {
		t.Fatalf("global public slugs = %q, %q", first.Slug, second.Slug)
	}
}

func TestPublicSubmissionRejectsIdentitySpoofingInactiveUsersAndDisallowedRoles(t *testing.T) {
	service, _, users := publicSubmissionTestRuntime(t)
	alice, _ := users.FindByID(1)
	bob, _ := users.FindByID(2)
	disabled, _ := users.FindByID(3)
	limited, _ := users.FindByID(4)
	input := PublicSubmissionInput{ContentType: "question", UserID: alice.ID, Title: "Question", Content: "Enough body"}

	if _, err := service.CreateOwn(publicSubmissionContext(bob), input); !errors.Is(err, ErrPublicSubmissionForbidden) {
		t.Fatalf("identity spoofing error = %v, want forbidden", err)
	}
	input.UserID = disabled.ID
	if _, err := service.CreateOwn(publicSubmissionContext(disabled), input); !errors.Is(err, ErrPublicSubmissionForbidden) {
		t.Fatalf("disabled account error = %v, want forbidden", err)
	}
	input.UserID = limited.ID
	if _, err := service.CreateOwn(publicSubmissionContext(limited), input); !errors.Is(err, ErrPublicSubmissionForbidden) {
		t.Fatalf("disallowed role error = %v, want forbidden", err)
	}
}

func TestPublicSubmissionUpdateAndDeleteEnforceOwnership(t *testing.T) {
	service, repository, users := publicSubmissionTestRuntime(t)
	alice, _ := users.FindByID(1)
	bob, _ := users.FindByID(2)
	created, err := service.CreateOwn(publicSubmissionContext(alice), PublicSubmissionInput{
		ContentType: "question", UserID: alice.ID, Title: "Original title", Content: "Original body",
	})
	if err != nil {
		t.Fatal(err)
	}

	foreignInput := PublicSubmissionInput{ContentType: "question", UserID: bob.ID, Title: "Hijacked", Content: "Hijacked body"}
	if _, err := service.UpdateOwn(publicSubmissionContext(bob), created.ID, foreignInput); !errors.Is(err, ErrPublicSubmissionNotFound) {
		t.Fatalf("foreign update error = %v, want not found", err)
	}
	if err := service.TrashOwn(publicSubmissionContext(bob), "question", bob.ID, created.ID); !errors.Is(err, ErrPublicSubmissionNotFound) {
		t.Fatalf("foreign delete error = %v, want not found", err)
	}

	updated, err := service.UpdateOwn(publicSubmissionContext(alice), created.ID, PublicSubmissionInput{
		ContentType: "question", UserID: alice.ID, Title: "Updated title", Content: "Updated body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated title" || updated.Status != StatusPending {
		t.Fatalf("owner update = title:%q status:%q", updated.Title, updated.Status)
	}
	if err := service.TrashOwn(publicSubmissionContext(alice), "question", alice.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := repository.FindByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != StatusTrash {
		t.Fatalf("deleted status = %q, want trash", saved.Status)
	}
}

func TestPublicSubmissionRateLimitCannotBeBypassedWithRapidRequests(t *testing.T) {
	service, _, users := publicSubmissionTestRuntime(t)
	account, _ := users.FindByID(1)
	for index := 0; index < publicSubmissionPerMinute; index++ {
		_, err := service.CreateOwn(publicSubmissionContext(account), PublicSubmissionInput{
			ContentType: "question", UserID: account.ID, Title: "Question", Content: "Rate limited body",
		})
		if err != nil {
			t.Fatalf("submission %d: %v", index+1, err)
		}
	}
	_, err := service.CreateOwn(publicSubmissionContext(account), PublicSubmissionInput{
		ContentType: "question", UserID: account.ID, Title: "One too many", Content: "Rate limited body",
	})
	if !errors.Is(err, ErrPublicSubmissionRateLimited) {
		t.Fatalf("rate limit error = %v, want rate limited", err)
	}
}
