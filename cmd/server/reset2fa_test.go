// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/mfa"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func resetTestDB(t *testing.T) *gorm.DB {
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
	for _, statement := range []string{
		`CREATE TABLE gp_users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, email TEXT UNIQUE,
			password_hash TEXT, display_name TEXT, avatar_url TEXT, role TEXT NOT NULL DEFAULT 'subscriber',
			is_active BOOLEAN NOT NULL DEFAULT TRUE, last_login_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE gp_audit_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, username TEXT, action TEXT NOT NULL,
			resource TEXT, resource_id INTEGER, details TEXT, ip_address TEXT, created_at DATETIME)`,
		`CREATE TABLE gp_user_mfa (user_id INTEGER PRIMARY KEY, totp_secret TEXT NOT NULL DEFAULT '',
			pending_secret TEXT NOT NULL DEFAULT '', pending_expires_at DATETIME, key_id TEXT NOT NULL DEFAULT '',
			enabled_at DATETIME, last_step INTEGER NOT NULL DEFAULT 0, recovery_hashes TEXT NOT NULL DEFAULT '',
			recovery_generated_at DATETIME, failed_count INTEGER NOT NULL DEFAULT 0, failed_since DATETIME,
			locked_until DATETIME, sessions_not_before DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`INSERT INTO gp_users (id, username, role) VALUES (1, 'admin', 'super_admin')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "requires cgo") {
				t.Skip("needs CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	return db
}

func TestResetSecondFactorCommand(t *testing.T) {
	db := resetTestDB(t)
	m, err := mfa.New(db, "site-secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e, err := m.Begin(ctx, 1, "GoPress", "admin")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := mfa.CodeAt(e.Secret, time.Now())
	if _, _, err := m.Confirm(ctx, 1, code); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if got := resetSecondFactorIn(&out, &errOut, db, "site-secret", "nobody"); got != 1 || !strings.Contains(errOut.String(), "no account") {
		t.Fatalf("unknown user = %d %q", got, errOut.String())
	}
	out.Reset()
	if got := resetSecondFactorIn(&out, &errOut, db, "site-secret", "admin"); got != 0 || !strings.Contains(out.String(), "is off for admin") {
		t.Fatalf("reset = %d %q %q", got, out.String(), errOut.String())
	}
	_, on, notBefore, err := m.SecondFactorSession(1)
	if err != nil || on || notBefore.IsZero() {
		t.Fatalf("after the command: on=%v notBefore=%v err=%v", on, notBefore, err)
	}
	var actions []string
	if err := db.Raw(`SELECT action || ' ' || details FROM gp_audit_logs`).Scan(&actions).Error; err != nil || len(actions) != 1 || !strings.Contains(actions[0], "-reset-2fa") {
		t.Fatalf("audit = %v %v", actions, err)
	}
	if got := resetSecondFactor(&out, &errOut, "admin", &config.Config{}, "site.toml"); got != 2 {
		t.Fatalf("site not installed = %d", got)
	}
}
