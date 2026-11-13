// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/dbschema"
	"github.com/0xmattg/go-press/core/extension"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/migrate"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/version"

	"gorm.io/gorm"
)

// CoreMigrationModule is the module name of core's own migrations in the
// <prefix>schema_migrations table.
const CoreMigrationModule = "core"

// Migrate runs the blocking core migrations for the engine's database.
func (e *Engine) Migrate() error {
	return MigrateDB(e.DB)
}

// MigrateDB brings the core tables of the current site (dbprefix) up to date.
//
// It holds the site's migration lock while it runs GORM AutoMigrate and the
// legacy patches exactly as earlier releases did, then applies every pending
// blocking core step and records it. Online core steps are left to
// Engine.StartOnlineMigrations. Calling it again, as the installer and the
// first boot do, finds nothing to do apart from the idempotent AutoMigrate.
func MigrateDB(db *gorm.DB) error {
	logger.Info("Running GoPress core table migration...")
	_, err := MigrationRunner(db).Run(context.Background(), migrate.Blocking, CoreMigrations())
	return err
}

// MigrationRunner returns a migration runner for the current site.
func MigrationRunner(db *gorm.DB) *migrate.Runner {
	return &migrate.Runner{DB: db, Prefix: dbprefix.Get(), AppVersion: version.String()}
}

// CoreMigrations returns core's migration module.
//
// Steps follow the rules in the migrate package: add only, idempotent with
// respect to AutoMigrate, and named indexes through dbschema.SiteIndexName.
// Version 1 is the baseline: the schema that AutoMigrate and the legacy
// patches maintain. Sites installed before versioned migrations record it as
// adopted, new installs as applied. Version 2 creates the extension package
// tables (core/extension), which are not part of AutoMigrate.
func CoreMigrations() migrate.Module {
	return migrate.Module{
		Name: CoreMigrationModule,
		Plan: migrate.Plan{
			Existing: func(ctx context.Context, db *gorm.DB) (bool, error) {
				// Exact name: another site's prefix may start with this one.
				return migrate.TableExists(ctx, db, (content.Content{}).TableName())
			},
			Prepare: func(_ context.Context, db *gorm.DB) error {
				return prepareCoreSchema(db)
			},
			// Versions are numbered here, in one list, for all of core.
			Steps: []migrate.Step{
				{Version: 1, Name: "baseline", Baseline: true},
				{Version: 2, Name: "create_extension_tables", Up: extension.CreateTablesV1},
			},
		},
	}
}

// migratePlugin applies a MigrationProvider plugin's blocking steps. The
// plugin manager calls it before every Activate callback, so a failure keeps
// the plugin inactive. After startup it also kicks off the plugin's online
// steps; during startup StartOnlineMigrations picks them up.
func (e *Engine) migratePlugin(p plugin.Plugin) error {
	provider, ok := p.(plugin.MigrationProvider)
	if !ok {
		return nil
	}
	if e.DB == nil {
		return fmt.Errorf("plugin %q has migrations but no database is configured", p.Name())
	}
	module := migrate.Module{Name: plugin.MigrationModule(p), Plan: provider.Migrations()}
	if _, err := MigrationRunner(e.DB).Run(e.migrationContext(), migrate.Blocking, module); err != nil {
		return fmt.Errorf("plugin %q migrations failed: %w", p.Name(), err)
	}
	if e.ready.Load() {
		e.startOnlineMigrations(module)
	}
	return nil
}

// StartOnlineMigrations runs the pending online steps of core and of every
// active plugin in the background. BuildAndBootstrap calls it right after
// MarkReady; custom composition roots should do the same. Failures are
// logged and retried on the next start.
func (e *Engine) StartOnlineMigrations() {
	modules := []migrate.Module{CoreMigrations()}
	for _, p := range e.PluginManager.ActivePlugins() {
		if provider, ok := p.(plugin.MigrationProvider); ok {
			modules = append(modules, migrate.Module{Name: plugin.MigrationModule(p), Plan: provider.Migrations()})
		}
	}
	e.startOnlineMigrations(modules...)
}

