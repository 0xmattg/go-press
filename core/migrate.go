package core

import (
	"errors"
	"fmt"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/logger"

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
	if err := migrateLegacyUserColumns(db.Migrator()); err != nil {
		return err
	}
	return backfillMediaUpdatedAt(db)
}

func backfillMediaUpdatedAt(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	return db.Model(&coreMedia.Media{}).
		Where("updated_at IS NULL").
		Update("updated_at", gorm.Expr("created_at")).Error
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
		// Comments reference both content and users.
		&comment.Comment{},
		// Options
		&option.Option{},
		// Menus
		&menu.Menu{},
		&menu.Item{},
		// Media
		&coreMedia.Media{},
		&coreMedia.MediaVariant{},
		// Cross-transport audit events
		&audit.Event{},
		// Protocol-neutral Agent foundation
		&agent.ServiceAccount{},
		&agent.Credential{},
		&agent.IdempotencyRecord{},
		&agent.AuditEvent{},
	}
}
