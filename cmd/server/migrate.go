package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/migrate"
	"github.com/0xmattg/go-press/pkg/dbprefix"
	pg "github.com/0xmattg/go-press/pkg/postgresql"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// onlineMigrationWait bounds how long -migrate=up waits for online steps.
const onlineMigrationWait = 2 * time.Hour

// runMigrateCommand implements -migrate and returns the process exit code.
//
//	status  lists the site's recorded migrations and the core steps this
//	        binary would still apply; it changes nothing.
//	up      applies every pending migration the way a server start does
//	        (core, then each active plugin), waits for the online steps,
//	        prints the status, and exits without serving HTTP.
func runMigrateCommand(action string, cfg *config.Config, configPath string) int {
	if action != "status" && action != "up" {
		fmt.Fprintf(os.Stderr, "unknown -migrate action %q: use status or up\n", action)
		return 2
	}
	if cfg == nil || !cfg.Install.Completed {
		fmt.Fprintf(os.Stderr, "-migrate needs an installed site, but %s is missing or not installed yet\n", configPath)
		return 2
	}
	if action == "up" {
		engine, err := core.BuildAndBootstrap(cfg, configPath, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
			return 1
		}
		waitCtx, cancelWait := context.WithTimeout(context.Background(), onlineMigrationWait)
		waitErr := engine.WaitOnlineMigrations(waitCtx)
		cancelWait()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		_ = engine.Shutdown(shutdownCtx)
		cancelShutdown()
		if waitErr != nil {
			fmt.Fprintf(os.Stderr, "online migrations did not finish: %v\n", waitErr)
			return 1
		}
	}

	failed, err := printMigrationStatus(os.Stdout, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read migration status: %v\n", err)
		return 1
	}
	if action == "up" && failed > 0 {
		return 1
	}
	return 0
}

// printMigrationStatus writes the status report and returns how many
// recorded steps are in the failed state.
func printMigrationStatus(w io.Writer, cfg *config.Config) (int, error) {
	if cfg.PG.TablePrefix != "" {
		dbprefix.Set(cfg.PG.TablePrefix)
	}
	db, err := pg.NewConnection(cfg.PG)
	if err != nil {
		return 0, err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Discard})

	runner := core.MigrationRunner(db)
	records, err := runner.Status(context.Background())
	if err != nil {
		return 0, err
	}
	coreModule := core.CoreMigrations()

	fmt.Fprintf(w, "Site table prefix %q, record table %s\n\n", runner.Prefix, runner.Table())
	failed := 0
	if len(records) == 0 {
		fmt.Fprintln(w, "No migrations recorded yet. The next start, or -migrate=up, records the baseline.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "MODULE\tVERSION\tNAME\tSTATUS\tMODE\tPHASE\tRUNS\tAPPLIED AT\tLAST RUN\tBY")
		for _, rec := range records {
			applied := "-"
			if rec.AppliedAt != nil {
				applied = rec.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
				rec.Module, rec.Version, rec.Name, rec.Status, rec.Mode, rec.Phase, rec.Runs,
				applied, rec.LastRunAt.UTC().Format(time.RFC3339), rec.AppliedBy)
		}
		_ = tw.Flush()
		for _, rec := range records {
			if rec.Status == migrate.StatusFailed {
				failed++
				fmt.Fprintf(w, "\nFAILED %s %d_%s: %s\n", rec.Module, rec.Version, rec.Name, rec.Error)
			}
		}
	}

	fmt.Fprintln(w)
	pending := migrate.Pending(records, coreModule)
	if len(pending) == 0 {
		fmt.Fprintln(w, "Core: up to date.")
	} else {
		fmt.Fprintln(w, "Core steps this binary applies on the next start or with -migrate=up:")
		for _, step := range pending {
			mode, phase := step.Mode, step.Phase
			if mode == "" {
				mode = migrate.Once
			}
			if phase == "" {
				phase = migrate.Blocking
			}
			fmt.Fprintf(w, "  %d_%s (%s, %s)\n", step.Version, step.Name, mode, phase)
		}
	}
	for _, rec := range migrate.Unknown(records, coreModule) {
		fmt.Fprintf(w, "Recorded by a newer release, unknown to this binary: %s %d_%s (by %s)\n", rec.Module, rec.Version, rec.Name, rec.AppliedBy)
	}
	fmt.Fprintln(w, "Plugin steps are applied when each plugin is activated.")
	return failed, nil
}
