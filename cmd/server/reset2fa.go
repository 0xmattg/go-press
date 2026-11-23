// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/mfa"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"
	pg "github.com/0xmattg/go-press/pkg/postgresql"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// runResetSecondFactor implements -reset-2fa: it turns two-step
// verification off for one admin account and ends that account's sessions,
// for an owner who lost both their phone and their recovery codes when no
// other super admin can reset it in the admin. It needs shell access to the
// server, as reading config.toml does, and writes the audit log. The account
// sets two-step verification up again after signing in.
func runResetSecondFactor(username string, cfg *config.Config, configPath string) int {
	return resetSecondFactor(os.Stdout, os.Stderr, username, cfg, configPath)
}

func resetSecondFactor(stdout, stderr io.Writer, username string, cfg *config.Config, configPath string) int {
	if cfg == nil || !cfg.Install.Completed {
		fmt.Fprintf(stderr, "-reset-2fa needs an installed site, but %s is missing or not installed yet\n", configPath)
		return 2
	}
	if cfg.PG.TablePrefix != "" {
		dbprefix.Set(cfg.PG.TablePrefix)
	}
	db, err := pg.NewConnection(cfg.PG)
	if err != nil {
		fmt.Fprintf(stderr, "connect to the database: %v\n", err)
		return 1
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Discard})
	return resetSecondFactorIn(stdout, stderr, db, cfg.CMS.JWTSecret, username)
}

func resetSecondFactorIn(stdout, stderr io.Writer, db *gorm.DB, jwtSecret, username string) int {
	account, err := user.NewRepository(db).FindByUsername(username)
	if err != nil {
		fmt.Fprintf(stderr, "no account named %q\n", username)
		return 1
	}
	service, err := mfa.New(db, jwtSecret)
	if err != nil {
		fmt.Fprintf(stderr, "two-step verification: %v\n", err)
		return 1
	}
	_, on, err := service.EnabledAt(account.ID)
	if err != nil {
		fmt.Fprintf(stderr, "read two-step verification (start the server once, or run -migrate=up, after upgrading): %v\n", err)
		return 1
	}
	ctx := context.Background()
	if err := service.Reset(ctx, account.ID); err != nil {
		fmt.Fprintf(stderr, "reset two-step verification: %v\n", err)
		return 1
	}
	_ = audit.NewService(db).Record(ctx, &audit.Event{
		UserID: account.ID, Username: account.Username, Action: "mfa_reset", Resource: "user",
		ResourceID: account.ID, Details: "reset from the server command line (-reset-2fa)", IPAddress: "cli",
	})
	if on {
		fmt.Fprintf(stdout, "Two-step verification is off for %s and their sessions have ended. They set it up again after signing in.\n", account.Username)
	} else {
		fmt.Fprintf(stdout, "Two-step verification was not on for %s; their sessions have ended.\n", account.Username)
	}
	return 0
}
