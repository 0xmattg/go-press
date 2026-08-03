package option

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSetManyRollsBackDatabaseAndCacheOnFailure(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
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
	statement := `CREATE TABLE ` + (Option{}).TableName() + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		value TEXT CHECK(value <> 'reject'),
		autoload BOOLEAN DEFAULT TRUE
	)`
	if err := db.Exec(statement).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("atomic option test requires CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.SetMany(map[string]string{"first": "accepted", "second": "reject"}); err == nil {
		t.Fatal("SetMany accepted a transaction containing a rejected row")
	}
	var count int64
	if err := db.Model(&Option{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("persisted rows = %d, err=%v; want 0 after rollback", count, err)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.data) != 0 {
		t.Fatalf("cache after rollback = %#v; want empty", store.data)
	}
}

func TestSetManyUpdatesMemoryStore(t *testing.T) {
	store := NewMemoryStore(map[string]string{"old": "value"})
	if err := store.SetMany(map[string]string{"first": "1", "second": "2"}); err != nil {
		t.Fatal(err)
	}
	if store.Get("old") != "value" || store.Get("first") != "1" || store.Get("second") != "2" {
		t.Fatalf("memory options = %#v", store.All())
	}
}
