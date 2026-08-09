package audit

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

func auditTestService(t *testing.T) *Service {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE gp_audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, username TEXT,
		action TEXT NOT NULL, resource TEXT, resource_id INTEGER, details TEXT,
		ip_address TEXT, created_at DATETIME
	)`).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("audit tests require CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	return NewService(db)
}

func TestServiceRecordsAndListsNewestAuditEvents(t *testing.T) {
	service := auditTestService(t)
	older := &Event{UserID: 1, Username: "alice", Action: "create", Resource: "post", ResourceID: 7, CreatedAt: time.Now().Add(-time.Hour)}
	newer := &Event{UserID: 2, Username: "bob", Action: "update", Resource: "post", ResourceID: 7, CreatedAt: time.Now()}
	if err := service.Record(context.Background(), older); err != nil {
		t.Fatal(err)
	}
	if err := service.Record(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	events, err := service.ListRecent(context.Background(), 1)
	if err != nil || len(events) != 1 || events[0].Username != "bob" {
		t.Fatalf("recent events=%+v err=%v", events, err)
	}
	usernames, err := service.LatestUsernamesByResource(context.Background(), "create", "post", []uint{7})
	if err != nil || usernames[7] != "alice" {
		t.Fatalf("latest creator usernames=%v err=%v", usernames, err)
	}
}

func TestServiceRejectsUnavailableOrNilEvents(t *testing.T) {
	if err := (*Service)(nil).Record(context.Background(), &Event{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil service error=%v", err)
	}
	if err := auditTestService(t).Record(context.Background(), nil); err == nil {
		t.Fatal("nil audit event should be rejected")
	}
}
