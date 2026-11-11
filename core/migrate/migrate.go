// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package migrate applies versioned database migrations for one GoPress site
// and records them in the site's own <prefix>schema_migrations table.
//
// A site is one table prefix in one database schema. Several sites may share
// a schema, so the record table, the migration lock, and every index a step
// creates are scoped to the prefix (see dbschema.SiteIndexName).
//
// Each Module (core, or one plugin) lists numbered Steps. A Runner takes a
// PostgreSQL session advisory lock for the site on one pinned connection,
// runs the module's Prepare function and every pending step on that same
// connection, records the outcome, and releases the lock. Two processes of
// the same site therefore never run DDL at the same time, while different
// sites never wait for each other.
//
// Compatibility rules the Runner relies on:
//
//   - Releases may be rolled back by starting the previous binary on the
//     migrated database. Older binaries ignore the record table, so steps must
//     only add things: nullable or defaulted columns, tables, non-conflicting
//     indexes. Destructive changes wait for a later contract release.
//   - A record whose version this binary does not know (the database was
//     migrated by a newer release) is logged and otherwise ignored, so the
//     rollback still boots.
//   - Steps must be idempotent with respect to Prepare: on a new install the
//     tables Prepare creates already contain what an older step added.
//
// On databases other than PostgreSQL, such as the SQLite unit tests, locking
// is skipped and everything else behaves the same.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/dblock"
	"github.com/0xmattg/go-press/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

// TableBaseName is the record table's name without the site prefix.
const TableBaseName = "schema_migrations"

// DefaultLockTimeout bounds how long a Runner waits for another process of
// the same site to finish migrating.
const DefaultLockTimeout = 5 * time.Minute

const lockPollInterval = 500 * time.Millisecond

// Mode says how often a step runs.
type Mode string

const (
	// Once steps run until they succeed, then never again. This is the default.
	Once Mode = "once"
	// Repeatable steps run on every pass and must be idempotent. Use them for
	// backfills that must also cover rows an older binary wrote after a
	// rollback.
	Repeatable Mode = "repeatable"
)

// Phase says when a step runs.
type Phase string

const (
	// Blocking steps run before the site reports ready; a failure stops the
	// site from starting. This is the default.
	Blocking Phase = "blocking"
	// Online steps run in the background after the site is ready, for slow
	// work such as CREATE INDEX CONCURRENTLY. A failure is logged and retried
	// on the next start. Later blocking steps must not depend on them.
	Online Phase = "online"
)

// Record statuses.
const (
	StatusApplied = "applied" // the step ran and succeeded
	StatusAdopted = "adopted" // a baseline step was recorded over a schema that predates versioned migrations
	StatusFailed  = "failed"  // the last attempt failed; the step runs again next time
)

// Step is one numbered change of a module.
type Step struct {
	// Version orders the module's steps. It must be positive and unique
	// within the module; once released it never changes.
	Version int64
	// Name is a short snake_case description, for example "add_order_notes".
	Name string
	// Mode defaults to Once.
	Mode Mode
	// Phase defaults to Blocking.
	Phase Phase
	// Baseline marks the step that stands for the module's schema as it was
	// before the module used versioned migrations. When Plan.Existing reports
	// that schema present, the step is recorded as adopted and Up is not run.
	// Baseline steps must be Once and Blocking.
	Baseline bool
	// NoTransaction runs the step outside a transaction, which PostgreSQL
	// requires for statements such as CREATE INDEX CONCURRENTLY. If the
	// process stops after the change but before the record is written, the
	// step runs again, so it must be safe to repeat (IF NOT EXISTS).
	NoTransaction bool
	// SQL is executed as is. Its checksum is recorded, and a changed text is
	// reported as a warning. Use either SQL or Up; with neither the step only
	// records a marker.
	SQL string
	// Up performs the step. It receives the pinned connection, or the
	// transaction unless NoTransaction is set.
	Up func(ctx context.Context, db *gorm.DB) error
}

// Plan is what a module contributes; the module name is chosen by its owner
// (core for itself, core for each plugin).
type Plan struct {
	// Existing reports whether the module's tables were created before the
	// module used versioned migrations. It is asked once, the first time the
	// Runner sees the module, before Prepare runs. Check exact table names
	// with TableExists, never a name pattern: one site's prefix can be the
	// beginning of another's ("gp_" and "gp_shop_").
	Existing func(ctx context.Context, db *gorm.DB) (bool, error)
	// Prepare runs at the start of every blocking pass, under the lock and
	// outside a transaction. Core uses it for the schema it still keeps in
	// step with GORM AutoMigrate on every start.
	Prepare func(ctx context.Context, db *gorm.DB) error
	// Steps may be listed in any order.
	Steps []Step
}

