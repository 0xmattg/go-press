// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package dblock

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestKeyIsStableAndDistinguishesNames(t *testing.T) {
	t.Parallel()
	if Key("gopress:content-slug:gp_contents:post:hello") != Key("gopress:content-slug:gp_contents:post:hello") {
		t.Fatal("Key() is not deterministic")
	}
	if Key("gopress:content-slug:gp_contents:post:hello") == Key("gopress:content-slug:gp_bitcuz_contents:post:hello") {
		t.Fatal("different sites produced the same lock key")
	}
}

// Unit tests run the postgres dialector over SQLite; locking must be a no-op
// there rather than an error.
func TestXactLockIsNoOpWithoutAdvisoryLocks(t *testing.T) {
	t.Parallel()
	sqlDB, err := sql.Open("sqlite3", "file:dblock_noop?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return XactLock(tx, "a", "b") }); err != nil {
		t.Fatalf("XactLock() on SQLite = %v, want nil", err)
	}
	if err := XactLock(nil, "a"); err != nil {
		t.Fatalf("XactLock(nil) = %v, want nil", err)
	}
}

// TestXactLockSerializesTransactions runs against a real PostgreSQL when
// GOPRESS_TEST_PG_DSN is set (for example
// "host=localhost user=postgres dbname=testdb sslmode=disable").
func TestXactLockSerializesTransactions(t *testing.T) {
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	key := fmt.Sprintf("gopress:dblock-itest:%d", time.Now().UnixNano())

	holder := db.Begin()
	if err := XactLock(holder, key); err != nil {
		t.Fatalf("first lock: %v", err)
	}

	// A different key is independent and must not wait.
	if err := db.Transaction(func(tx *gorm.DB) error { return XactLock(tx, key+":other") }); err != nil {
		t.Fatalf("unrelated key: %v", err)
	}

	acquired := make(chan time.Time, 1)
	go func() {
		_ = db.Transaction(func(tx *gorm.DB) error {
			if err := XactLock(tx, key); err != nil {
				return err
			}
			acquired <- time.Now()
			return nil
		})
	}()

	select {
	case <-acquired:
		t.Fatal("second transaction acquired the lock while the first still held it")
	case <-time.After(300 * time.Millisecond):
	}
	released := time.Now()
	if err := holder.Commit().Error; err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	select {
	case at := <-acquired:
		if at.Before(released) {
			t.Fatalf("lock acquired at %v before release at %v", at, released)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second transaction never acquired the lock after the first committed")
	}
}
