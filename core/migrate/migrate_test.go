// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// sqliteDB opens an in-memory SQLite database behind the postgres dialector,
// the same arrangement other core unit tests use. The Runner detects that it
// is not a real PostgreSQL server and skips locking.
func sqliteDB(t *testing.T) *gorm.DB {
	t.Helper()
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
	if err := db.Exec("SELECT 1").Error; err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("migration tests require CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	return db
}

func recordsByVersion(t *testing.T, r *Runner, module string) map[int64]Record {
	t.Helper()
	rows, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]Record{}
	for _, row := range rows {
		if row.Module == module {
			out[row.Version] = row
		}
	}
	return out
}

func TestRunAppliesOnceStepsOnlyOnceAndRepeatableEveryPass(t *testing.T) {
	db := sqliteDB(t)
	r := &Runner{DB: db, Prefix: "gp_", AppVersion: "9.9.9"}
	onceRuns, repeatRuns, prepareRuns := 0, 0, 0
	module := Module{Name: "core", Plan: Plan{
		Prepare: func(context.Context, *gorm.DB) error { prepareRuns++; return nil },
		Steps: []Step{
			{Version: 3, Name: "backfill", Mode: Repeatable, Up: func(context.Context, *gorm.DB) error { repeatRuns++; return nil }},
			{Version: 2, Name: "create_notes", SQL: `CREATE TABLE gp_notes (id INTEGER PRIMARY KEY)`},
			{Version: 1, Name: "count", Up: func(context.Context, *gorm.DB) error { onceRuns++; return nil }},
		},
	}}

	for pass := 0; pass < 3; pass++ {
		if _, err := r.Run(context.Background(), Blocking, module); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	if onceRuns != 1 || repeatRuns != 3 || prepareRuns != 3 {
		t.Fatalf("once=%d repeatable=%d prepare=%d, want 1, 3, 3", onceRuns, repeatRuns, prepareRuns)
	}
	if exists, _ := TableExists(context.Background(), db, "gp_notes"); !exists {
		t.Fatal("SQL step did not run")
	}
	records := recordsByVersion(t, r, "core")
	if len(records) != 3 {
		t.Fatalf("records = %+v", records)
	}
	if rec := records[2]; rec.Status != StatusApplied || rec.Checksum == "" || rec.AppliedBy != "9.9.9" || rec.AppliedAt == nil || rec.Runs != 1 {
		t.Fatalf("SQL step record = %+v", rec)
	}
	if rec := records[3]; rec.Runs != 3 || rec.Mode != string(Repeatable) {
		t.Fatalf("repeatable record = %+v", rec)
	}
	if pending := Pending(mustStatus(t, r), module); len(pending) != 1 || pending[0].Version != 3 {
		t.Fatalf("Pending = %+v, want only the repeatable step", pending)
	}
}

func TestRunAdoptsBaselineOnlyForPreexistingSchema(t *testing.T) {
	db := sqliteDB(t)
	if err := db.Exec(`CREATE TABLE gp_contents (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatal(err)
	}
	baselineUp := 0
	plan := func(table string) Plan {
		return Plan{
			Existing: func(ctx context.Context, db *gorm.DB) (bool, error) { return TableExists(ctx, db, table) },
			// Prepare creates the tables, as AutoMigrate does; Existing must
			// have been asked before it.
			Prepare: func(_ context.Context, db *gorm.DB) error {
				return db.Exec(`CREATE TABLE IF NOT EXISTS ` + table + ` (id INTEGER PRIMARY KEY)`).Error
			},
			Steps: []Step{{Version: 1, Name: "baseline", Baseline: true, Up: func(context.Context, *gorm.DB) error { baselineUp++; return nil }}},
		}
	}

	existing := &Runner{DB: db, Prefix: "gp_"}
	if _, err := existing.Run(context.Background(), Blocking, Module{Name: "core", Plan: plan("gp_contents")}); err != nil {
		t.Fatal(err)
	}
	if rec := recordsByVersion(t, existing, "core")[1]; rec.Status != StatusAdopted || baselineUp != 0 {
		t.Fatalf("existing site baseline = %+v (Up ran %d times), want adopted without running Up", rec, baselineUp)
	}

	// "gp_" is a string prefix of "gp_x_": the new site must not mistake the
	// other site's tables for its own.
	fresh := &Runner{DB: db, Prefix: "gp_x_"}
	if _, err := fresh.Run(context.Background(), Blocking, Module{Name: "core", Plan: plan("gp_x_contents")}); err != nil {
		t.Fatal(err)
	}
	if rec := recordsByVersion(t, fresh, "core")[1]; rec.Status != StatusApplied || baselineUp != 1 {
		t.Fatalf("fresh site baseline = %+v (Up ran %d times), want applied", rec, baselineUp)
	}

	// A later pass changes nothing.
	if _, err := existing.Run(context.Background(), Blocking, Module{Name: "core", Plan: plan("gp_contents")}); err != nil {
		t.Fatal(err)
	}
	if rec := recordsByVersion(t, existing, "core")[1]; rec.Status != StatusAdopted || rec.Runs != 1 {
		t.Fatalf("second pass changed the baseline record: %+v", rec)
	}
}

func TestRunRecordsFailureRollsBackAndRetries(t *testing.T) {
	db := sqliteDB(t)
	r := &Runner{DB: db, Prefix: "gp_"}
	fail := true
	later := 0
	module := func() Module {
		return Module{Name: "core", Plan: Plan{Steps: []Step{
			{Version: 1, Name: "create_then_fail", Up: func(_ context.Context, tx *gorm.DB) error {
				if err := tx.Exec(`CREATE TABLE gp_half (id INTEGER PRIMARY KEY)`).Error; err != nil {
					return err
				}
				if fail {
					return errors.New("boom")
				}
				return nil
			}},
			{Version: 2, Name: "later", Up: func(context.Context, *gorm.DB) error { later++; return nil }},
		}}}
	}

	_, err := r.Run(context.Background(), Blocking, module())
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "1_create_then_fail") {
		t.Fatalf("Run error = %v, want the step failure", err)
	}
	if exists, _ := TableExists(context.Background(), db, "gp_half"); exists {
		t.Fatal("a failed step must be rolled back")
	}
	if later != 0 {
		t.Fatal("steps after a blocking failure must not run")
	}
	if rec := recordsByVersion(t, r, "core")[1]; rec.Status != StatusFailed || !strings.Contains(rec.Error, "boom") || rec.AppliedAt != nil {
		t.Fatalf("failure record = %+v", rec)
	}

	fail = false
	if _, err := r.Run(context.Background(), Blocking, module()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	records := recordsByVersion(t, r, "core")
	if rec := records[1]; rec.Status != StatusApplied || rec.Error != "" || rec.Runs != 2 || rec.AppliedAt == nil {
		t.Fatalf("retried record = %+v", rec)
	}
	if later != 1 || records[2].Status != StatusApplied {
		t.Fatalf("later step ran %d times, record %+v", later, records[2])
	}
}

func TestRunSeparatesBlockingAndOnlinePhases(t *testing.T) {
	db := sqliteDB(t)
	r := &Runner{DB: db, Prefix: "gp_"}
	var ran []string
	step := func(version int64, name string, phase Phase, err error) Step {
		return Step{Version: version, Name: name, Phase: phase, Up: func(context.Context, *gorm.DB) error {
			ran = append(ran, name)
			return err
		}}
	}
	broken := Module{Name: "plugin:broken", Plan: Plan{Steps: []Step{
		step(1, "index_fails", Online, errors.New("index failed")),
		step(2, "after_failure", Online, nil),
	}}}
	healthy := Module{Name: "core", Plan: Plan{Steps: []Step{
		step(1, "blocking_one", Blocking, nil),
		step(2, "online_one", Online, nil),
	}}}
	if !HasPhase(Online, healthy) || HasPhase(Online, Module{Name: "x", Plan: Plan{Steps: []Step{step(1, "b", Blocking, nil)}}}) {
		t.Fatal("HasPhase misreports online steps")
	}

	if _, err := r.Run(context.Background(), Blocking, broken, healthy); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "blocking_one" {
		t.Fatalf("blocking pass ran %v", ran)
	}

	ran = nil
	_, err := r.Run(context.Background(), Online, broken, healthy)
	if err == nil || !strings.Contains(err.Error(), "index failed") {
		t.Fatalf("online error = %v", err)
	}
	if strings.Join(ran, ",") != "index_fails,online_one" {
		t.Fatalf("online pass ran %v: a failure stops its module but not the next one", ran)
	}
}

func TestUnknownRecordsAreReportedNotRejected(t *testing.T) {
	db := sqliteDB(t)
	r := &Runner{DB: db, Prefix: "gp_", AppVersion: "9.0.0"}
	newer := Module{Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "baseline"}, {Version: 2, Name: "from_newer_release"}}}}
	if _, err := r.Run(context.Background(), Blocking, newer); err != nil {
		t.Fatal(err)
	}
	older := Module{Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "baseline"}}}}
	r.AppVersion = "8.0.0"
	if _, err := r.Run(context.Background(), Blocking, older); err != nil {
		t.Fatalf("an older binary must still start on a newer database: %v", err)
	}
	unknown := Unknown(mustStatus(t, r), older)
	if len(unknown) != 1 || unknown[0].Version != 2 || unknown[0].AppliedBy != "9.0.0" {
		t.Fatalf("Unknown = %+v", unknown)
	}
}

func TestRunRejectsInvalidModules(t *testing.T) {
	db := sqliteDB(t)
	r := &Runner{DB: db, Prefix: "gp_"}
	cases := map[string]Module{
		"empty name":          {Plan: Plan{Steps: []Step{{Version: 1, Name: "a"}}}},
		"zero version":        {Name: "core", Plan: Plan{Steps: []Step{{Version: 0, Name: "a"}}}},
		"duplicate version":   {Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "a"}, {Version: 1, Name: "b"}}}},
		"bad name":            {Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "Add Column"}}}},
		"sql and up":          {Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "a", SQL: "SELECT 1", Up: func(context.Context, *gorm.DB) error { return nil }}}}},
		"online baseline":     {Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "a", Baseline: true, Phase: Online}}}},
		"repeatable baseline": {Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "a", Baseline: true, Mode: Repeatable}}}},
	}
	for name, module := range cases {
		if _, err := r.Run(context.Background(), Blocking, module); err == nil {
			t.Errorf("%s: Run accepted an invalid module", name)
		}
	}
	if exists, _ := TableExists(context.Background(), db, r.Table()); exists {
		t.Fatal("validation must happen before anything is written")
	}
}

func TestStatusWithoutRecordTable(t *testing.T) {
	r := &Runner{DB: sqliteDB(t), Prefix: "gp_"}
	records, err := r.Status(context.Background())
	if err != nil || len(records) != 0 {
		t.Fatalf("Status = %v, %v; want no records and no error", records, err)
	}
	if r.Table() != "gp_schema_migrations" {
		t.Fatalf("Table() = %q", r.Table())
	}
}

func mustStatus(t *testing.T, r *Runner) []Record {
	t.Helper()
	rows, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