// Module is a named Plan. Core uses "core"; plugins are "plugin:<slug>".
type Module struct {
	Name string
	Plan
}

// HasPhase reports whether any module has a step in phase.
func HasPhase(phase Phase, modules ...Module) bool {
	for _, module := range modules {
		for _, step := range module.Steps {
			if step.phase() == phase {
				return true
			}
		}
	}
	return false
}

// Record is one row of the record table.
type Record struct {
	Module     string     `gorm:"column:module"`
	Version    int64      `gorm:"column:version"`
	Name       string     `gorm:"column:name"`
	Mode       string     `gorm:"column:mode"`
	Phase      string     `gorm:"column:phase"`
	Checksum   string     `gorm:"column:checksum"`
	Status     string     `gorm:"column:status"`
	Error      string     `gorm:"column:error"`
	AppliedBy  string     `gorm:"column:applied_by"`
	AppliedAt  *time.Time `gorm:"column:applied_at"`
	LastRunAt  time.Time  `gorm:"column:last_run_at"`
	Runs       int64      `gorm:"column:runs"`
	DurationMS int64      `gorm:"column:duration_ms"`
}

// Done reports whether a Once step with this record needs no further run.
func (r Record) Done() bool {
	return r.Status == StatusApplied || r.Status == StatusAdopted
}

// Result describes what a pass did with one step.
type Result struct {
	Module   string
	Version  int64
	Name     string
	Action   string // applied, adopted, repeated, or failed
	Duration time.Duration
	Err      error
}

// Runner applies modules for one site.
type Runner struct {
	// DB is the site's connection pool. The Runner pins one connection from
	// it for the whole pass.
	DB *gorm.DB
	// Prefix is the site's table prefix, for example "gp_".
	Prefix string
	// AppVersion is recorded with each step, for example "0.7.7".
	AppVersion string
	// LockTimeout defaults to DefaultLockTimeout.
	LockTimeout time.Duration
}

// Table returns the site's record table name.
func (r *Runner) Table() string {
	return r.Prefix + TableBaseName
}

