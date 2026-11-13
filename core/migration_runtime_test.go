// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/migrate"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type migratingTestPlugin struct {
	plan        migrate.Plan
	activations int
}

func (p *migratingTestPlugin) Name() string             { return "migration-test" }
func (p *migratingTestPlugin) Version() string          { return "1.0.0" }
func (p *migratingTestPlugin) Description() string      { return "" }
func (p *migratingTestPlugin) Activate(plugin.App)      { p.activations++ }
func (p *migratingTestPlugin) Deactivate(plugin.App)    {}
func (p *migratingTestPlugin) Migrations() migrate.Plan { return p.plan }
func (p *migratingTestPlugin) step(version int64, name string, phase migrate.Phase, ran *[]string, err error) {
	p.plan.Steps = append(p.plan.Steps, migrate.Step{Version: version, Name: name, Phase: phase,
		Up: func(context.Context, *gorm.DB) error {
			*ran = append(*ran, name)
			return err
		}})
}

// migrationTestEngine builds a minimal engine on SQLite (behind the postgres
// dialector) whose plugin manager applies plugin migrations like New does.
func migrationTestEngine(t *testing.T, p plugin.Plugin) (*Engine, *gorm.DB) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	sqlDB, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE ` + (option.Option{}).TableName() + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, value TEXT, autoload BOOLEAN DEFAULT TRUE)`).Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("plugin migration tests require CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	manager := plugin.NewManager()
	manager.Register(p)
	e := &Engine{DB: db, PluginManager: manager, Options: option.NewStore(db)}
	manager.SetBeforeActivate(e.migratePlugin)
	return e, db
}

func pluginRecords(t *testing.T, db *gorm.DB) map[int64]migrate.Record {
	t.Helper()
	rows, err := MigrationRunner(db).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]migrate.Record{}
	for _, row := range rows {
		if row.Module == "plugin:migration-test" {
			out[row.Version] = row
		}
	}
	return out
}

