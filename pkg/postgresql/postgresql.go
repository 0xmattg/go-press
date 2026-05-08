package postgresql

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"go-press/config"
	"go-press/pkg/dbprefix"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var safeDatabaseName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func BuildDSN(cfg config.PGConfig) string {
	schema := cfg.Schema
	if schema == "" {
		schema = "public"
	}

	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s search_path=%s sslmode=disable",
		cfg.Hostname, cfg.Port, cfg.User, cfg.Password, cfg.Database, schema,
	)
}

func NewConnection(cfg config.PGConfig) (*gorm.DB, error) {
	dsn := BuildDSN(cfg)

	logLevel := logger.Info

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: dbprefix.Get(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

	if cfg.ConnMaxLifetime != "" {
		if d, err := time.ParseDuration(cfg.ConnMaxLifetime); err == nil {
			sqlDB.SetConnMaxLifetime(d)
		}
	}

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Database connection established")
	return db, nil
}

func EnsureDatabase(cfg config.PGConfig) error {
	if strings.TrimSpace(cfg.Database) == "" {
		return fmt.Errorf("database name is required")
	}
	if !safeDatabaseName.MatchString(cfg.Database) {
		return fmt.Errorf("database name %q contains unsupported characters", cfg.Database)
	}

	db, err := NewConnection(cfg)
	if err == nil {
		return closeSQLDB(db)
	}

	if !isMissingDatabaseError(err) {
		return err
	}

	for _, maintenanceDB := range []string{"postgres", "template1"} {
		adminCfg := cfg
		adminCfg.Database = maintenanceDB

		adminDB, adminErr := NewConnection(adminCfg)
		if adminErr != nil {
			continue
		}

		if ensureErr := createDatabaseIfNeeded(adminDB, cfg.Database); ensureErr != nil {
			_ = closeSQLDB(adminDB)
			return ensureErr
		}
		_ = closeSQLDB(adminDB)

		targetDB, targetErr := NewConnection(cfg)
		if targetErr != nil {
			return targetErr
		}
		defer closeSQLDB(targetDB)

		if cfg.Schema != "" && cfg.Schema != "public" {
			if err := targetDB.Exec(`CREATE SCHEMA IF NOT EXISTS "` + quoteIdentifier(cfg.Schema) + `"`).Error; err != nil {
				return fmt.Errorf("failed to create schema %q: %w", cfg.Schema, err)
			}
		}

		return nil
	}

	return fmt.Errorf("database %q does not exist, and GoPress could not connect to maintenance databases postgres/template1 to create it", cfg.Database)
}

func createDatabaseIfNeeded(db *gorm.DB, databaseName string) error {
	var exists bool
	if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = ?)", databaseName).Scan(&exists).Error; err != nil {
		return fmt.Errorf("failed to query database catalog: %w", err)
	}
	if exists {
		return nil
	}

	if err := db.Exec(`CREATE DATABASE "` + quoteIdentifier(databaseName) + `"`).Error; err != nil {
		return fmt.Errorf("failed to create database %q: %w", databaseName, err)
	}

	return nil
}

func closeSQLDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func isMissingDatabaseError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 3D000") || strings.Contains(msg, "does not exist")
}

func quoteIdentifier(name string) string {
	return strings.ReplaceAll(name, `"`, `""`)
}