// Run applies every pending step of phase for modules, in module order and
// then version order, while holding the site's migration lock.
//
// Blocking passes also run each module's Prepare first and stop at the first
// failure. Online passes skip Prepare, stop a module at its first failure but
// carry on with the next module, and return all failures joined.
func (r *Runner) Run(ctx context.Context, phase Phase, modules ...Module) ([]Result, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("migrate: database is required")
	}
	if phase != Blocking && phase != Online {
		return nil, fmt.Errorf("migrate: unknown phase %q", phase)
	}
	for _, module := range modules {
		if err := module.validate(); err != nil {
			return nil, err
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var results []Result
	err := r.DB.WithContext(ctx).Connection(func(pinned *gorm.DB) error {
		conn := pinned.Session(&gorm.Session{NewDB: true})
		unlock, err := r.lock(ctx, conn)
		if err != nil {
			return err
		}
		defer unlock()

		if err := r.ensureTable(conn); err != nil {
			return err
		}
		var errs []error
		for _, module := range modules {
			moduleResults, err := r.runModule(ctx, conn, phase, module)
			results = append(results, moduleResults...)
			if err == nil {
				continue
			}
			if phase == Blocking {
				return err
			}
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
	return results, err
}

func (r *Runner) runModule(ctx context.Context, conn *gorm.DB, phase Phase, module Module) ([]Result, error) {
	records, err := r.records(conn, module.Name)
	if err != nil {
		return nil, err
	}
	existing := false
	if phase == Blocking {
		if len(records) == 0 && module.Existing != nil {
			if existing, err = module.Existing(ctx, conn); err != nil {
				return nil, fmt.Errorf("migrate %s: detect existing schema: %w", module.Name, err)
			}
		}
		if module.Prepare != nil {
			if err := module.Prepare(ctx, conn); err != nil {
				return nil, fmt.Errorf("migrate %s: prepare: %w", module.Name, err)
			}
		}
		warnUnknown(module, records)
	}

	var results []Result
	for _, step := range module.sortedSteps() {
		if step.phase() != phase {
			continue
		}
		record, seen := records[step.Version]
		if step.mode() == Once && seen && record.Done() {
			if sum := step.checksum(); sum != "" && record.Checksum != "" && sum != record.Checksum {
				logger.Warn("Applied migration changed since it ran; the database keeps the old version",
					"module", module.Name, "version", step.Version, "name", step.Name)
			}
			continue
		}

		started := time.Now()
		if step.Baseline && existing {
			if err := r.save(conn, module.Name, step, record, seen, StatusAdopted, "", 0); err != nil {
				return results, err
			}
			results = append(results, Result{Module: module.Name, Version: step.Version, Name: step.Name, Action: StatusAdopted})
			logger.Info("Migration baseline adopted for existing schema", "module", module.Name, "version", step.Version, "name", step.Name, "table_prefix", r.Prefix)
			continue
		}

		runErr := r.apply(ctx, conn, module.Name, step, record, seen)
		elapsed := time.Since(started)
		if runErr != nil {
			// Best effort: the failure is reported to the caller either way.
			if err := r.save(conn, module.Name, step, record, seen, StatusFailed, runErr.Error(), elapsed); err != nil {
				logger.Warn("Failed to record migration failure", "module", module.Name, "version", step.Version, "error", err)
			}
			results = append(results, Result{Module: module.Name, Version: step.Version, Name: step.Name, Action: StatusFailed, Duration: elapsed, Err: runErr})
			return results, fmt.Errorf("migrate %s %d_%s: %w", module.Name, step.Version, step.Name, runErr)
		}
		action := StatusApplied
		if step.mode() == Repeatable {
			action = "repeated"
		}
		results = append(results, Result{Module: module.Name, Version: step.Version, Name: step.Name, Action: action, Duration: elapsed})
		if step.mode() == Once {
			logger.Info("Migration applied", "module", module.Name, "version", step.Version, "name", step.Name, "phase", string(phase), "duration", elapsed.String())
		}
	}
	return results, nil
}

// apply runs a step and writes its record, in one transaction unless the
// step opts out.
func (r *Runner) apply(ctx context.Context, conn *gorm.DB, module string, step Step, previous Record, seen bool) error {
	run := func(db *gorm.DB) error {
		started := time.Now()
		if step.SQL != "" {
			if err := db.Exec(step.SQL).Error; err != nil {
				return err
			}
		} else if step.Up != nil {
			if err := step.Up(ctx, db); err != nil {
				return err
			}
		}
		return r.save(db, module, step, previous, seen, StatusApplied, "", time.Since(started))
	}
	if step.NoTransaction {
		return run(conn)
	}
	return conn.Transaction(run)
}

// save inserts or updates a step's record. The caller holds the site lock,
// so a read-then-write is safe and works on every dialect.
func (r *Runner) save(db *gorm.DB, module string, step Step, previous Record, seen bool, status, message string, elapsed time.Duration) error {
	now := time.Now().UTC()
	rec := Record{
		Module:     module,
		Version:    step.Version,
		Name:       step.Name,
		Mode:       string(step.mode()),
		Phase:      string(step.phase()),
		Checksum:   step.checksum(),
		Status:     status,
		Error:      truncate(message, 2000),
		AppliedBy:  r.AppVersion,
		LastRunAt:  now,
		Runs:       previous.Runs + 1,
		DurationMS: elapsed.Milliseconds(),
	}
	switch {
	case previous.AppliedAt != nil:
		rec.AppliedAt = previous.AppliedAt
	case status != StatusFailed:
		rec.AppliedAt = &now
	}
	if !seen {
		return db.Table(r.Table()).Create(&rec).Error
	}
	return db.Table(r.Table()).
		Where("module = ? AND version = ?", module, step.Version).
		Select("*").
		Updates(&rec).Error
}

// Status returns every record of the site, ordered by module and version.
// It does not take the lock and does not create the table; a missing table
// yields no records.
func (r *Runner) Status(ctx context.Context) ([]Record, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("migrate: database is required")
	}
	db := r.DB.WithContext(ctx)
	exists, err := TableExists(ctx, db, r.Table())
	if err != nil || !exists {
		return nil, err
	}
	var rows []Record
	err = db.Table(r.Table()).Order("module, version").Find(&rows).Error
	return rows, err
}

// Pending lists the steps of modules that a run would execute, given the
// records Status returned. Repeatable steps are always listed.
func Pending(records []Record, modules ...Module) []Step {
	done := make(map[string]bool, len(records))
	for _, rec := range records {
		if rec.Done() {
			done[fmt.Sprintf("%s\x00%d", rec.Module, rec.Version)] = true
		}
	}
	var out []Step
	for _, module := range modules {
		for _, step := range module.sortedSteps() {
			if step.mode() == Once && done[fmt.Sprintf("%s\x00%d", module.Name, step.Version)] {
				continue
			}
			out = append(out, step)
		}
	}
	return out
}

// Unknown returns records of modules whose versions the modules do not
// declare, usually because a newer release migrated the database.
func Unknown(records []Record, modules ...Module) []Record {
	known := make(map[string]map[int64]bool, len(modules))
	for _, module := range modules {
		versions := make(map[int64]bool, len(module.Steps))
		for _, step := range module.Steps {
			versions[step.Version] = true
		}
		known[module.Name] = versions
	}
	var out []Record
	for _, rec := range records {
		if versions, ok := known[rec.Module]; ok && !versions[rec.Version] {
			out = append(out, rec)
		}
	}
	return out
}

// TableExists reports whether a table with exactly this name is visible in
// the current schema. On PostgreSQL it asks to_regclass for the exact name;
// elsewhere it probes the table with a query that reads no rows.
func TableExists(ctx context.Context, db *gorm.DB, table string) (bool, error) {
	db = db.WithContext(ctx)
	if !isPostgres(db) {
		return quiet(db).Exec("SELECT 1 FROM ? WHERE 1 = 0", clause.Table{Name: table}).Error == nil, nil
	}
	var found bool
	err := quiet(db).Raw("SELECT to_regclass(quote_ident(?)) IS NOT NULL", table).Scan(&found).Error
	return found, err
}

func (r *Runner) ensureTable(db *gorm.DB) error {
	timestamp := "TIMESTAMP"
	if isPostgres(db) {
		timestamp = "TIMESTAMPTZ"
	}
	ddl := `CREATE TABLE IF NOT EXISTS ? (
	module VARCHAR(100) NOT NULL,
	version BIGINT NOT NULL,
	name VARCHAR(191) NOT NULL,
	mode VARCHAR(16) NOT NULL,
	phase VARCHAR(16) NOT NULL,
	checksum VARCHAR(64) NOT NULL DEFAULT '',
	status VARCHAR(16) NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	applied_by VARCHAR(40) NOT NULL DEFAULT '',
	applied_at ` + timestamp + ` NULL,
	last_run_at ` + timestamp + ` NOT NULL,
	runs BIGINT NOT NULL DEFAULT 0,
	duration_ms BIGINT NOT NULL DEFAULT 0,
	PRIMARY KEY (module, version)
)`
	if err := quiet(db).Exec(ddl, clause.Table{Name: r.Table()}).Error; err != nil {
		return fmt.Errorf("migrate: create %s: %w", r.Table(), err)
	}
	return nil
}

func (r *Runner) records(db *gorm.DB, module string) (map[int64]Record, error) {
	var rows []Record
	if err := db.Table(r.Table()).Where("module = ?", module).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("migrate: read %s: %w", r.Table(), err)
	}
	out := make(map[int64]Record, len(rows))
	for _, row := range rows {
		out[row.Version] = row
	}
	return out, nil
}

// lock takes the site's session advisory lock on the pinned connection and
// returns its release. Polling with pg_try_advisory_lock lets the wait time
// out and name the process holding the lock.
func (r *Runner) lock(ctx context.Context, conn *gorm.DB) (func(), error) {
	if !isPostgres(conn) {
		return func() {}, nil
	}
	q := quiet(conn)
	var schema string
	if err := q.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		return nil, fmt.Errorf("migrate: read current schema: %w", err)
	}
	key := LockKey(schema, r.Prefix)
	timeout := r.LockTimeout
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	deadline := time.Now().Add(timeout)
	waiting := false
	for {
		var got bool
		if err := q.Raw("SELECT pg_try_advisory_lock(?)", key).Scan(&got).Error; err != nil {
			return nil, fmt.Errorf("migrate: take lock: %w", err)
		}
		if got {
			if waiting {
				logger.Info("Migration lock acquired", "schema", schema, "table_prefix", r.Prefix)
			}
			return func() { r.unlock(conn, key) }, nil
		}
		if !waiting {
			waiting = true
			logger.Info("Waiting for another process of this site to finish migrating",
				"schema", schema, "table_prefix", r.Prefix, "holder_pid", lockHolder(q, key), "timeout", timeout.String())
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("migrate: timed out after %s waiting for the migration lock of %s.%s* (held by backend pid %d)",
				timeout, schema, r.Prefix, lockHolder(q, key))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}

// unlock releases the lock. If that fails, the pinned connection is marked
// bad so the pool closes it, which ends the session and frees the lock.
func (r *Runner) unlock(conn *gorm.DB, key int64) {
	var released bool
	err := quiet(conn).Raw("SELECT pg_advisory_unlock(?)", key).Scan(&released).Error
	if err == nil && released {
		return
	}
	logger.Warn("Failed to release migration lock; closing its connection", "table_prefix", r.Prefix, "error", err)
	if sqlConn, ok := conn.Statement.ConnPool.(*sql.Conn); ok {
		_ = sqlConn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

// LockKey is the advisory lock key of one site. PostgreSQL scopes advisory
// locks to the database, so sites in different databases never collide.
func LockKey(schema, prefix string) int64 {
	return dblock.Key("gopress:migrate:" + schema + ":" + prefix)
}

func lockHolder(db *gorm.DB, key int64) int64 {
	var pid int64
	_ = db.Raw(`SELECT pid FROM pg_catalog.pg_locks
 WHERE locktype = 'advisory' AND granted
   AND database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
   AND classid::bigint = ? AND objid::bigint = ? AND objsubid = 1
 LIMIT 1`, int64(uint64(key)>>32), int64(uint64(key)&0xffffffff)).Scan(&pid).Error
	return pid
}

var stepName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (m Module) validate() error {
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 100 {
		return fmt.Errorf("migrate: module name %q must be 1-100 characters", m.Name)
	}
	seen := make(map[int64]bool, len(m.Steps))
	for _, step := range m.Steps {
		where := fmt.Sprintf("migrate %s step %d", m.Name, step.Version)
		switch {
		case step.Version <= 0:
			return fmt.Errorf("%s: version must be positive", where)
		case seen[step.Version]:
			return fmt.Errorf("%s: duplicate version", where)
		case !stepName.MatchString(step.Name) || len(step.Name) > 191:
			return fmt.Errorf("%s: name %q must be snake_case", where, step.Name)
		case step.SQL != "" && step.Up != nil:
			return fmt.Errorf("%s: set SQL or Up, not both", where)
		case step.Mode != "" && step.Mode != Once && step.Mode != Repeatable:
			return fmt.Errorf("%s: unknown mode %q", where, step.Mode)
		case step.Phase != "" && step.Phase != Blocking && step.Phase != Online:
			return fmt.Errorf("%s: unknown phase %q", where, step.Phase)
		case step.Baseline && (step.mode() != Once || step.phase() != Blocking):
			return fmt.Errorf("%s: a baseline step must be once and blocking", where)
		}
		seen[step.Version] = true
	}
	return nil
}

func (m Module) sortedSteps() []Step {
	steps := append([]Step(nil), m.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].Version < steps[j].Version })
	return steps
}

func warnUnknown(module Module, records map[int64]Record) {
	rows := make([]Record, 0, len(records))
	for _, rec := range records {
		rows = append(rows, rec)
	}
	for _, rec := range Unknown(rows, module) {
		logger.Warn("Database has a migration this binary does not know; it was probably applied by a newer release",
			"module", rec.Module, "version", rec.Version, "name", rec.Name, "applied_by", rec.AppliedBy)
	}
}

func (s Step) mode() Mode {
	if s.Mode == "" {
		return Once
	}
	return s.Mode
}

func (s Step) phase() Phase {
	if s.Phase == "" {
		return Blocking
	}
	return s.Phase
}

func (s Step) checksum() string {
	if s.SQL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.SQL))
	return hex.EncodeToString(sum[:])
}

// isPostgres reports whether db is a real PostgreSQL server. Some unit tests
// run the postgres dialector over SQLite, so the catalog is probed as well.
func isPostgres(db *gorm.DB) bool {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return false
	}
	var found int
	err := quiet(db).Raw("SELECT 1 FROM pg_catalog.pg_proc WHERE proname = 'pg_try_advisory_lock' LIMIT 1").Scan(&found).Error
	return err == nil && found == 1
}

func quiet(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{Logger: gormlogger.Discard})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
