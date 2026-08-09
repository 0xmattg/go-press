package core

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type recordingColumnAlterer struct {
	fields []string
	errAt  string
}

func TestBackfillMediaUpdatedAtUsesCreatedAtForLegacyRows(t *testing.T) {
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
	if err := db.Exec(`CREATE TABLE gp_media (id INTEGER PRIMARY KEY, created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("migration test requires CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 9, 3, 4, 5, 0, time.UTC)
	if err := db.Exec(`INSERT INTO gp_media (id, created_at, updated_at) VALUES (?, ?, NULL)`, 1, created).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillMediaUpdatedAt(db); err != nil {
		t.Fatal(err)
	}
	var updated time.Time
	if err := db.Raw(`SELECT updated_at FROM gp_media WHERE id = 1`).Scan(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if !updated.Equal(created) {
		t.Fatalf("updated_at=%s want=%s", updated, created)
	}
}

func (m *recordingColumnAlterer) AlterColumn(value interface{}, field string) error {
	if _, ok := value.(*user.User); !ok {
		return errors.New("unexpected migration model")
	}
	m.fields = append(m.fields, field)
	if field == m.errAt {
		return errors.New("alter failed")
	}
	return nil
}

func TestCoreModelsIncludeExternalIdentityAndPublicSession(t *testing.T) {
	models := coreModels()
	want := map[reflect.Type]bool{
		reflect.TypeOf(&user.UserIdentity{}):       false,
		reflect.TypeOf(&user.UserSession{}):        false,
		reflect.TypeOf(&agent.ServiceAccount{}):    false,
		reflect.TypeOf(&agent.Credential{}):        false,
		reflect.TypeOf(&agent.IdempotencyRecord{}): false,
		reflect.TypeOf(&agent.AuditEvent{}):        false,
	}
	for _, model := range models {
		if _, ok := want[reflect.TypeOf(model)]; ok {
			want[reflect.TypeOf(model)] = true
		}
	}
	for modelType, found := range want {
		if !found {
			t.Fatalf("core migration missing %v", modelType)
		}
	}
}

func TestLegacyUserMigrationRelaxesExternalAccountColumns(t *testing.T) {
	migrator := &recordingColumnAlterer{}
	if err := migrateLegacyUserColumns(migrator); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(migrator.fields, []string{"Email", "PasswordHash"}) {
		t.Fatalf("altered columns = %#v", migrator.fields)
	}

	migrator = &recordingColumnAlterer{errAt: "Email"}
	if err := migrateLegacyUserColumns(migrator); err == nil {
		t.Fatal("column migration error was ignored")
	}
}
