package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func adminUserTestRepository(t *testing.T) *user.Repository {
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
	statement := `CREATE TABLE gp_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		email TEXT UNIQUE,
		password_hash TEXT,
		display_name TEXT,
		avatar_url TEXT,
		role TEXT NOT NULL DEFAULT 'subscriber',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		last_login_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`
	if err := db.Exec(statement).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("admin user tests require CGO-backed SQLite")
		}
		t.Fatalf("create users schema: %v", err)
	}
	if err := db.Exec(`INSERT INTO gp_users
		(id, username, email, display_name, role, is_active)
		VALUES (1, 'target', 'target@example.com', 'Target', 'subscriber', TRUE)`).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user.NewRepository(db)
}

func userUpdateTestRouter(repository *user.Repository, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := &Handler{svc: &Service{userRepo: repository, rbac: user.NewRBAC()}}
	router := gin.New()
	router.POST("/admin/users/:id/edit", func(c *gin.Context) {
		c.Set("admin_user_id", uint(99))
		c.Set("admin_username", "operator")
		c.Set("admin_role", role)
		handler.UserUpdate(c)
	})
	return router
}

func submitUserUpdate(t *testing.T, router http.Handler, active bool, displayName string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"email":        {"target@example.com"},
		"display_name": {displayName},
		"role":         {user.RoleSubscriber},
	}
	if active {
		form.Set("is_active", "1")
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestUserUpdatePersistsDisabledAndReenabledState(t *testing.T) {
	repository := adminUserTestRepository(t)
	router := userUpdateTestRouter(repository, user.RoleSuperAdmin)

	if rec := submitUserUpdate(t, router, false, "Disabled Target"); rec.Code != http.StatusFound {
		t.Fatalf("disable status = %d, want %d", rec.Code, http.StatusFound)
	}
	account, err := repository.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if account.IsActive {
		t.Fatal("unchecked is_active was not persisted as false")
	}

	if rec := submitUserUpdate(t, router, true, "Reenabled Target"); rec.Code != http.StatusFound {
		t.Fatalf("reenable status = %d, want %d", rec.Code, http.StatusFound)
	}
	account, err = repository.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if !account.IsActive {
		t.Fatal("checked is_active was not persisted as true")
	}
}

func TestUserUpdateRBACRejectsSubscriberAndAllowsSuperAdmin(t *testing.T) {
	repository := adminUserTestRepository(t)

	denied := submitUserUpdate(t, userUpdateTestRouter(repository, user.RoleSubscriber), false, "Denied Change")
	if denied.Code != http.StatusFound {
		t.Fatalf("subscriber status = %d, want %d", denied.Code, http.StatusFound)
	}
	account, err := repository.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if !account.IsActive || account.DisplayName != "Target" {
		t.Fatalf("subscriber changed protected user: active=%v display_name=%q", account.IsActive, account.DisplayName)
	}
	if location := denied.Header().Get("Location"); !strings.HasPrefix(location, "/admin/?error=") {
		t.Fatalf("subscriber redirect = %q, want permission error", location)
	}

	allowed := submitUserUpdate(t, userUpdateTestRouter(repository, user.RoleSuperAdmin), false, "Allowed Change")
	if allowed.Code != http.StatusFound {
		t.Fatalf("super admin status = %d, want %d", allowed.Code, http.StatusFound)
	}
	account, err = repository.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if account.IsActive || account.DisplayName != "Allowed Change" {
		t.Fatalf("super admin update not applied: active=%v display_name=%q", account.IsActive, account.DisplayName)
	}
	if location := allowed.Header().Get("Location"); !strings.HasPrefix(location, "/admin/users?success=") {
		t.Fatalf("super admin redirect = %q, want success", location)
	}
}
