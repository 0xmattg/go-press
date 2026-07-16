package core

import (
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
	return db.AutoMigrate(coreModels()...)
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
