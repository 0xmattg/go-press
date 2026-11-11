// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// pgSchema creates a throwaway schema in the database named by
// GOPRESS_TEST_PG_DSN (for example "host=localhost user=postgres
// password=... dbname=testdb sslmode=disable") and returns a function that
// opens a new connection pool on it, one per simulated process. The schema is
// dropped afterwards.
func pgSchema(t *testing.T) (schema string, open func() *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	quiet := &gorm.Config{Logger: gormlogger.Discard}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schema = fmt.Sprintf("gopress_migrate_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	var pools []*gorm.DB
	t.Cleanup(func() {
		for _, db := range pools {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return schema, func() *gorm.DB {
		db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: gormlogger.Discard})
		if err != nil {
			t.Fatalf("connect to test schema: %v", err)
		}
		pools = append(pools, db)
		return db
	}
}

func advisoryLocksHeld(t *testing.T, db *gorm.DB, key int64) int {
	t.Helper()
	var n int
	err := db.Raw(`SELECT count(*) FROM pg_catalog.pg_locks
 WHERE locktype = 'advisory' AND granted AND classid::bigint = ? AND objid::bigint = ? AND objsubid = 1`,
		int64(uint64(key)>>32), int64(uint64(key)&0xffffffff)).Scan(&n).Error
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPGSameSiteProcessesMigrateOneAtATime(t *testing.T) {
	schema, open := pgSchema(t)
	var active, maxActive, onceRuns int32
	enter := func() {
		n := atomic.AddInt32(&active, 1)
		for {
			m := atomic.LoadInt32(&maxActive)
			if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
		atomic.AddInt32(&active, -1)
	}
	module := Module{Name: "core", Plan: Plan{
		Prepare: func(context.Context, *gorm.DB) error { enter(); return nil },
		Steps: []Step{{Version: 1, Name: "slow_once", Up: func(context.Context, *gorm.DB) error {
			atomic.AddInt32(&onceRuns, 1)
			enter()
			return nil
		}}},
	}}

	const processes = 3
	var wg sync.WaitGroup
	errs := make(chan error, processes)
	for i := 0; i < processes; i++ {
		r := &Runner{DB: open(), Prefix: "gp_"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Run(context.Background(), Blocking, module)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxActive != 1 || onceRuns != 1 {
		t.Fatalf("max concurrent migration work = %d, once step runs = %d; want 1 and 1", maxActive, onceRuns)
	}
	if held := advisoryLocksHeld(t, open(), LockKey(schema, "gp_")); held != 0 {
		t.Fatalf("%d migration locks still held after the runs", held)
	}
}

func TestPGSitesWithOverlappingPrefixesDoNotWait(t *testing.T) {
	_, open := pgSchema(t)
	started, release := make(chan struct{}), make(chan struct{})
	slow := Module{Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "hold", Up: func(context.Context, *gorm.DB) error {
		close(started)
		<-release
		return nil
	}}}}}
	done := make(chan error, 1)
	go func() {
		_, err := (&Runner{DB: open(), Prefix: "gp_"}).Run(context.Background(), Blocking, slow)
		done <- err
	}()
	<-started

	// "gp_x_" starts with "gp_": its lock, records, and baseline are its own.
	other := &Runner{DB: open(), Prefix: "gp_x_", LockTimeout: 2 * time.Second}
	baseline := Module{Name: "core", Plan: Plan{
		Existing: func(ctx context.Context, db *gorm.DB) (bool, error) { return TableExists(ctx, db, "gp_x_contents") },
		Steps:    []Step{{Version: 1, Name: "baseline", Baseline: true}},
	}}
	if err := other.DB.Exec(`CREATE TABLE gp_contents (id bigint)`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := other.Run(context.Background(), Blocking, baseline); err != nil {
		t.Fatalf("site gp_x_ waited for site gp_: %v", err)
	}
	rows, err := other.Status(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Status != StatusApplied {
		t.Fatalf("gp_x_ records = %+v, %v; want its own baseline applied, not adopted from gp_contents", rows, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPGLockTimeoutNamesTheHolder(t *testing.T) {
	schema, open := pgSchema(t)
	holder := open()
	ctx := context.Background()
	err := holder.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SELECT pg_advisory_lock(?)", LockKey(schema, "gp_")).Error; err != nil {
			return err
		}
		defer conn.Exec("SELECT pg_advisory_unlock(?)", LockKey(schema, "gp_"))

		r := &Runner{DB: open(), Prefix: "gp_", LockTimeout: time.Second}
		_, runErr := r.Run(ctx, Blocking, Module{Name: "core"})
		if runErr == nil || !strings.Contains(runErr.Error(), "timed out") || !strings.Contains(runErr.Error(), "pid") ||
			strings.Contains(runErr.Error(), "pid 0)") {
			t.Fatalf("Run error = %v, want a timeout naming the holder's pid", runErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPGFailedRunReleasesLockAndRollsBack(t *testing.T) {
	schema, open := pgSchema(t)
	db := open()
	r := &Runner{DB: db, Prefix: "gp_"}
	module := Module{Name: "core", Plan: Plan{Steps: []Step{{Version: 1, Name: "half_done", Up: func(_ context.Context, tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TABLE gp_half (id bigint)`).Error; err != nil {
			return err
		}
		return tx.Exec(`SELECT * FROM gp_missing_table`).Error
	}}}}}
	if _, err := r.Run(context.Background(), Blocking, module); err == nil {
		t.Fatal("Run succeeded although the step failed")
	}
	if exists, _ := TableExists(context.Background(), db, "gp_half"); exists {
		t.Fatal("DDL of a failed step must be rolled back")
	}
	if held := advisoryLocksHeld(t, db, LockKey(schema, "gp_")); held != 0 {
		t.Fatal("a failed run must release the migration lock")
	}
	rows, _ := r.Status(context.Background())
	if len(rows) != 1 || rows[0].Status != StatusFailed || !strings.Contains(rows[0].Error, "gp_missing_table") {
		t.Fatalf("records = %+v", rows)
	}
}

func TestPGOnlineStepCanBuildIndexConcurrently(t *testing.T) {
	_, open := pgSchema(t)
	db := open()
	if err := db.Exec(`CREATE TABLE gp_items (id bigint, code text)`).Error; err != nil {
		t.Fatal(err)
	}
	r := &Runner{DB: db, Prefix: "gp_"}
	module := Module{Name: "core", Plan: Plan{Steps: []Step{{
		Version: 1, Name: "index_items_code", Phase: Online, NoTransaction: true,
		SQL: `CREATE INDEX CONCURRENTLY IF NOT EXISTS gp_items_idx_code ON gp_items (code)`,
	}}}}
	if _, err := r.Run(context.Background(), Blocking, module); err != nil {
		t.Fatal(err)
	}
	if exists, _ := TableExists(context.Background(), db, "gp_items_idx_code"); exists {
		t.Fatal("the blocking pass must leave online steps alone")
	}
	if _, err := r.Run(context.Background(), Online, module); err != nil {
		t.Fatalf("online pass: %v", err)
	}
	if exists, _ := TableExists(context.Background(), db, "gp_items_idx_code"); !exists {
		t.Fatal("online step did not create the index")
	}
}
