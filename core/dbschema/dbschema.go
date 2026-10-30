// Package dbschema keeps GORM-managed schemas correct when several GoPress
// sites share one PostgreSQL schema and are separated only by table prefixes.
//
// PostgreSQL index names are unique per schema, not per table. Models that
// declare a fixed index name (for example uniqueIndex:idx_user_identity_key)
// therefore get that index only on the first site that migrates; GORM's
// "CREATE INDEX IF NOT EXISTS" silently skips it for every other prefix, so
// those sites lose their uniqueness guarantees without any error.
//
// EnsureSiteIndexes repairs this additively: for every index a model declares
// it looks for an equivalent index on the current site's own table and, only
// when none exists, creates one under a table-derived name. It never drops or
// renames an index, so previously deployed binaries keep working against the
// migrated database.
package dbschema

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/0xmattg/go-press/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// maxIdentifierLen is PostgreSQL's NAMEDATALEN - 1.
const maxIdentifierLen = 63

// AutoMigrate runs GORM AutoMigrate for models and then EnsureSiteIndexes.
//
// Only AutoMigrate errors are returned. Index repair problems are logged
// instead: a site must still boot when, for example, existing duplicate rows
// prevent a unique index, and the repair is retried on the next start.
func AutoMigrate(db *gorm.DB, models ...interface{}) error {
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	if err := EnsureSiteIndexes(db, models...); err != nil {
		logger.Warn("Site index repair incomplete; affected uniqueness or lookups stay unprotected until fixed", "error", err)
	}
	return nil
}

// EnsureSiteIndexes creates any declared index that the current site's table
// lacks. It is a no-op on databases without the PostgreSQL catalog.
func EnsureSiteIndexes(db *gorm.DB, models ...interface{}) error {
	if db == nil || !hasPostgresCatalog(db) {
		return nil
	}
	var errs []error
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			errs = append(errs, fmt.Errorf("parse %T: %w", model, err))
			continue
		}
		table := stmt.Schema.Table
		indexes := stmt.Schema.ParseIndexes()
		names := make([]string, 0, len(indexes))
		for name := range indexes {
			names = append(names, name)
		}
		sort.Strings(names)

		var existing []tableIndex
		loaded := false
		for _, name := range names {
			want, ok := declaredIndex(indexes[name])
			if !ok {
				continue
			}
			if !loaded {
				var err error
				if existing, err = loadTableIndexes(db, table); err != nil {
					errs = append(errs, fmt.Errorf("inspect indexes of %s: %w", table, err))
					break
				}
				loaded = true
			}
			if hasEquivalent(existing, want) {
				continue
			}
			created, err := createSiteIndex(db, table, want)
			if err != nil {
				errs = append(errs, fmt.Errorf("create %s on %s: %w", want.name, table, err))
				continue
			}
			logger.Info("Created site-scoped index", "table", table, "index", created, "declared", want.name)
			existing = append(existing, tableIndex{columns: want.columns, unique: want.unique, partial: want.where != ""})
		}
	}
	return errors.Join(errs...)
}

// SiteIndexName derives a schema-unique index name from the table name, which
// already carries the site prefix. Names longer than PostgreSQL allows are
// truncated and suffixed with a short hash so they stay unique.
func SiteIndexName(table, declared string) string {
	name := table + "_" + declared
	if len(name) <= maxIdentifierLen {
		return name
	}
	sum := sha256.Sum256([]byte(table + "|" + declared))
	suffix := "_" + hex.EncodeToString(sum[:4])
	return name[:maxIdentifierLen-len(suffix)] + suffix
}

type wantedIndex struct {
	name      string
	columns   []string
	unique    bool
	where     string
	indexType string
}

type tableIndex struct {
	columns []string
	unique  bool
	partial bool
}

// declaredIndex converts a GORM index into the plain column form this package
// can compare and create. Expression indexes are skipped.
func declaredIndex(idx schema.Index) (wantedIndex, bool) {
	want := wantedIndex{
		name:      idx.Name,
		unique:    idx.Class == "UNIQUE",
		where:     idx.Where,
		indexType: idx.Type,
	}
	if idx.Class != "" && !want.unique {
		return wantedIndex{}, false
	}
	for _, field := range idx.Fields {
		if field.Expression != "" || field.Field == nil || field.DBName == "" {
			return wantedIndex{}, false
		}
		want.columns = append(want.columns, field.DBName)
	}
	return want, len(want.columns) > 0
}

// hasEquivalent reports whether an existing valid index enforces at least what
// want declares: the same ordered columns, uniqueness when required, and the
// same full/partial coverage.
func hasEquivalent(existing []tableIndex, want wantedIndex) bool {
	for _, idx := range existing {
		if !equalColumns(idx.columns, want.columns) {
			continue
		}
		if want.unique && !idx.unique {
			continue
		}
		if idx.partial != (want.where != "") {
			continue
		}
		return true
	}
	return false
}

func equalColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasPostgresCatalog(db *gorm.DB) bool {
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return false
	}
	var one int
	quiet := db.Session(&gorm.Session{Logger: gormlogger.Discard})
	return quiet.Raw("SELECT 1 FROM pg_catalog.pg_index LIMIT 1").Scan(&one).Error == nil
}

func loadTableIndexes(db *gorm.DB, table string) ([]tableIndex, error) {
	var rows []struct {
		Columns string
		Unique  bool
		Partial bool
	}
	err := db.Raw(`
SELECT array_to_string(ARRAY(
         SELECT a.attname
           FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
           JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
          ORDER BY k.ord), ',') AS columns,
       i.indisunique AS "unique",
       i.indpred IS NOT NULL AS partial
  FROM pg_catalog.pg_index i
  JOIN pg_catalog.pg_class t ON t.oid = i.indrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
 WHERE t.relname = ? AND n.nspname = current_schema() AND i.indisvalid`, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	indexes := make([]tableIndex, 0, len(rows))
	for _, row := range rows {
		var columns []string
		if row.Columns != "" {
			columns = strings.Split(row.Columns, ",")
		}
		indexes = append(indexes, tableIndex{columns: columns, unique: row.Unique, partial: row.Partial})
	}
	return indexes, nil
}

func createSiteIndex(db *gorm.DB, table string, want wantedIndex) (string, error) {
	name := SiteIndexName(table, want.name)
	var taken bool
	if err := db.Raw("SELECT to_regclass(quote_ident(?)) IS NOT NULL", name).Scan(&taken).Error; err != nil {
		return "", err
	}
	if taken {
		return "", fmt.Errorf("relation %q already exists but is not an equivalent valid index", name)
	}

	columns := make([]interface{}, 0, len(want.columns))
	for _, column := range want.columns {
		columns = append(columns, clause.Column{Name: column})
	}
	sql := "CREATE "
	if want.unique {
		sql += "UNIQUE "
	}
	sql += "INDEX ? ON ?"
	if want.indexType != "" {
		sql += " USING " + want.indexType
	}
	sql += " ?"
	if want.where != "" {
		sql += " WHERE " + want.where
	}
	return name, db.Exec(sql, clause.Column{Name: name}, clause.Table{Name: table}, columns).Error
}