func TestPluginMigrationsRunBeforeActivateAndCatchUp(t *testing.T) {
	p := &migratingTestPlugin{}
	var ran []string
	p.step(1, "create_tables", migrate.Blocking, &ran, nil)
	e, db := migrationTestEngine(t, p)

	e.LoadPlugin(p)
	if !e.PluginManager.IsActive(p.Name()) || p.activations != 1 || strings.Join(ran, ",") != "create_tables" {
		t.Fatalf("active=%v activations=%d ran=%v", e.PluginManager.IsActive(p.Name()), p.activations, ran)
	}
	if rec := pluginRecords(t, db)[1]; rec.Status != migrate.StatusApplied {
		t.Fatalf("plugin step record = %+v", rec)
	}

	if err := e.deactivatePluginFromAdmin(p.Name()); err != nil {
		t.Fatal(err)
	}
	// A newer build of the plugin adds a step while it is disabled.
	p.step(2, "add_column", migrate.Blocking, &ran, nil)
	if err := e.activatePluginFromAdmin(p.Name()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "create_tables,add_column" {
		t.Fatalf("ran = %v, want the new step applied once on re-enable", ran)
	}
	if records := pluginRecords(t, db); len(records) != 2 || records[2].Status != migrate.StatusApplied {
		t.Fatalf("records = %+v", records)
	}
}

func TestPluginMigrationFailureKeepsPluginInactive(t *testing.T) {
	p := &migratingTestPlugin{}
	var ran []string
	p.step(1, "broken", migrate.Blocking, &ran, errors.New("column clash"))
	e, db := migrationTestEngine(t, p)

	e.LoadPlugin(p)
	if e.PluginManager.IsActive(p.Name()) || p.activations != 0 {
		t.Fatal("a plugin whose migration failed at startup must stay inactive")
	}

	err := e.activatePluginFromAdmin(p.Name())
	if err == nil || !strings.Contains(err.Error(), "启用失败") || !strings.Contains(err.Error(), "column clash") {
		t.Fatalf("activatePluginFromAdmin error = %v, want the migration failure", err)
	}
	if e.PluginManager.IsActive(p.Name()) || p.activations != 0 {
		t.Fatal("a plugin whose migration failed must not be activated")
	}
	if got := e.Options.Get("plugin_active_" + p.Name()); got == "true" {
		t.Fatal("a failed activation must not be persisted")
	}
	if rec := pluginRecords(t, db)[1]; rec.Status != migrate.StatusFailed || !strings.Contains(rec.Error, "column clash") {
		t.Fatalf("failure record = %+v", rec)
	}
	if err := e.PluginManager.TryActivate("missing", e); !errors.Is(err, plugin.ErrNotRegistered) {
		t.Fatalf("TryActivate(missing) = %v, want ErrNotRegistered", err)
	}
}

func TestPluginOnlineMigrationsRunAfterReady(t *testing.T) {
	p := &migratingTestPlugin{}
	var ran []string
	p.step(1, "tables", migrate.Blocking, &ran, nil)
	p.step(2, "slow_index", migrate.Online, &ran, nil)
	e, _ := migrationTestEngine(t, p)

	// Startup: blocking steps run during activation, online ones only once
	// the engine is ready.
	e.LoadPlugin(p)
	if strings.Join(ran, ",") != "tables" {
		t.Fatalf("before ready ran = %v", ran)
	}
	e.MarkReady()
	e.StartOnlineMigrations()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.WaitOnlineMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "tables,slow_index" {
		t.Fatalf("after ready ran = %v", ran)
	}

	// Enabling in the admin after startup starts new online steps itself.
	if err := e.deactivatePluginFromAdmin(p.Name()); err != nil {
		t.Fatal(err)
	}
	p.step(3, "another_index", migrate.Online, &ran, nil)
	if err := e.activatePluginFromAdmin(p.Name()); err != nil {
		t.Fatal(err)
	}
	if err := e.WaitOnlineMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "tables,slow_index,another_index" {
		t.Fatalf("after admin enable ran = %v", ran)
	}
}

// TestPGCoreMigrationsAcrossInstallUpgradeAndRollback runs core's real
// migrations against PostgreSQL when GOPRESS_TEST_PG_DSN is set. Three sites
// share one throwaway schema, with prefixes that overlap as strings.
func TestPGCoreMigrationsAcrossInstallUpgradeAndRollback(t *testing.T) {
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schemaName := fmt.Sprintf("gopress_core_migrate_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	var pools []*gorm.DB
	t.Cleanup(func() {
		dbprefix.Set(dbprefix.DefaultPrefix)
		for _, db := range pools {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		_ = admin.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// openSite mirrors pkg/postgresql.NewConnection: the global prefix is set
	// before the pool exists, and each site gets its own pool.
	openSite := func(prefix string) *gorm.DB {
		dbprefix.Set(prefix)
		db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), &gorm.Config{
			Logger:         gormlogger.Discard,
			NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
		})
		if err != nil {
			t.Fatal(err)
		}
		pools = append(pools, db)
		return db
	}
	coreStep := func(db *gorm.DB, version int64) migrate.Record {
		t.Helper()
		rows, err := MigrationRunner(db).Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Module == CoreMigrationModule && row.Version == version {
				return row
			}
		}
		t.Fatalf("no core step %d record for %s, got %+v", version, dbprefix.Get(), rows)
		return migrate.Record{}
	}
	baseline := func(db *gorm.DB) migrate.Record {
		t.Helper()
		return coreStep(db, 1)
	}
	// Step 2 creates the extension tables on every site, new or upgraded,
	// exactly once.
	extensionTables := func(db *gorm.DB) {
		t.Helper()
		if rec := coreStep(db, 2); rec.Status != migrate.StatusApplied || rec.Runs != 1 || rec.Name != "create_extension_tables" {
			t.Fatalf("%s core step 2 = %+v, want applied once", dbprefix.Get(), rec)
		}
		for _, table := range []string{"ext_packages", "ext_site"} {
			if ok, err := migrate.TableExists(context.Background(), db, dbprefix.Table(table)); err != nil || !ok {
				t.Fatalf("%s: table %s missing (%v)", dbprefix.Get(), dbprefix.Table(table), err)
			}
		}
		if gen, err := extension.NewStore(db).Generation(context.Background()); err != nil || gen != 0 {
			t.Fatalf("%s: generation = %d, %v", dbprefix.Get(), gen, err)
		}
	}

	// New install of "gp_x_"; the installer and the first start both migrate.
	site := openSite("gp_x_")
	for i := 0; i < 2; i++ {
		if err := MigrateDB(site); err != nil {
			t.Fatalf("gp_x_ pass %d: %v", i, err)
		}
	}
	if rec := baseline(site); rec.Status != migrate.StatusApplied || rec.Runs != 1 {
		t.Fatalf("gp_x_ baseline = %+v, want applied once", rec)
	}
	extensionTables(site)

	// New install of "gp_": gp_x_contents exists and must not be taken for
	// this site's tables.
	site = openSite("gp_")
	if err := MigrateDB(site); err != nil {
		t.Fatal(err)
	}
	if rec := baseline(site); rec.Status != migrate.StatusApplied {
		t.Fatalf("gp_ baseline = %+v, want applied (not adopted from gp_x_)", rec)
	}
	extensionTables(site)

	// "gp_old_" was created by a release before versioned migrations, which
	// ran exactly prepareCoreSchema on every start.
	site = openSite("gp_old_")
	if err := prepareCoreSchema(site); err != nil {
		t.Fatal(err)
	}
	if err := site.Create(&content.Content{Title: "kept", Slug: "kept", Type: "post", Status: "publish"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateDB(site); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if rec := baseline(site); rec.Status != migrate.StatusAdopted {
		t.Fatalf("upgraded baseline = %+v, want adopted", rec)
	}
	// Roll back: the previous binary starts on the migrated database.
	if err := prepareCoreSchema(site); err != nil {
		t.Fatalf("previous release on the migrated database: %v", err)
	}
	// And upgrade again.
	if err := MigrateDB(site); err != nil {
		t.Fatalf("upgrade after rollback: %v", err)
	}
	if rec := baseline(site); rec.Status != migrate.StatusAdopted || rec.Runs != 1 {
		t.Fatalf("baseline after rollback = %+v, want unchanged", rec)
	}
	extensionTables(site)
	var count int64
	if err := site.Model(&content.Content{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("content rows = %d, %v; want the existing row kept", count, err)
	}
	for _, prefix := range []string{"gp_", "gp_x_", "gp_old_"} {
		if held := advisoryLockCount(t, admin, migrate.LockKey(schemaName, prefix)); held != 0 {
			t.Fatalf("migration lock of %s still held", prefix)
		}
	}
}

func advisoryLockCount(t *testing.T, db *gorm.DB, key int64) int {
	t.Helper()
	var n int
	if err := db.Raw(`SELECT count(*) FROM pg_catalog.pg_locks
 WHERE locktype = 'advisory' AND granted AND classid::bigint = ? AND objid::bigint = ? AND objsubid = 1`,
		int64(uint64(key)>>32), int64(uint64(key)&0xffffffff)).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
