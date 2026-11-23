// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package mfa

import (
	"context"
	"fmt"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// record is one user's second factor, a row of user_mfa. It mirrors
// userMFAV1; when a later migration changes the table, record changes with
// it and userMFAV1 stays as it was.
type record struct {
	UserID              uint       `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	TOTPSecret          string     `gorm:"column:totp_secret"`
	PendingSecret       string     `gorm:"column:pending_secret"`
	PendingExpiresAt    *time.Time `gorm:"column:pending_expires_at"`
	KeyID               string     `gorm:"column:key_id"`
	EnabledAt           *time.Time `gorm:"column:enabled_at"`
	LastStep            int64      `gorm:"column:last_step"`
	RecoveryHashes      string     `gorm:"column:recovery_hashes"`
	RecoveryGeneratedAt *time.Time `gorm:"column:recovery_generated_at"`
	FailedCount         int        `gorm:"column:failed_count"`
	FailedSince         *time.Time `gorm:"column:failed_since"`
	LockedUntil         *time.Time `gorm:"column:locked_until"`
	SessionsNotBefore   *time.Time `gorm:"column:sessions_not_before"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (record) TableName() string { return dbprefix.Table("user_mfa") }

// userMFAV1 freezes the table as core migration step 3 creates it. The
// table is not part of the AutoMigrate that runs on every start.
type userMFAV1 struct {
	UserID              uint       `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	TOTPSecret          string     `gorm:"column:totp_secret;type:text;not null;default:''"`
	PendingSecret       string     `gorm:"column:pending_secret;type:text;not null;default:''"`
	PendingExpiresAt    *time.Time `gorm:"column:pending_expires_at"`
	KeyID               string     `gorm:"column:key_id;size:16;not null;default:''"`
	EnabledAt           *time.Time `gorm:"column:enabled_at"`
	LastStep            int64      `gorm:"column:last_step;not null;default:0"`
	RecoveryHashes      string     `gorm:"column:recovery_hashes;type:text;not null;default:''"`
	RecoveryGeneratedAt *time.Time `gorm:"column:recovery_generated_at"`
	FailedCount         int        `gorm:"column:failed_count;not null;default:0"`
	FailedSince         *time.Time `gorm:"column:failed_since"`
	LockedUntil         *time.Time `gorm:"column:locked_until"`
	SessionsNotBefore   *time.Time `gorm:"column:sessions_not_before"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;not null"`
}

func (userMFAV1) TableName() string { return dbprefix.Table("user_mfa") }

// CreateTableV1 is core migration step 3, "create_user_mfa". It creates the
// user_mfa table, one row per user who set up or is setting up two-step
// verification, and does nothing when the table exists.
//
// There is no foreign key to users: users are soft-deleted, so a cascade
// would never fire, and a deleted user cannot sign in anyway. Remove
// deletes the row when an account's second factor is reset.
func CreateTableV1(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	if db.Migrator().HasTable(&userMFAV1{}) {
		return nil
	}
	if err := db.Migrator().CreateTable(&userMFAV1{}); err != nil {
		return fmt.Errorf("create %s: %w", (userMFAV1{}).TableName(), err)
	}
	return nil
}
