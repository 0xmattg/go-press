package user

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func publicSessionTestStores(t *testing.T) (*gorm.DB, *Repository, *SessionRepository) {
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
		`CREATE TABLE gp_users (
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
		)`,
		`CREATE TABLE gp_user_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			identity_id INTEGER,
			token_hash TEXT NOT NULL UNIQUE,
			ip_address TEXT,
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL,
			revoked_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`INSERT INTO gp_users
			(id, username, email, display_name, role, is_active)
			VALUES (1, 'public-user', 'public@example.com', 'Public User', 'subscriber', TRUE)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("public session tests require CGO-backed SQLite")
			}
			t.Fatalf("prepare session test schema: %v", err)
		}
	}
	return db, NewRepository(db), NewSessionRepository(db)
}

func TestDisabledAccountInvalidatesExistingPublicSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, users, sessions := publicSessionTestStores(t)
	manager := NewSessionManager(sessions, users, time.Hour)
	token, err := manager.Create(context.Background(), 1, SessionMetadata{UserAgent: "session-test"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	account, err := users.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	account.IsActive = false
	if err := users.Update(account); err != nil {
		t.Fatal(err)
	}

	if _, _, err := manager.Authenticate(context.Background(), token.Token); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("Authenticate() error = %v, want %v", err, ErrAccountDisabled)
	}
	var stored UserSession
	if err := db.Where("token_hash = ?", hashSessionToken(token.Token)).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RevokedAt == nil {
		t.Fatal("disabled account session was rejected but not revoked")
	}

	publicAuth := NewPublicAuth(nil, manager, nil, nil, false, nil)
	router := gin.New()
	router.Use(publicAuth.Middleware())
	router.GET("/", func(c *gin.Context) {
		if IsLoggedIn(c) {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: PublicSessionCookie, Value: token.Token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == PublicSessionCookie && cookie.MaxAge < 0 && cookie.Value == "" {
			return
		}
	}
	t.Fatal("disabled public session response did not clear the session cookie")
}
