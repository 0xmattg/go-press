package core

import (
	"fmt"
	"path/filepath"

	"go-press/config"
	"go-press/pkg/dbprefix"
	pg "go-press/pkg/postgresql"
)

// BuildAndBootstrap creates a fully initialized Engine by connecting to the
// database, running migrations, optionally seeding data, bootstrapping all
// subsystems, loading themes and plugins, and setting up the router.
//
// This is the standard way to construct a production-ready Engine from a config.
func BuildAndBootstrap(cfg *config.Config, configPath string, seed bool) (*Engine, error) {
	// Set table prefix before any DB operations
	if cfg.PG.TablePrefix != "" {
		dbprefix.Set(cfg.PG.TablePrefix)
	}

	database, err := pg.NewConnection(cfg.PG)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	engine := New(cfg, database)

	if err := engine.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	if seed {
		seedPath := filepath.Join(filepath.Dir(configPath), "seed.toml")
		if err := engine.SeedFromFile(seedPath); err != nil {
			return nil, fmt.Errorf("failed to seed data: %w", err)
		}
	}

	if err := engine.Bootstrap(); err != nil {
		return nil, fmt.Errorf("failed to bootstrap engine: %w", err)
	}

	if err := engine.LoadAllThemes(); err != nil {
		return nil, fmt.Errorf("failed to load themes: %w", err)
	}

	engine.LoadAllPlugins()
	engine.SetupAdmin()
	engine.SetupRouter()
	return engine, nil
}
