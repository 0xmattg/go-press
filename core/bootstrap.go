// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/pkg/dbprefix"
	"github.com/0xmattg/go-press/pkg/logger"
	pg "github.com/0xmattg/go-press/pkg/postgresql"
	"github.com/0xmattg/go-press/version"
)

// BuildAndBootstrap creates a fully initialized Engine by connecting to the
// database, running migrations, optionally seeding data, bootstrapping all
// subsystems, loading themes and plugins, and setting up the router.
//
// This is the standard way to construct a production-ready Engine from a config.
// If any step after the database connection fails, the partially built engine
// is shut down so its workers, tickers, cache sweepers, and connections do not
// leak (the installer retries bootstrap inside the same process).
func BuildAndBootstrap(cfg *config.Config, configPath string, seed bool) (*Engine, error) {
	return BuildAndBootstrapWithOptions(cfg, configPath, BootstrapOptions{Seed: seed})
}

// BootstrapOptions are the optional inputs of BuildAndBootstrapWithOptions.
type BootstrapOptions struct {
	// Seed seeds from seed.toml next to the config file on first run.
	Seed bool
	// ThemePackages are theme packages, .zip archives or theme source
	// directories, to load at boot for local development. Each is checked
	// like an installed package but not verified, so they are refused when
	// server.mode is "release".
	ThemePackages []string
}

// BuildAndBootstrapWithOptions is BuildAndBootstrap with optional inputs.
func BuildAndBootstrapWithOptions(cfg *config.Config, configPath string, opts BootstrapOptions) (_ *Engine, err error) {
	// Previewing unsigned theme packages is a development tool; production
	// sites install packages through the signed install flow.
	if len(opts.ThemePackages) > 0 && cfg.Server.Mode == "release" {
		return nil, fmt.Errorf("theme package previews are not available when server.mode is \"release\" (%s)", configPath)
	}
	// Refuse to start with an empty or placeholder admin token secret: with a
	// known secret anyone can forge admin sessions. The installer generates a
	// unique random secret, so a completed install should never hit this.
	if cfg.CMS.JWTSecretInsecure() {
		return nil, fmt.Errorf("insecure cms.jwt_secret in %s: it is empty or the shipped placeholder; set a unique random value before starting", configPath)
	}
	// A partial or malformed [market] section would leave the site trusting
	// an inconsistent mix of markets and keys, so refuse to start.
	marketTrust, err := extension.NewTrustRoot(cfg.Market)
	if err != nil {
		return nil, fmt.Errorf("invalid [market] in %s: %w", configPath, err)
	}

	// Set table prefix before any DB operations
	if cfg.PG.TablePrefix != "" {
		dbprefix.Set(cfg.PG.TablePrefix)
	}

	database, err := pg.NewConnection(cfg.PG)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	engine := New(cfg, database)
	defer func() {
		if err == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := engine.Shutdown(ctx); shutdownErr != nil {
			logger.Error("Failed to release partially bootstrapped engine", "error", shutdownErr)
		}
	}()
	engine.SiteDir = filepath.Dir(configPath)
	engine.ConfigPath = configPath
	engine.previewThemePackages = append([]string(nil), opts.ThemePackages...)
	engine.ExtStore = extension.NewStore(database)
	engine.PackageArchives = extension.NewArchives(filepath.Join(engine.SiteDir, "packages", "archives"))
	engine.Market = extension.NewMarket(engine.ExtStore, marketTrust, nil, "GoPress/"+version.String())
	logger.Info("Extension market", "mode", marketTrust.Mode(), "host", marketTrust.Host())

	if err := engine.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	if opts.Seed {
		seedPath := filepath.Join(filepath.Dir(configPath), "seed.toml")
		if err := engine.SeedFromFile(seedPath); err != nil {
			return nil, fmt.Errorf("failed to seed data: %w", err)
		}
	}

	if err := engine.Bootstrap(); err != nil {
		return nil, fmt.Errorf("failed to bootstrap engine: %w", err)
	}
	engine.logDeveloperMode()

	if err := engine.LoadAllThemes(); err != nil {
		return nil, fmt.Errorf("failed to load themes: %w", err)
	}

	engine.LoadAllPlugins()

	// Gate: every theme/plugin version must be valid semver so dependency
	// constraints stay comparable. Non-fatal — warn and keep serving.
	for _, problem := range engine.ValidateExtensionVersions() {
		logger.Warn("Extension version is not valid semver", "detail", problem)
	}

	// Auto-activate the active theme's required plugins (and log unmet deps)
	// before the router is built, so their hooks/routes are wired for requests.
	engine.ReconcileActiveThemeDependencies()

	engine.SetupAdmin()
	engine.SetupRouter()
	engine.MarkReady()
	engine.StartOnlineMigrations()
	return engine, nil
}
