package multilang

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// LEGACY(multilang-slug): tests for legacy_slug.go; remove together with it.

func legacySlugTestDB(t *testing.T, prefixes ...string) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	t.Cleanup(func() { dbprefix.Set(dbprefix.DefaultPrefix) })
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
	for _, prefix := range prefixes {
		if err := db.Exec(`CREATE TABLE ` + prefix + `options (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, value TEXT, autoload BOOLEAN DEFAULT true)`).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("legacy slug tests require CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	return db
}

func insertOption(t *testing.T, db *gorm.DB, table, name, value string, autoload bool) {
	t.Helper()
	if err := db.Exec(`INSERT INTO `+table+` (name, value, autoload) VALUES (?, ?, ?)`, name, value, autoload).Error; err != nil {
		t.Fatal(err)
	}
}

func optionRows(t *testing.T, db *gorm.DB, table string) map[string]option.Option {
	t.Helper()
	var rows []option.Option
	if err := db.Table(table).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make(map[string]option.Option, len(rows))
	for _, row := range rows {
		out[row.Name] = row
	}
	return out
}

// T16 ① ② ⑦: a site that disabled the plugin keeps it disabled, every
// setting keeps its value and autoload flag, and the cache serves only the
// new keys.
func TestLegacySlugMigrationRunsBeforeLoadPlugin(t *testing.T) {
	db := legacySlugTestDB(t, "gp_")
	insertOption(t, db, "gp_options", "plugin_active_multi-language", "false", true)
	insertOption(t, db, "gp_options", "plugin_multi-language_default_lang", "zh", true)
	insertOption(t, db, "gp_options", "plugin_multi-language_auto_detect", "false", false)
	insertOption(t, db, "gp_options", "plugin_multi-language_taxonomy_category_mode", "translated", true)
	store := option.NewStore(db)
	store.LoadAll()
	engine := &core.Engine{DB: db, Options: store, PluginManager: plugin.NewManager()}

	load(engine)

	if _, ok := engine.PluginManager.FindBySlug(PluginName); !ok {
		t.Fatal("plugin was not registered")
	}
	if engine.PluginManager.IsActive(PluginName) {
		t.Fatal("a plugin disabled under the legacy slug was activated")
	}
	rows := optionRows(t, db, "gp_options")
	want := map[string]struct {
		value    string
		autoload bool
	}{
		"plugin_active_multilang":                 {"false", true},
		"plugin_multilang_default_lang":           {"zh", true},
		"plugin_multilang_auto_detect":            {"false", false},
		"plugin_multilang_taxonomy_category_mode": {"translated", true},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for name, w := range want {
		row, ok := rows[name]
		if !ok || row.Value != w.value || row.Autoload != w.autoload {
			t.Errorf("%s = %+v, want value %q autoload %v", name, row, w.value, w.autoload)
		}
	}
	if got := store.Get("plugin_multilang_default_lang"); got != "zh" {
		t.Errorf("cached new key = %q", got)
	}
	if got := store.Get("plugin_multi-language_default_lang"); got != "" {
		t.Errorf("legacy key still served from cache: %q", got)
	}
}

// T16 ③: a second run finds nothing and writes nothing.
func TestLegacySlugMigrationIsIdempotent(t *testing.T) {
	db := legacySlugTestDB(t, "gp_")
	insertOption(t, db, "gp_options", "plugin_multi-language_default_lang", "zh", true)
	if n, err := migrateLegacySlugOptions(db, nil); err != nil || n != 1 {
		t.Fatalf("first run = %d, %v", n, err)
	}
	if n, err := migrateLegacySlugOptions(db, nil); err != nil || n != 0 {
		t.Fatalf("second run = %d, %v", n, err)
	}
}

// T16 ④: when the new key already exists, it wins and the legacy row goes.
func TestLegacySlugMigrationKeepsExistingNewKey(t *testing.T) {
	db := legacySlugTestDB(t, "gp_")
	insertOption(t, db, "gp_options", "plugin_multi-language_default_lang", "zh", true)
	insertOption(t, db, "gp_options", "plugin_multilang_default_lang", "en", true)
	if _, err := migrateLegacySlugOptions(db, nil); err != nil {
		t.Fatal(err)
	}
	rows := optionRows(t, db, "gp_options")
	if len(rows) != 1 || rows["plugin_multilang_default_lang"].Value != "en" {
		t.Fatalf("rows = %v", rows)
	}
}

// T16 ⑤: keys that only look similar are left alone.
func TestLegacySlugMigrationIgnoresLookalikeKeys(t *testing.T) {
	db := legacySlugTestDB(t, "gp_")
	for _, name := range []string{"plugin_multi_language_x", "plugin_multi-language-x_y", "plugin_active_multi-language-pro", "plugin_multi-languagex"} {
		insertOption(t, db, "gp_options", name, "v", true)
	}
	if n, err := migrateLegacySlugOptions(db, nil); err != nil || n != 0 {
		t.Fatalf("migrated %d lookalike keys, err %v", n, err)
	}
	if rows := optionRows(t, db, "gp_options"); len(rows) != 4 || rows["plugin_multi_language_x"].Value != "v" {
		t.Fatalf("rows = %v", rows)
	}
}

// T16 ⑥: sites sharing one schema only migrate their own prefix.
func TestLegacySlugMigrationOnlyTouchesCurrentPrefix(t *testing.T) {
	db := legacySlugTestDB(t, "gp_", "gp_site2_")
	insertOption(t, db, "gp_options", "plugin_active_multi-language", "true", true)
	insertOption(t, db, "gp_site2_options", "plugin_active_multi-language", "false", true)
	dbprefix.Set("gp_site2_")
	if n, err := migrateLegacySlugOptions(db, nil); err != nil || n != 1 {
		t.Fatalf("migrated %d, err %v", n, err)
	}
	if _, ok := optionRows(t, db, "gp_site2_options")["plugin_active_multilang"]; !ok {
		t.Error("current site was not migrated")
	}
	if _, ok := optionRows(t, db, "gp_options")["plugin_active_multi-language"]; !ok {
		t.Error("another site's options were changed")
	}
}

// T16 on PostgreSQL: the substr query, the in-transaction rename and the
// autoload flag behave the same as on SQLite. Runs only when
// GOPRESS_TEST_PG_DSN is set, inside a throwaway schema dropped afterwards.
func TestLegacySlugMigrationOnPostgres(t *testing.T) {
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	dbprefix.Set(dbprefix.DefaultPrefix)
	quiet := &gorm.Config{Logger: gormlogger.Discard}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schemaName := fmt.Sprintf("gopress_multilang_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), quiet)
	if err != nil {
		t.Fatalf("connect to test schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&option.Option{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []option.Option{
		{Name: "plugin_active_multi-language", Value: "false", Autoload: true},
		{Name: "plugin_multi-language_default_lang", Value: "zh", Autoload: true},
		{Name: "plugin_multi-language_auto_detect", Value: "false"},
		{Name: "plugin_multi_language_x", Value: "lookalike", Autoload: true},
		{Name: "plugin_multilang_taxonomy_tag_mode", Value: "new", Autoload: true},
		{Name: "plugin_multi-language_taxonomy_tag_mode", Value: "old", Autoload: true},
	} {
		row := row
		// autoload has a database default, so GORM omits a false value on
		// insert and reads the default back; set it explicitly afterwards.
		autoload := row.Autoload
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if !autoload {
			if err := db.Model(&option.Option{}).Where("id = ?", row.ID).Update("autoload", false).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	store := option.NewStore(db)
	store.LoadAll()

	if n, err := migrateLegacySlugOptions(db, store); err != nil || n != 4 {
		t.Fatalf("migrated %d, err %v", n, err)
	}
	rows := optionRows(t, db, "gp_options")
	want := map[string]option.Option{
		"plugin_active_multilang":            {Value: "false", Autoload: true},
		"plugin_multilang_default_lang":      {Value: "zh", Autoload: true},
		"plugin_multilang_auto_detect":       {Value: "false", Autoload: false},
		"plugin_multi_language_x":            {Value: "lookalike", Autoload: true},
		"plugin_multilang_taxonomy_tag_mode": {Value: "new", Autoload: true},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for name, w := range want {
		if got := rows[name]; got.Value != w.Value || got.Autoload != w.Autoload {
			t.Errorf("%s = %+v, want %+v", name, got, w)
		}
	}
	if store.Get("plugin_multi-language_default_lang") != "" || store.Get("plugin_multilang_default_lang") != "zh" {
		t.Error("option cache was not refreshed")
	}
	if n, err := migrateLegacySlugOptions(db, store); err != nil || n != 0 {
		t.Fatalf("second run = %d, %v", n, err)
	}
}
