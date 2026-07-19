package core

import (
	"fmt"

	"go-press/core/admin"
	"go-press/core/content"
	coreMedia "go-press/core/media"
	"go-press/core/menu"
	"go-press/core/option"
	"go-press/core/taxonomy"
	"go-press/core/user"
	"go-press/pkg/logger"

	"gorm.io/gorm"
)

// Migrate runs database auto-migration for all GoPress core tables.
func (e *Engine) Migrate() error {
	return MigrateDB(e.DB)
}

// MigrateDB runs database auto-migration for all GoPress core tables.
func MigrateDB(db *gorm.DB) error {
	logger.Info("Running GoPress core table migration...")
	if err := db.AutoMigrate(coreModels()...); err != nil {
		return err
	}
	return migrateLegacyUserColumns(db.Migrator())
}

type columnAlterer interface {
	AlterColumn(value interface{}, field string) error
}

// GORM AutoMigrate does not relax an existing NOT NULL constraint when a
// model field becomes nullable. Explicitly align credentials that external-only
// accounts are allowed to omit.
func migrateLegacyUserColumns(migrator columnAlterer) error {
	for _, field := range []string{"Email", "PasswordHash"} {
		if err := migrator.AlterColumn(&user.User{}, field); err != nil {
			return fmt.Errorf("migrate nullable user column %s: %w", field, err)
		}
	}
	return nil
}

func coreModels() []interface{} {
	return []interface{}{
		// Content
		&content.Content{},
		&content.ContentMeta{},
		// Taxonomy
		&taxonomy.Term{},
		&taxonomy.Taxonomy{},
		&taxonomy.TermRelationship{},
		// Users
		&user.User{},
		&user.UserMeta{},
		&user.UserIdentity{},
		&user.UserSession{},
		// Options
		&option.Option{},
		// Menus
		&menu.Menu{},
		&menu.Item{},
		// Media
		&coreMedia.Media{},
		&coreMedia.MediaVariant{},
		// Admin (audit logs)
		&admin.AuditLog{},
	}
}
