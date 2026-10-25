package core

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type lifecycleTestPlugin struct {
	activations, deactivations int
}

func (p *lifecycleTestPlugin) Name() string          { return "lifecycle-test" }
func (p *lifecycleTestPlugin) Version() string       { return "1.0.0" }
func (p *lifecycleTestPlugin) Description() string   { return "" }
func (p *lifecycleTestPlugin) Activate(plugin.App)   { p.activations++ }
func (p *lifecycleTestPlugin) Deactivate(plugin.App) { p.deactivations++ }

// lifecycleOptionStore returns an option store whose table rejects rejected
// values through a CHECK constraint, so persistence failures can be injected.
func lifecycleOptionStore(t *testing.T, rejected string) *option.Store {
	t.Helper()
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
	statement := `CREATE TABLE ` + (option.Option{}).TableName() + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		value TEXT CHECK(value <> '` + rejected + `'),
		autoload BOOLEAN DEFAULT TRUE
	)`
	if err := db.Exec(statement).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("lifecycle persistence test requires CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	return option.NewStore(db)
}

func lifecycleTestEngine(store *option.Store) (*Engine, *lifecycleTestPlugin) {
	p := &lifecycleTestPlugin{}
	manager := plugin.NewManager()
	manager.Register(p)
	return &Engine{PluginManager: manager, Options: store}, p
}

func TestAdminPluginActivationRollsBackWhenStateCannotBePersisted(t *testing.T) {
	engine, p := lifecycleTestEngine(lifecycleOptionStore(t, "true"))

	if err := engine.activatePluginFromAdmin(p.Name()); err == nil {
		t.Fatal("activatePluginFromAdmin() succeeded although the option write failed")
	}
	if engine.PluginManager.IsActive(p.Name()) {
		t.Fatal("plugin stayed active after a failed persistence write")
	}
	if p.activations != 1 || p.deactivations != 1 {
		t.Fatalf("activations/deactivations = %d/%d, want 1/1", p.activations, p.deactivations)
	}
}

func TestAdminPluginDeactivationRestoresPluginWhenStateCannotBePersisted(t *testing.T) {
	engine, p := lifecycleTestEngine(lifecycleOptionStore(t, "false"))
	if err := engine.activatePluginFromAdmin(p.Name()); err != nil {
		t.Fatalf("activatePluginFromAdmin() error = %v", err)
	}

	if err := engine.deactivatePluginFromAdmin(p.Name()); err == nil {
		t.Fatal("deactivatePluginFromAdmin() succeeded although the option write failed")
	}
	if !engine.PluginManager.IsActive(p.Name()) {
		t.Fatal("plugin is inactive although its disabled state was not persisted")
	}
	if got := engine.Options.Get("plugin_active_" + p.Name()); got != "true" {
		t.Fatalf("persisted state = %q, want true", got)
	}
}

func TestAdminPluginToggleSucceedsAndPersists(t *testing.T) {
	engine, p := lifecycleTestEngine(option.NewMemoryStore(nil))

	if err := engine.activatePluginFromAdmin(p.Name()); err != nil {
		t.Fatalf("activatePluginFromAdmin() error = %v", err)
	}
	if err := engine.activatePluginFromAdmin(p.Name()); err == nil {
		t.Fatal("second activation did not report the plugin as already active")
	}
	if err := engine.deactivatePluginFromAdmin(p.Name()); err != nil {
		t.Fatalf("deactivatePluginFromAdmin() error = %v", err)
	}
	if got := engine.Options.Get("plugin_active_" + p.Name()); got != "false" {
		t.Fatalf("persisted state = %q, want false", got)
	}
}