func (e *Engine) startOnlineMigrations(modules ...migrate.Module) {
	if e.DB == nil || !migrate.HasPhase(migrate.Online, modules...) {
		return
	}
	e.onlineMigrations.Add(1)
	go func() {
		defer e.onlineMigrations.Done()
		if _, err := MigrationRunner(e.DB).Run(e.migrationContext(), migrate.Online, modules...); err != nil {
			logger.Warn("Online migrations incomplete; they are retried on the next start", "error", err)
		}
	}()
}

// WaitOnlineMigrations blocks until the background online migrations
// started so far have finished, or ctx ends.
func (e *Engine) WaitOnlineMigrations(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		e.onlineMigrations.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) migrationContext() context.Context {
	if e.migrationCtx != nil {
		return e.migrationCtx
	}
	return context.Background()
}

// prepareCoreSchema is the transitional baseline that runs on every start:
// the same AutoMigrate and legacy patches as releases before versioned
// migrations, so the DDL against an existing database does not change.
func prepareCoreSchema(db *gorm.DB) error {
	if err := dbschema.AutoMigrate(db, coreModels()...); err != nil {
		return err
	}
	// LEGACY(taxonomy-slug-index): see migrateTaxonomySlugIndex.
	if err := migrateTaxonomySlugIndex(db); err != nil {
		return err
	}
	// LEGACY(user-nullable-credentials): see migrateLegacyUserColumns.
	if err := migrateLegacyUserColumns(db.Migrator()); err != nil {
		return err
	}
	// LEGACY(media-updated-at): see backfillMediaUpdatedAt.
	return backfillMediaUpdatedAt(db)
}

// migrateTaxonomySlugIndex relaxes the legacy globally-unique term slug index.
// Core command services continue enforcing the exact historical rule when no
// request scope is active, while extensions may safely reuse a slug in another
// scoped variant. Existing rows and relationships remain untouched.
//
// LEGACY(taxonomy-slug-index): sites installed before core 0.6.65 still carry
// the old unique index on terms.slug; this replaces it with a plain index. It
// runs on every boot from MigrateDB, after AutoMigrate, and does nothing once
// the unique index is gone, so keeping it costs one index lookup. Remove only
// after a versioned migration (audit A15) owns this upgrade, or once release
// notes stop supporting direct upgrades from versions before 0.6.65.
func migrateTaxonomySlugIndex(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	indexes, err := db.Migrator().GetIndexes(&taxonomy.Term{})
	if err != nil {
		return fmt.Errorf("inspect taxonomy term indexes: %w", err)
	}
	for _, index := range indexes {
		columns := index.Columns()
		unique, ok := index.Unique()
		if !ok || !unique || len(columns) != 1 || columns[0] != "slug" {
			continue
		}
		if err := db.Migrator().DropIndex(&taxonomy.Term{}, index.Name()); err != nil {
			return fmt.Errorf("drop legacy unique taxonomy slug index: %w", err)
		}
	}
	if !db.Migrator().HasIndex(&taxonomy.Term{}, "Slug") {
		if err := db.Migrator().CreateIndex(&taxonomy.Term{}, "Slug"); err != nil {
			return fmt.Errorf("create taxonomy slug index: %w", err)
		}
	}
	return nil
}

// backfillMediaUpdatedAt copies created_at into a missing media updated_at.
//
// LEGACY(media-updated-at): media rows created before core 0.6.62 have no
// updated_at, which optimistic metadata updates rely on. It runs on every boot
// from MigrateDB as one UPDATE that matches no rows once backfilled. Remove
// under the same conditions as LEGACY(taxonomy-slug-index), for versions
// before 0.6.62.
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
//
// LEGACY(user-nullable-credentials): users tables created by releases that
// predate external-only accounts (this step was added in core 0.6.16) declare
// email and password_hash NOT NULL, so wallet or OAuth sign-ups would fail.
// It runs on every boot from MigrateDB; re-applying the column definition is
// harmless. Remove under the same conditions as LEGACY(taxonomy-slug-index),
// for versions before 0.6.16.
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
